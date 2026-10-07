package database

import (
	"path/filepath"
	"testing"

	"github.com/omgitsmoe/coldcat/internal/base"
)

func TestFuzzyFingerprintCollisionDoesNotEstablishMatch(t *testing.T) {
	db, err := Open(filepath.Join(t.TempDir(), "catalog.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	_, err = db.db.Exec(`INSERT INTO disk(id,label,capacity) VALUES(1,'disk',0);
 INSERT INTO snapshot(id,disk_id,state,captured_at,imported_at,capture_provenance)
 VALUES(1,1,'importing','2023-01-01T00:00:00.000000000Z',
 '2023-01-01T00:00:00.000000000Z','explicit');
 INSERT INTO content(id,hash_type,hash) VALUES(1,'sha256',zeroblob(32));
 INSERT INTO observation(snapshot_id,content_id,path) VALUES(1,1,'zzzzzz');`)
	if err != nil {
		t.Fatal(err)
	}
	_, err = db.db.Exec(`INSERT INTO search_fuzzy_signature(field,signature,path_id)
 SELECT 'name',?,id FROM search_path WHERE path='zzzzzz'`, fuzzySignatures("report", true)[0])
	if err != nil {
		t.Fatal(err)
	}
	_, err = db.db.Exec(`UPDATE snapshot SET state='complete',source_digest=zeroblob(32),
 file_count=1,content_count=1 WHERE id=1`)
	if err != nil {
		t.Fatal(err)
	}
	page, err := db.Search(t.Context(), base.SearchFilters{
		Query: "report", Field: "name", Match: "fuzzy",
		ContentFilters: base.ContentFilters{Scope: base.ScopeCurrent, ReplicaMetric: base.ReplicaDisks},
	}, 50, base.SearchAnchor{}, nil)
	if err != nil || len(page.Items) != 0 {
		t.Fatalf("accepted collision: %+v %v", page.Items, err)
	}
}
