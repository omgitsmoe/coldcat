package database

import (
	"errors"
	"path/filepath"
	"testing"
)

func TestSourceDigestConstraints(t *testing.T) {
	db, err := Open(filepath.Join(t.TempDir(), "catalog.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if _, err := db.CreateDisk("disk", "", "", 0); err != nil {
		t.Fatal(err)
	}
	for _, digest := range []string{"NULL", "zeroblob(31)", "zeroblob(33)", "printf('%032d', 0)"} {
		err := db.Transaction(func(tx *Tx) error {
			_, err := tx.Exec(`INSERT INTO snapshot(disk_id,state,captured_at,imported_at,capture_provenance,file_count,content_count,source_digest)
 VALUES(1,'complete','2023-01-01T00:00:00.000000000Z','2023-01-01T00:00:00.000000000Z','explicit',0,0,` + digest + `)`)
			return err
		})
		if !errors.Is(err, ErrValidation) {
			t.Fatalf("accepted invalid digest %s: %v", digest, err)
		}
	}
}

func TestPublicationFailureRollsBackDigestAndEnrichment(t *testing.T) {
	db, err := Open(filepath.Join(t.TempDir(), "catalog.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	_, err = db.db.Exec(`INSERT INTO disk(id,label,capacity) VALUES(1,'disk',0);
 INSERT INTO snapshot(id,disk_id,state,captured_at,imported_at,capture_provenance,input_format) VALUES(1,1,'importing','2023-01-01T00:00:00.000000000Z','2023-01-01T00:00:00.000000000Z','explicit','cshd');
 INSERT INTO content(id,hash_type,hash) VALUES(1,'sha256',X'AB');
 INSERT INTO observation(snapshot_id,content_id,path) VALUES(1,1,'file');
 INSERT INTO pending_size(snapshot_id,content_id,size) VALUES(1,1,5);
 INSERT INTO import_content(snapshot_id,content_id) VALUES(1,1);
 CREATE TRIGGER fail_publish BEFORE UPDATE ON snapshot WHEN NEW.state='complete' BEGIN SELECT RAISE(ABORT,'publication failed'); END;`)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.PublishImport(t.Context(), PublishImportRequest{SnapshotID: 1, SourceDigest: [32]byte{1}}); err == nil {
		t.Fatal("publication succeeded")
	}
	var untouched bool
	if err := db.db.QueryRow(`SELECT s.state='importing' AND s.source_digest IS NULL AND c.size IS NULL
 AND EXISTS(SELECT 1 FROM pending_size WHERE snapshot_id=1)
 AND EXISTS(SELECT 1 FROM import_content WHERE snapshot_id=1)
 FROM snapshot s JOIN content c ON c.id=1 WHERE s.id=1`).Scan(&untouched); err != nil || !untouched {
		t.Fatalf("publication did not roll back: %v %v", untouched, err)
	}
}
