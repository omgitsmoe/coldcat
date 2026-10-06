package database

import (
	"context"
	"errors"
	"path/filepath"
	"testing"

	"github.com/omgitsmoe/coldcat/internal/base"
)

func TestInventoryRevisionAndRecovery(t *testing.T) {
	path := filepath.Join(t.TempDir(), "catalog.sqlite")
	db, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}

	t.Cleanup(func() { db.Close() })
	if _, err := db.db.Exec(`INSERT INTO disk(id,label,capacity) VALUES(1,'disk',0);
INSERT INTO snapshot(id,disk_id,state,captured_at,imported_at,capture_provenance,file_count,content_count,source_digest) VALUES(3,1,'complete','2023-01-01T00:00:00.000000000Z','2023-01-01T00:00:00.000000000Z','explicit',0,0,zeroblob(32));
INSERT INTO snapshot(id,disk_id,state,captured_at,imported_at,capture_provenance) VALUES(100,1,'importing','2023-01-02T00:00:00.000000000Z','2023-01-02T00:00:00.000000000Z','explicit');
INSERT INTO content(id,hash_type,hash) VALUES(100,'sha256',X'AB');
INSERT INTO observation(snapshot_id,content_id,path) VALUES(100,100,'unfinished');
INSERT INTO import_content(snapshot_id,content_id) VALUES(100,100);`); err != nil {
		t.Fatal(err)
	}

	state, err := db.GetCatalogState(t.Context())
	if err != nil || state.Revision != 3 {
		t.Fatalf("incomplete revision: %+v %v", state, err)
	}

	if _, err := db.LookupContent(t.Context(), "sha256", []byte{0xab}, base.ScopeCurrent); !errors.Is(
		err,
		ErrNotFound,
	) {
		t.Fatalf("incomplete hash visibility: %v", err)
	}

	if _, err := db.ListContentObservations(t.Context(), 100, base.ScopeHistory, 1, 0, nil); !errors.Is(
		err,
		ErrNotFound,
	) {
		t.Fatalf("incomplete observations: %v", err)
	}

	db.Close()
	db, err = Open(path)
	if err != nil {
		t.Fatal(err)
	}

	state, err = db.GetCatalogState(t.Context())
	if err != nil || state.Revision != 3 {
		t.Fatalf("recovered revision: %+v %v", state, err)
	}

	var count int
	if err := db.db.QueryRow("SELECT COUNT(*) FROM snapshot WHERE state='importing'").Scan(&count); err != nil ||
		count != 0 {
		t.Fatalf("recovery: %d %v", count, err)
	}

	cancelled, cancel := context.WithCancel(t.Context())
	cancel()
	if _, err := db.GetCatalogState(cancelled); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancelled readiness: %v", err)
	}

	if _, err := db.ListContentObservations(cancelled, 1, base.ScopeCurrent, 1, 0, nil); !errors.Is(
		err,
		context.Canceled,
	) {
		t.Fatalf("cancelled list: %v", err)
	}
}

func BenchmarkContentQueries(b *testing.B) {
	b.Run("content_lists", benchmarkContentLists)
	db, err := Open(filepath.Join(b.TempDir(), "catalog.sqlite"))
	if err != nil {
		b.Fatal(err)
	}
	defer db.Close()
	err = db.Transaction(func(tx *Tx) error {
		if _, err := tx.Exec(`INSERT INTO disk(id,label,capacity) VALUES(1,'disk',0);
INSERT INTO snapshot(id,disk_id,state,captured_at,imported_at,capture_provenance) VALUES(1,1,'importing','2023-01-01T00:00:00.000000000Z','2023-01-01T00:00:00.000000000Z','explicit');
INSERT INTO content(id,hash_type,hash) VALUES(1,'sha256',X'AB');
WITH RECURSIVE files(n) AS (VALUES(1) UNION ALL SELECT n+1 FROM files WHERE n<50000)
INSERT INTO observation(id,snapshot_id,content_id,path) SELECT n,1,1,'photos/é-'||n||'.jpg' FROM files;
UPDATE snapshot SET state='complete',file_count=50000,content_count=1,source_digest=zeroblob(32) WHERE id=1;`); err != nil {
			return err
		}

		return nil
	})
	if err != nil {
		b.Fatal(err)
	}

	b.Run("hash_lookup", func(b *testing.B) {
		b.ReportAllocs()
		for b.Loop() {
			if _, err := db.LookupContent(b.Context(), "sha256", []byte{0xab}, base.ScopeCurrent); err != nil {
				b.Fatal(err)
			}
		}
	})
	for _, test := range []struct {
		name  string
		after base.FileObservationId
	}{{"first_page", 0}, {"deep_page", 49000}} {
		b.Run(test.name, func(b *testing.B) {
			b.ReportAllocs()
			for b.Loop() {
				if _, err := db.ListContentObservations(b.Context(), 1, base.ScopeCurrent, 50, test.after, nil); err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}
