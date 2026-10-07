package database

import (
	"path/filepath"
	"testing"
)

func TestSearchPathSealingRollsBackWithPublication(t *testing.T) {
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
 INSERT INTO observation(snapshot_id,content_id,path) VALUES(1,1,'report');
 CREATE TRIGGER fail_seal AFTER UPDATE OF sealed ON search_path WHEN NEW.sealed=1
 BEGIN SELECT RAISE(ABORT,'seal failed'); END;`)
	if err != nil {
		t.Fatal(err)
	}
	publish := `UPDATE snapshot SET state='complete',source_digest=zeroblob(32),
 file_count=1,content_count=1 WHERE id=1`
	if _, err := db.db.Exec(publish); err == nil {
		t.Fatal("accepted failed sealing")
	}
	var state string
	var sealed int
	if err := db.db.QueryRow(`SELECT s.state,p.sealed FROM snapshot s
 JOIN observation o ON o.snapshot_id=s.id JOIN search_path p ON p.path=o.path`).
		Scan(&state, &sealed); err != nil || state != "importing" || sealed != 0 {
		t.Fatalf("publication rollback: state=%s sealed=%d: %v", state, sealed, err)
	}
	if _, err := db.db.Exec(`DROP TRIGGER fail_seal`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.db.Exec(publish); err != nil {
		t.Fatal(err)
	}
	if err := db.db.QueryRow(`SELECT sealed FROM search_path WHERE path='report'`).
		Scan(&sealed); err != nil || sealed != 1 {
		t.Fatalf("published seal: %d: %v", sealed, err)
	}
	_, err = db.db.Exec(`DROP TRIGGER observation_search_insert;
 INSERT INTO snapshot(id,disk_id,state,captured_at,imported_at,capture_provenance)
 VALUES(2,1,'importing','2024-01-01T00:00:00.000000000Z',
 '2024-01-01T00:00:00.000000000Z','explicit');
 INSERT INTO observation(snapshot_id,content_id,path) VALUES(2,1,'missing');
 UPDATE snapshot SET state='complete',source_digest=zeroblob(32),file_count=1,content_count=1
 WHERE id=2;`)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.db.Exec(`INSERT INTO search_path(path,name) VALUES('missing','missing')`); err == nil {
		t.Fatal("inserted an unsealed index for a completed observation")
	}
}
