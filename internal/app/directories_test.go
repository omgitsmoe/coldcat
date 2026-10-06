package app

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/omgitsmoe/coldcat/internal/base"
	"github.com/omgitsmoe/coldcat/internal/database"
)

func TestDirectoryBrowsingAndEnrichment(t *testing.T) {
	db, err := database.Open(filepath.Join(t.TempDir(), "catalog.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	a := New(db)
	importFiles := func(label, input string, capture int64) base.Snapshot {
		t.Helper()
		disk, err := a.CreateDisk(t.Context(), label, "", "", 100)
		if err != nil {
			t.Fatal(err)
		}
		file := filepath.Join(t.TempDir(), "files.cshd")
		if err := os.WriteFile(file, []byte("# version 1\n"+input), 0600); err != nil {
			t.Fatal(err)
		}
		snapshot, err := a.Import(t.Context(), ImportRequest{
			DiskID: disk, Path: file, CapturedAt: time.Unix(capture, 0),
		})
		if err != nil {
			t.Fatal(err)
		}
		return snapshot
	}
	source := importFiles("source", "10,4,sha256,"+fixtureSHA256AB+" foo/a\n"+
		"20,4,sha256,"+fixtureSHA256AB+" foo/nested/b\n"+
		",,sha256,"+fixtureSHA256CD+" foo/unknown\n"+
		",0,sha256,"+fixtureSHA256EF+" root-empty\n"+
		",0,sha256,"+fixtureSHA256EF+" foobar/empty\n"+
		",0,sha256,"+fixtureSHA256EF+" %_*/é\\name\n"+
		",0,sha256,"+fixtureSHA256EF+" []?/file\n"+
		",0,sha256,"+fixtureSHA256EF+" \U0010ffff-tail\n", 1)
	copy := importFiles("copy", ",4,sha256,"+fixtureSHA256AB+" unrelated/copy\n"+
		",0,sha256,"+fixtureSHA256EF+" elsewhere/empty\n", 1)
	partial := importFiles("partial", ",4,sha256,"+fixtureSHA256AB+" other/copy\n"+
		",,sha256,"+fixtureSHA256CD+" scattered/unknown\n", 1)
	if source.ContentCount != 3 || copy.ContentCount != 2 || partial.ContentCount != 2 {
		t.Fatalf("partial-copy fixture counts: %d, %d, %d",
			source.ContentCount, copy.ContentCount, partial.ContentCount)
	}

	detail, err := a.GetDirectory(t.Context(), GetDirectoryRequest{SnapshotID: source.Id, Path: "foo"})
	if err != nil {
		t.Fatal(err)
	}
	if detail.FileCount != 3 || detail.ContentCount != 2 || detail.KnownBytes != 8 ||
		detail.UniqueContentKnownBytes != 4 || detail.UnknownSizeFileCount != 1 ||
		detail.UnknownSizeContentCount != 1 || detail.MaxKnownMTime == nil ||
		detail.MaxKnownMTime.Unix() != 20 {
		t.Fatalf("summary: %+v", detail)
	}
	if fmt.Sprint(detail.RedundancyHistogram) != "[{1 1} {2 2}]" {
		t.Fatalf("histogram: %+v", detail.RedundancyHistogram)
	}

	request := ListDirectoryEntriesRequest{
		Filters: base.DirectoryFilters{SnapshotID: source.Id}, Limit: 1,
	}
	var paths []string
	for {
		page, err := a.ListDirectoryEntries(t.Context(), request)
		if err != nil {
			t.Fatal(err)
		}
		for _, entry := range page.Items {
			paths = append(paths, entry.Kind+":"+entry.Path)
		}
		request.Cursor = page.NextCursor
		if request.Cursor == "" {
			break
		}
	}
	wantPaths := "[directory:%_* directory:[]? directory:foo directory:foobar " +
		"file:root-empty file:\U0010ffff-tail]"
	if fmt.Sprint(paths) != wantPaths {
		t.Fatalf("root entries: %v", paths)
	}
	root, err := a.GetDirectory(t.Context(), GetDirectoryRequest{SnapshotID: source.Id})
	if err != nil || root.FileCount != 8 || root.ContentCount != 3 {
		t.Fatalf("root summary: %+v, %v", root, err)
	}
	if fmt.Sprint(root.RedundancyHistogram) != "[{1 6} {2 2}]" {
		t.Fatalf("distributed partial copies: %+v", root.RedundancyHistogram)
	}
	page, err := a.ListDirectoryEntries(t.Context(), ListDirectoryEntriesRequest{
		Filters: base.DirectoryFilters{SnapshotID: source.Id, Recursive: true},
	})
	if err != nil || len(page.Items) != 8 || page.Items[7].Path != "\U0010ffff-tail" {
		t.Fatalf("recursive Unicode boundary: %+v, %v", page, err)
	}
	one := int64(1)
	page, err = a.ListDirectoryEntries(t.Context(), ListDirectoryEntriesRequest{
		Filters: base.DirectoryFilters{
			SnapshotID: source.Id, Path: "foo", Recursive: true, OtherReplicas: &one,
		},
	})
	if err != nil || len(page.Items) != 1 || page.Items[0].Path != "foo/unknown" {
		t.Fatalf("filtered recursive page: %+v, %v", page, err)
	}
	importFiles("enrich", ",7,sha256,"+fixtureSHA256CD+" known\n", 2)
	detail, err = a.GetDirectory(t.Context(), GetDirectoryRequest{SnapshotID: source.Id, Path: "foo"})
	if err != nil {
		t.Fatal(err)
	}
	if detail.KnownBytes != 15 || detail.UniqueContentKnownBytes != 11 ||
		detail.UnknownSizeFileCount != 0 || detail.UnknownSizeContentCount != 0 {
		t.Fatalf("enriched summary: %+v", detail)
	}

	empty := importFiles("empty", "", 1)
	detail, err = a.GetDirectory(t.Context(), GetDirectoryRequest{SnapshotID: empty.Id})
	if err != nil || detail.FileCount != 0 || detail.ContentCount != 0 ||
		detail.MaxKnownMTime != nil || len(detail.RedundancyHistogram) != 0 {
		t.Fatalf("empty root: %+v, %v", detail, err)
	}
}
