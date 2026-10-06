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

func diskField[T any](value T) base.DiskField[T] {
	return base.DiskField[T]{Present: true, Value: &value}
}

func TestDiskManagement(t *testing.T) {
	ctx := t.Context()
	db, err := database.Open(filepath.Join(t.TempDir(), "catalog.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	a := New(db)
	id, err := a.CreateDiskWithRequest(ctx, CreateDiskRequest{
		Label: "Archive α", Notes: "notes", Serial: "serial", Capacity: 123,
	})
	if err != nil {
		t.Fatal(err)
	}
	detail, err := a.GetDisk(ctx, id)
	if err != nil || detail.Disk.Label != "Archive α" || detail.Disk.Notes != "notes" ||
		detail.Disk.Serial != "serial" || detail.Disk.Capacity != 123 ||
		detail.LatestSnapshot != nil || detail.Cataloged != nil {
		t.Fatalf("created: %+v %v", detail, err)
	}
	other, err := a.CreateDisk(ctx, "other", "", "", 0)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := a.CreateDisk(ctx, "Archive α", "", "", 0); !errors.Is(err, database.ErrConflict) {
		t.Fatalf("duplicate: %v", err)
	}
	if _, err := a.UpdateDisk(ctx, id, UpdateDiskRequest{
		Label: diskField("other"), Notes: diskField("must roll back"),
	}); !errors.Is(err, database.ErrConflict) {
		t.Fatalf("conflict: %v", err)
	}
	unchanged, err := a.GetDisk(ctx, id)
	if err != nil || !reflect.DeepEqual(detail, unchanged) {
		t.Fatalf("conflict changed disk: %+v %v", unchanged, err)
	}
	updated, err := a.UpdateDisk(ctx, id, UpdateDiskRequest{
		Label: diskField("renamed"), Notes: base.DiskField[string]{Present: true},
		Capacity: diskField(int64(0)),
	})
	if err != nil || updated.Disk.Label != "renamed" || updated.Disk.Notes != "" ||
		updated.Disk.Serial != "serial" || updated.Disk.Capacity != 0 {
		t.Fatalf("update: %+v %v", updated, err)
	}
	if _, err := a.UpdateDisk(ctx, id, UpdateDiskRequest{
		Label: diskField("renamed"), Serial: diskField(""),
	}); err != nil {
		t.Fatal(err)
	}
	for _, req := range []UpdateDiskRequest{
		{}, {Label: diskField("")}, {Label: base.DiskField[string]{Present: true}},
		{Capacity: diskField(int64(-1))}, {Capacity: base.DiskField[int64]{Present: true}},
	} {
		if _, err := a.UpdateDisk(ctx, id, req); !errors.Is(err, database.ErrValidation) {
			t.Fatalf("accepted invalid update %+v: %v", req, err)
		}
	}
	for _, id := range []base.DiskId{0, -1, 999} {
		want := database.ErrValidation
		if id == 999 {
			want = database.ErrNotFound
		}
		if _, err := a.GetDisk(ctx, id); !errors.Is(err, want) {
			t.Fatalf("get %d: %v", id, err)
		}
		if _, err := a.UpdateDisk(ctx, id, UpdateDiskRequest{Label: diskField("x")}); !errors.Is(err, want) {
			t.Fatalf("update %d: %v", id, err)
		}
	}
	for _, req := range []CreateDiskRequest{{}, {Label: "x", Capacity: -1}} {
		if _, err := a.CreateDiskWithRequest(ctx, req); !errors.Is(err, database.ErrValidation) {
			t.Fatalf("create %+v: %v", req, err)
		}
	}
	canceled, cancel := context.WithCancel(ctx)
	cancel()
	if _, err := a.GetDisk(canceled, id); !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled get: %v", err)
	}
	if _, err := a.ListDisks(canceled, ListDisksRequest{}); !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled list: %v", err)
	}
	if _, err := a.UpdateDisk(canceled, id, UpdateDiskRequest{Label: diskField("x")}); !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled update: %v", err)
	}
	release, err := db.AcquireImport(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer release()
	if _, err := a.GetDisk(ctx, other); !errors.Is(err, database.ErrBusy) {
		t.Fatalf("get during import: %v", err)
	}
	if _, err := a.ListDisks(ctx, ListDisksRequest{}); !errors.Is(err, database.ErrBusy) {
		t.Fatalf("list during import: %v", err)
	}
	if _, err := a.UpdateDisk(ctx, id, UpdateDiskRequest{Label: diskField("x")}); !errors.Is(err, database.ErrBusy) {
		t.Fatalf("update during import: %v", err)
	}
}

func TestDiskInventorySummaries(t *testing.T) {
	ctx := t.Context()
	db, err := database.Open(filepath.Join(t.TempDir(), "catalog.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	a := New(db)
	id, err := a.CreateDisk(ctx, "source", "", "", 0)
	if err != nil {
		t.Fatal(err)
	}
	file := filepath.Join(t.TempDir(), "inventory.cshd")
	importAt := func(seconds int64, input string) base.Snapshot {
		t.Helper()
		if err := os.WriteFile(file, []byte(input), 0600); err != nil {
			t.Fatal(err)
		}
		s, err := a.ImportByLabel(ctx, "source", ImportRequest{
			Path: file, CapturedAt: time.Unix(seconds, 0), AllowRepeat: true,
		})
		if err != nil {
			t.Fatal(err)
		}
		return s
	}
	input := "# version 1\n,10,sha256," + fixtureSHA256AB + " one\n" +
		",10,sha256," + fixtureSHA256AB + " copy\n" +
		",0,sha256," + fixtureSHA256CD + " empty\n,,sha256," + fixtureSHA256EF + " unknown\n"
	importAt(30, input)
	importAt(10, "")
	latest := importAt(30, input)
	detail, err := a.GetDisk(ctx, id)
	if err != nil || detail.LatestSnapshot == nil || detail.LatestSnapshot.Id != latest.Id ||
		detail.Cataloged == nil || *detail.Cataloged != (base.DiskCataloged{
		FileCount: 4, ContentCount: 3, KnownBytes: 20, UnknownSizeFileCount: 1,
	}) {
		t.Fatalf("summary: %+v %v", detail, err)
	}
	page, err := a.ListDisks(ctx, ListDisksRequest{})
	if err != nil || len(page.Items) != 1 || !reflect.DeepEqual(
		page.Items[0].LatestSnapshot, detail.LatestSnapshot,
	) {
		t.Fatalf("list summary: %+v %v", page, err)
	}
	if _, err := a.UpdateDisk(ctx, id, UpdateDiskRequest{Label: diskField("new label")}); err != nil {
		t.Fatal(err)
	}
	if _, err := a.ImportByLabel(ctx, "source", ImportRequest{Path: file,
		CapturedAt: time.Unix(40, 0)}); !errors.Is(err, database.ErrNotFound) {
		t.Fatalf("old label lookup: %v", err)
	}
	if err := os.WriteFile(file, nil, 0600); err != nil {
		t.Fatal(err)
	}
	empty, err := a.ImportByLabel(ctx, "new label", ImportRequest{
		Path: file, CapturedAt: time.Unix(40, 0),
	})
	if err != nil {
		t.Fatal(err)
	}
	detail, err = a.GetDisk(ctx, id)
	if err != nil || detail.LatestSnapshot.Id != empty.Id ||
		*detail.Cataloged != (base.DiskCataloged{SizeComplete: true}) {
		t.Fatalf("empty inventory: %+v %v", detail, err)
	}
}

func TestDiskPaginationAndCursorLifetime(t *testing.T) {
	ctx := t.Context()
	path := filepath.Join(t.TempDir(), "catalog.sqlite")
	db, err := database.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	a := New(db)
	empty, err := a.ListDisks(ctx, ListDisksRequest{})
	if err != nil || empty.Items == nil || len(empty.Items) != 0 || empty.NextCursor != "" {
		t.Fatalf("empty: %+v %v", empty, err)
	}
	for _, label := range []string{"a", "b", "c"} {
		if _, err := a.CreateDisk(ctx, label, "", "", 0); err != nil {
			t.Fatal(err)
		}
	}
	first, err := a.ListDisks(ctx, ListDisksRequest{Limit: 1})
	if err != nil || len(first.Items) != 1 || first.NextCursor == "" {
		t.Fatalf("first: %+v %v", first, err)
	}
	if _, err := a.CreateDisk(ctx, "d", "", "", 0); err != nil {
		t.Fatal(err)
	}
	if _, err := a.UpdateDisk(ctx, 2, UpdateDiskRequest{Label: diskField("edited")}); err != nil {
		t.Fatal(err)
	}
	req := ListDisksRequest{Limit: 1, Cursor: first.NextCursor}
	var ids []base.DiskId
	for {
		page, err := a.ListDisks(ctx, req)
		if err != nil {
			t.Fatal(err)
		}
		for _, item := range page.Items {
			ids = append(ids, item.Disk.Id)
			if item.Disk.Id == 2 && item.Disk.Label != "edited" {
				t.Fatalf("metadata not current: %+v", item)
			}
		}
		if page.NextCursor == "" {
			break
		}
		req.Cursor = page.NextCursor
	}
	if !reflect.DeepEqual(ids, []base.DiskId{2, 3}) {
		t.Fatalf("traversal: %v", ids)
	}
	req.Cursor = first.NextCursor
	db.Close()
	db, err = database.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	a = New(db)
	if _, err := a.ListDisks(ctx, req); err != nil {
		t.Fatalf("restart: %v", err)
	}
	file := filepath.Join(t.TempDir(), "inventory.cshd")
	if err := os.WriteFile(file, []byte(",sha256,"+fixtureSHA256AB+" file\ninvalid\n"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := a.Import(ctx, ImportRequest{DiskID: 1, Path: file,
		CapturedAt: time.Unix(10, 0)}); err == nil {
		t.Fatal("accepted invalid import")
	}
	if _, err := a.ListDisks(ctx, req); err != nil {
		t.Fatalf("failed import: %v", err)
	}
	if err := os.WriteFile(file, nil, 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := a.Import(ctx, ImportRequest{DiskID: 1, Path: file,
		CapturedAt: time.Unix(10, 0)}); err != nil {
		t.Fatal(err)
	}
	if _, err := a.ListDisks(ctx, req); !errors.Is(err, database.ErrStaleCursor) {
		t.Fatalf("successful import: %v", err)
	}
	for _, limit := range []int{-1, 201} {
		if _, err := a.ListDisks(ctx, ListDisksRequest{Limit: limit}); !errors.Is(err, database.ErrValidation) {
			t.Fatalf("limit %d: %v", limit, err)
		}
	}
	for _, cursor := range []string{"!", strings.Repeat("x", 2049)} {
		if _, err := a.ListDisks(ctx, ListDisksRequest{Limit: 1, Cursor: cursor}); !errors.Is(err, database.ErrValidation) {
			t.Fatalf("cursor: %v", err)
		}
	}
	anchor := diskCursor{Version: 1, Kind: "disks", Limit: 1, AfterID: 1, MaxID: 3}
	for _, change := range []func(*diskCursor){
		func(c *diskCursor) { c.Version++ }, func(c *diskCursor) { c.Kind = "snapshots" },
		func(c *diskCursor) { c.Limit++ }, func(c *diskCursor) { c.AfterID = 0 },
		func(c *diskCursor) { c.MaxID = c.AfterID },
		func(c *diskCursor) { c.Catalog.Revision = -1 },
	} {
		bad := anchor
		change(&bad)
		data, err := json.Marshal(bad)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := a.ListDisks(ctx, ListDisksRequest{
			Limit: 1, Cursor: base64.RawURLEncoding.EncodeToString(data),
		}); !errors.Is(err, database.ErrValidation) {
			t.Fatalf("accepted %+v: %v", bad, err)
		}
	}
}
