package database

import (
	"context"
	"errors"
	"path/filepath"
	"testing"

	"github.com/omgitsmoe/coldcat/internal/base"
)

func TestDirectorySearchVisibilityAndCancellation(t *testing.T) {
	db, err := Open(filepath.Join(t.TempDir(), "catalog.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	_, err = db.db.Exec(`INSERT INTO disk(id,label,capacity) VALUES(1,'source',0);
 INSERT INTO snapshot(id,disk_id,state,captured_at,imported_at,capture_provenance)
 VALUES(1,1,'importing','2023-01-01T00:00:00.000000000Z',
 '2023-01-01T00:00:00.000000000Z','explicit'),
 (2,1,'importing','2023-01-01T00:00:00.000000000Z',
 '2023-01-01T00:00:00.000000000Z','explicit'),
 (3,1,'importing','2024-01-01T00:00:00.000000000Z',
 '2024-01-01T00:00:00.000000000Z','explicit');`)
	if err != nil {
		t.Fatal(err)
	}
	for _, id := range []int64{1, 2, 3} {
		if err := db.BuildDirectories(t.Context(), id); err != nil {
			t.Fatal(err)
		}
	}
	_, err = db.db.Exec(`UPDATE snapshot SET state='complete',source_digest=zeroblob(32),
 file_count=0,content_count=0 WHERE id IN(1,2)`)
	if err != nil {
		t.Fatal(err)
	}
	f := base.DirectorySearchFilters{
		Query: "*", Field: "name", Match: "syntax", Scope: base.ScopeCurrent,
	}
	page, err := db.SearchDirectories(t.Context(), f, 50, 0, nil)
	if err != nil || len(page.Items) != 1 || page.Items[0].Snapshot.Id != 2 ||
		!page.Items[0].IsCurrent || page.Catalog.Revision != 2 {
		t.Fatalf("current capture tie and incomplete exclusion: %+v, %v", page, err)
	}
	f.Scope = base.ScopeHistory
	page, err = db.SearchDirectories(t.Context(), f, 50, 0, nil)
	if err != nil || len(page.Items) != 2 || page.Items[0].IsCurrent ||
		!page.Items[1].IsCurrent {
		t.Fatalf("history visibility: %+v, %v", page, err)
	}
	f.SnapshotID = 3
	if _, err := db.SearchDirectories(t.Context(), f, 50, 0, nil); !errors.Is(err, ErrNotFound) {
		t.Fatalf("incomplete snapshot filter: %v", err)
	}
	f.SnapshotID = 0
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if _, err := db.SearchDirectories(ctx, f, 50, 0, nil); !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled search: %v", err)
	}
}
