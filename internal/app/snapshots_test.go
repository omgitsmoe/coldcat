package app

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/omgitsmoe/coldcat/internal/base"
	"github.com/omgitsmoe/coldcat/internal/database"
)

func TestDiskSnapshotsAndCursorLifetime(t *testing.T) {
	ctx := t.Context()
	path := filepath.Join(t.TempDir(), "catalog.sqlite")
	db, err := database.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	a := New(db)
	disk, err := a.CreateDisk(ctx, "source", "", "", 0)
	if err != nil {
		t.Fatal(err)
	}
	other, err := a.CreateDisk(ctx, "empty", "", "", 0)
	if err != nil {
		t.Fatal(err)
	}
	file := filepath.Join(t.TempDir(), "fixture.cshd")
	if err := os.WriteFile(file, []byte(",sha256,ab a\n"), 0600); err != nil {
		t.Fatal(err)
	}
	importAt := func(id base.DiskId, seconds int64) base.Snapshot {
		t.Helper()
		s, err := a.Import(ctx, ImportRequest{
			DiskID: id, Path: file, CapturedAt: time.Unix(seconds, 123), AllowRepeat: true,
		})
		if err != nil {
			t.Fatal(err)
		}
		return s
	}
	s1 := importAt(disk, 30)
	s2 := importAt(disk, 10)
	s3 := importAt(disk, 30)
	s4 := importAt(disk, 20)
	want := []base.Snapshot{s3, s1, s4, s2}
	req := ListDiskSnapshotsRequest{DiskID: disk, Limit: 1}
	first, err := a.ListDiskSnapshots(ctx, req)
	if err != nil || len(first.Items) != 1 || first.NextCursor == "" {
		t.Fatalf("first page: %+v %v", first, err)
	}
	latest, err := a.LatestCompleteSnapshot(ctx, disk)
	if err != nil || latest.Id != first.Items[0].Id {
		t.Fatalf("latest: %+v %v", latest, err)
	}
	var got []base.Snapshot
	for {
		page, err := a.ListDiskSnapshots(ctx, req)
		if err != nil {
			t.Fatal(err)
		}
		got = append(got, page.Items...)
		if page.NextCursor == "" {
			break
		}
		req.Cursor = page.NextCursor
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("snapshots: %+v, want %+v", got, want)
	}
	for _, limit := range []int{0, 1, 200} {
		page, err := a.ListDiskSnapshots(ctx, ListDiskSnapshotsRequest{DiskID: other, Limit: limit})
		if err != nil || page.Items == nil || len(page.Items) != 0 || page.NextCursor != "" {
			t.Fatalf("empty disk: %+v %v", page, err)
		}
	}
	if _, err := a.ListDiskSnapshots(ctx, ListDiskSnapshotsRequest{DiskID: 999}); !errors.Is(err, database.ErrNotFound) {
		t.Fatalf("missing disk: %v", err)
	}
	for _, limit := range []int{-1, 201} {
		if _, err := a.ListDiskSnapshots(ctx, ListDiskSnapshotsRequest{DiskID: disk, Limit: limit}); !errors.Is(err, database.ErrValidation) {
			t.Fatalf("limit %d: %v", limit, err)
		}
	}

	req.Cursor = first.NextCursor
	db.Close()
	db, err = database.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	a = New(db)
	if _, err := a.ListDiskSnapshots(ctx, req); err != nil {
		t.Fatalf("restart: %v", err)
	}
	if err := os.WriteFile(file, []byte(",sha256,ab temporary\ninvalid\n"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := a.Import(ctx, ImportRequest{DiskID: disk, Path: file,
		CapturedAt: time.Unix(40, 0)}); err == nil {
		t.Fatal("accepted failed import")
	}
	if _, err := a.ListDiskSnapshots(ctx, req); err != nil {
		t.Fatalf("failed import invalidated cursor: %v", err)
	}
	if err := os.WriteFile(file, nil, 0600); err != nil {
		t.Fatal(err)
	}
	importAt(other, 5)
	if _, err := a.ListDiskSnapshots(ctx, req); !errors.Is(err, database.ErrStaleCursor) {
		t.Fatalf("other disk import did not invalidate cursor: %v", err)
	}
	canceled, cancel := context.WithCancel(ctx)
	cancel()
	if _, err := a.ListDiskSnapshots(canceled, ListDiskSnapshotsRequest{DiskID: disk}); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancellation: %v", err)
	}
}

func TestSnapshotCursorValidation(t *testing.T) {
	db, err := database.Open(filepath.Join(t.TempDir(), "catalog.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	a := New(db)
	anchor := snapshotCursor{Version: 1, Kind: "disk_snapshots", DiskID: 1, Limit: 1,
		Catalog: base.CatalogState{Revision: 2}, AfterID: 1, AfterCapturedAt: time.Unix(1, 0)}
	encode := func(value snapshotCursor) string {
		data, err := json.Marshal(value)
		if err != nil {
			t.Fatal(err)
		}
		return base64.RawURLEncoding.EncodeToString(data)
	}
	for _, change := range []func(*snapshotCursor){
		func(c *snapshotCursor) { c.Version++ },
		func(c *snapshotCursor) { c.Kind = "observations" },
		func(c *snapshotCursor) { c.DiskID++ },
		func(c *snapshotCursor) { c.Limit++ },
		func(c *snapshotCursor) { c.AfterID = 0 },
		func(c *snapshotCursor) { c.AfterCapturedAt = time.Time{} },
		func(c *snapshotCursor) { c.Catalog.Revision = -1 },
	} {
		bad := anchor
		change(&bad)
		if _, err := a.ListDiskSnapshots(t.Context(), ListDiskSnapshotsRequest{
			DiskID: 1, Limit: 1, Cursor: encode(bad),
		}); !errors.Is(err, database.ErrValidation) {
			t.Fatalf("accepted cursor %+v: %v", bad, err)
		}
	}
	for _, cursor := range []string{
		"!", strings.Repeat("a", 2049),
		base64.RawURLEncoding.EncodeToString([]byte(`{"unknown":1}`)),
		base64.RawURLEncoding.EncodeToString([]byte(`{} {}`)),
	} {
		if _, err := a.ListDiskSnapshots(t.Context(), ListDiskSnapshotsRequest{
			DiskID: 1, Limit: 1, Cursor: cursor,
		}); !errors.Is(err, database.ErrValidation) {
			t.Fatalf("malformed cursor: %v", err)
		}
	}
}

func TestSnapshotPageLimits(t *testing.T) {
	db, err := database.Open(filepath.Join(t.TempDir(), "catalog.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	a := New(db)
	disk, err := a.CreateDisk(t.Context(), "source", "", "", 0)
	if err != nil {
		t.Fatal(err)
	}
	for i := range 51 {
		err := db.TransactionContext(t.Context(), func(tx *database.Tx) error {
			_, err := tx.ExecContext(t.Context(), `INSERT INTO snapshot(
				disk_id,state,captured_at,imported_at,capture_provenance,
				file_count,content_count,source_digest)
				VALUES(?,'complete',?,?,'explicit',0,0,zeroblob(32))`,
				disk, database.FormatTime(time.Unix(int64(i), 0)),
				database.FormatTime(time.Unix(100, 0)))
			return err
		})
		if err != nil {
			t.Fatal(err)
		}
	}
	first, err := a.ListDiskSnapshots(t.Context(), ListDiskSnapshotsRequest{DiskID: disk})
	if err != nil || len(first.Items) != 50 || first.NextCursor == "" {
		t.Fatalf("default limit: %+v %v", first, err)
	}
	last, err := a.ListDiskSnapshots(t.Context(), ListDiskSnapshotsRequest{
		DiskID: disk, Limit: 50, Cursor: first.NextCursor,
	})
	if err != nil || len(last.Items) != 1 || last.NextCursor != "" || last.Items[0].Id != 1 {
		t.Fatalf("last page: %+v %v", last, err)
	}
	all, err := a.ListDiskSnapshots(t.Context(), ListDiskSnapshotsRequest{DiskID: disk, Limit: 200})
	if err != nil || len(all.Items) != 51 || all.NextCursor != "" {
		t.Fatalf("maximum limit: %+v %v", all, err)
	}
}
