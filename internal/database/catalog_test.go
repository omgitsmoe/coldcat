package database

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
	"time"
)

func TestCatalogSummaryExcludesImportRemnants(t *testing.T) {
	db, err := Open(filepath.Join(t.TempDir(), "catalog.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	if _, err := db.CreateDisk("disk", "", "", 0); err != nil {
		t.Fatal(err)
	}
	err = db.TransactionContext(t.Context(), func(tx *Tx) error {
		_, err := tx.ExecContext(t.Context(), `
			INSERT INTO snapshot(id,disk_id,state,captured_at,imported_at,capture_provenance)
			VALUES(100,1,'importing',?,?,'explicit');
			INSERT INTO content(id,hash_type,hash) VALUES(1,'sha256',zeroblob(32));
			INSERT INTO observation(snapshot_id,content_id,path) VALUES(100,1,'hidden');
			INSERT INTO import_content(snapshot_id,content_id) VALUES(100,1);`,
			FormatTime(time.Unix(10, 0)), FormatTime(time.Unix(10, 0)))
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	got, err := db.GetCatalogSummary(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if got.Catalog.Revision != 0 || got.DiskCount != 1 ||
		got.FileCount != 0 || got.ContentCount != 0 {
		t.Fatalf("import remnants leaked: %+v", got)
	}
	ctx, cancel := context.WithDeadline(t.Context(), time.Now().Add(-time.Second))
	defer cancel()
	if _, err := db.GetCatalogSummary(ctx); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("deadline: %v", err)
	}
	recoveryErr := errors.New("cleanup failed")
	db.ImportCleanupFailed(recoveryErr)
	if _, err := db.GetCatalogSummary(t.Context()); !errors.Is(err, recoveryErr) {
		t.Fatalf("recovery-required access: %v", err)
	}
}
