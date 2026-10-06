package database

import (
	"errors"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestTimestampOrderingAndImmutability(t *testing.T) {
	db, err := Open(filepath.Join(t.TempDir(), "catalog.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	id, err := db.CreateDisk("disk", "", "", 1)
	if err != nil {
		t.Fatal(err)
	}
	for _, capture := range []time.Time{time.Unix(1, 100_000_000), time.Unix(1, 0), time.Unix(1, 90_000_000)} {
		err := db.Transaction(func(tx *Tx) error {
			_, err := tx.Exec(`INSERT INTO snapshot(disk_id,state,captured_at,imported_at,capture_provenance,file_count,content_count,source_digest) VALUES(?,'complete',?,?,'explicit',0,0,zeroblob(32))`, id, FormatTime(capture), FormatTime(time.Unix(2, 0)))
			return err
		})
		if err != nil {
			t.Fatal(err)
		}
	}
	latest, err := db.LatestCompleteSnapshot(t.Context(), 1)
	if err != nil {
		t.Fatal(err)
	}
	if latest.Id != 1 || latest.CapturedAt.Nanosecond() != 100_000_000 {
		t.Fatalf("fractional timestamp ordering: %+v", latest)
	}
	for _, statement := range []string{
		"DELETE FROM snapshot WHERE id=1",
		"UPDATE snapshot SET captured_at='2020-01-01T00:00:00Z' WHERE id=1",
		"UPDATE snapshot SET source_digest=randomblob(32) WHERE id=1",
		"INSERT INTO content(id,hash_type,hash) VALUES(1,'sha256',X'AB'); INSERT INTO observation(snapshot_id,content_id,path) VALUES(1,1,'a')",
	} {
		if err := db.Transaction(func(tx *Tx) error { _, err := tx.Exec(statement); return err }); err == nil {
			t.Fatalf("mutated complete snapshot: %s", statement)
		}
	}
}

func TestQueryIndexes(t *testing.T) {
	db, err := Open(filepath.Join(t.TempDir(), "catalog.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	for _, test := range []struct{ query, index string }{
		{"SELECT id FROM snapshot WHERE disk_id=1 AND state='complete' ORDER BY captured_at DESC,id DESC LIMIT 1", "snapshot_current"},
		{"SELECT id FROM snapshot WHERE state='complete' AND disk_id=1 AND input_format='cshd' AND captured_at='2023-01-01T00:00:00.000000000Z' AND source_digest=zeroblob(32) ORDER BY id LIMIT 1", "snapshot_source_identity"},
		{currentSnapshots + "SELECT COUNT(*) FROM observation o JOIN current_snapshot cs ON cs.id=o.snapshot_id WHERE o.content_id=1", "observation_content_snapshot"},
		{"SELECT id FROM observation WHERE snapshot_id=1 AND path='foo/bar'", "sqlite_autoindex_observation"},
		{"SELECT o.id FROM observation o JOIN snapshot s ON s.id=o.snapshot_id WHERE s.disk_id=1 AND o.path='foo/bar' AND s.state='complete'", "sqlite_autoindex_observation"},
		{"SELECT id FROM observation WHERE path='foo/bar'", "observation_path_snapshot"},
		{"SELECT id FROM content WHERE hash_type='sha256' AND hash=X'AB'", "sqlite_autoindex_content"},
		{contentObservationQuery, "observation_content_id"},
	} {
		var args []any
		if test.query == contentObservationQuery {
			args = []any{1, 0, "current", 51}
		}
		rows, err := db.db.Query("EXPLAIN QUERY PLAN "+test.query, args...)
		if err != nil {
			t.Fatal(err)
		}
		var plan []string
		for rows.Next() {
			var id, parent, unused int
			var detail string
			if err := rows.Scan(&id, &parent, &unused, &detail); err != nil {
				t.Fatal(err)
			}
			plan = append(plan, detail)
		}
		if err := rows.Err(); err != nil {
			t.Fatal(err)
		}
		rows.Close()
		if !strings.Contains(strings.Join(plan, "\n"), test.index) {
			t.Fatalf("missing index %s: %v", test.index, plan)
		}
	}
}

func TestRecoveryPreservesCompletedSnapshotsAndSharedContent(t *testing.T) {
	path := filepath.Join(t.TempDir(), "catalog.sqlite")
	db, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	_, err = db.db.Exec(`INSERT INTO disk(id,label,capacity) VALUES(7,'disk',100);
INSERT INTO snapshot(id,disk_id,state,captured_at,imported_at,capture_provenance) VALUES(9,7,'importing','2023-01-01T00:00:00.000000000Z','2023-01-02T00:00:00.000000000Z','explicit');
INSERT INTO content(id,hash_type,hash) VALUES(11,'sha256',X'AB');
INSERT INTO observation(snapshot_id,content_id,path) VALUES(9,11,'original');
UPDATE snapshot SET state='complete',file_count=1,content_count=1,source_digest=zeroblob(32) WHERE id=9;
INSERT INTO snapshot(id,disk_id,state,captured_at,imported_at,capture_provenance) VALUES(100,7,'importing','2023-01-03T00:00:00.000000000Z','2023-01-04T00:00:00.000000000Z','explicit');
INSERT INTO observation(snapshot_id,content_id,path) VALUES(100,11,'shared');
INSERT INTO import_content(snapshot_id,content_id) VALUES(100,11);
INSERT INTO pending_size(snapshot_id,content_id,size) VALUES(100,11,1);`)
	if err != nil {
		t.Fatal(err)
	}
	db.Close()
	db, err = Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	var count int
	if snapshot, err := db.GetCompleteSnapshot(t.Context(), 9); err != nil || snapshot.FileCount != 1 {
		t.Fatalf("completed snapshot not preserved: %+v %v", snapshot, err)
	}
	if err := db.db.QueryRow("SELECT COUNT(*) FROM content WHERE id=11 AND size IS NULL").Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 1 {
		t.Fatal("shared content removed or enriched")
	}
	if _, err := db.GetCompleteSnapshot(t.Context(), 100); !errors.Is(err, ErrNotFound) {
		t.Fatal(err)
	}
}

func TestDatabasePathURICharacters(t *testing.T) {
	path := filepath.Join(t.TempDir(), "catalog ?#% Unicode-é.sqlite")
	db, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	db.Close()
	db, err = Open(path)
	if err != nil {
		t.Fatal(err)
	}
	db.Close()
}
