package app

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/omgitsmoe/coldcat/internal/base"
	"github.com/omgitsmoe/coldcat/internal/database"
)

func TestCatalogSummary(t *testing.T) {
	path := filepath.Join(t.TempDir(), "catalog.sqlite")
	db, err := database.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	a := New(db)
	check := func(revision base.SnapshotId, disks, files, contents int64) {
		t.Helper()
		got, err := a.GetCatalogSummary(t.Context())
		if err != nil {
			t.Fatal(err)
		}
		want := base.CatalogSummary{
			Catalog:   base.CatalogState{Revision: revision},
			DiskCount: disks, FileCount: files, ContentCount: contents,
		}
		if got != want {
			t.Fatalf("summary: %+v, want %+v", got, want)
		}
	}
	check(0, 0, 0, 0)
	var disks []base.DiskId
	for i := range 3 {
		id, err := a.CreateDisk(t.Context(), fmt.Sprintf("disk-%d", i), "", "", 0)
		if err != nil {
			t.Fatal(err)
		}
		disks = append(disks, id)
	}
	check(0, 3, 0, 0)
	importInput := func(disk base.DiskId, capture int64, input string, fail bool) base.Snapshot {
		t.Helper()
		file := filepath.Join(t.TempDir(), "fixture.cshd")
		if err := os.WriteFile(file, []byte(input), 0600); err != nil {
			t.Fatal(err)
		}
		s, err := a.Import(t.Context(), ImportRequest{
			DiskID: disk, Path: file, CapturedAt: time.Unix(capture, 0),
		})
		if (err != nil) != fail {
			t.Fatalf("import: %v, expected failure %v", err, fail)
		}
		return s
	}
	shared := ",sha256," + fixtureSHA256AB + " a\n"
	first := importInput(disks[0], 10, shared+
		",sha256,"+fixtureSHA256AB+" copy\n"+
		",sha256,"+fixtureSHA256CD+" removed\n", false)
	check(first.Id, 3, 3, 2)
	second := importInput(disks[1], 10, shared+",md5,"+fixtureMD5AB+" other\n", false)
	check(second.Id, 3, 5, 3)
	newest := importInput(disks[0], 30, shared, false)
	check(newest.Id, 3, 3, 2)
	older := importInput(disks[0], 20, ",sha256,"+fixtureSHA256EE+" historical\n", false)
	check(older.Id, 3, 3, 2)
	tied := importInput(disks[0], 30, ",sha256,"+fixtureSHA256FF+" changed\n", false)
	check(tied.Id, 3, 3, 3)
	empty := importInput(disks[2], 10, "# version 1\n", false)
	check(empty.Id, 3, 3, 3)
	importInput(disks[2], 20, shared+"invalid record\n", true)
	check(empty.Id, 3, 3, 3)
	importInput(disks[2], 10, "# version 1\n", true)
	check(empty.Id, 3, 3, 3)

	release, err := db.AcquireImport(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	_, err = a.GetCatalogSummary(t.Context())
	release()
	if !errors.Is(err, database.ErrBusy) {
		t.Fatalf("import gate: %v", err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if _, err := a.GetCatalogSummary(ctx); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancellation: %v", err)
	}

	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	raw, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	_, err = raw.Exec(`INSERT INTO snapshot(id,disk_id,state,captured_at,imported_at,
		capture_provenance) VALUES(100,1,'importing',?,?,'explicit')`,
		database.FormatTime(time.Unix(40, 0)), database.FormatTime(time.Unix(40, 0)))
	closeErr := raw.Close()
	if err != nil || closeErr != nil {
		t.Fatalf("seed interruption: %v, close: %v", err, closeErr)
	}
	db, err = database.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	a = New(db)
	check(empty.Id, 3, 3, 3)
}
