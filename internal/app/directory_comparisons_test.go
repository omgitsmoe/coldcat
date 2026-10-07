package app

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/omgitsmoe/coldcat/internal/base"
	"github.com/omgitsmoe/coldcat/internal/database"
)

func TestDirectoryReplicasAndCoverage(t *testing.T) {
	db, err := database.Open(filepath.Join(t.TempDir(), "catalog.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })

	a := New(db)
	importFiles := func(label, input string) base.Snapshot {
		t.Helper()

		disk, err := a.CreateDisk(t.Context(), label, "", "", 100)
		if err != nil {
			t.Fatal(err)
		}

		file := filepath.Join(t.TempDir(), label+".cshd")
		if err := os.WriteFile(file, []byte("# version 1\n"+input), 0600); err != nil {
			t.Fatal(err)
		}

		snapshot, err := a.Import(t.Context(), ImportRequest{
			DiskID: disk, Path: file, CapturedAt: time.Unix(1, 0),
		})
		if err != nil {
			t.Fatal(err)
		}

		return snapshot
	}

	source := importFiles("source",
		"10,4,sha256,"+fixtureSHA256AB+" source/a.txt\n"+
			"11,,sha256,"+fixtureSHA256CD+" source/nested/b.txt\n"+
			"12,2,sha256,"+fixtureSHA256EF+" source/debug.log\n"+
			"13,0,sha256,"+fixtureSHA256FF+" source/literal*.txt\n"+
			"20,4,sha256,"+fixtureSHA256AB+" whole-copy/a.txt\n"+
			"21,,sha256,"+fixtureSHA256CD+" whole-copy/nested/b.txt\n"+
			"22,2,sha256,"+fixtureSHA256EF+" whole-copy/debug.log\n"+
			"23,0,sha256,"+fixtureSHA256FF+" whole-copy/literal*.txt\n"+
			",4,sha256,"+fixtureSHA256AB+" extra/a.txt\n"+
			",,sha256,"+fixtureSHA256CD+" extra/nested/b.txt\n"+
			",2,sha256,"+fixtureSHA256EF+" extra/debug.log\n"+
			",0,sha256,"+fixtureSHA256FF+" extra/literal*.txt\n"+
			",1,sha256,"+fixtureSHA256DD+" extra/additional.txt\n"+
			",4,sha256,"+fixtureSHA256AB+" missing/a.txt\n"+
			",4,sha256,"+fixtureSHA256AB+" rearranged/nested/a.txt\n"+
			",,sha256,"+fixtureSHA256CD+" rearranged/b.txt\n"+
			",2,sha256,"+fixtureSHA256EF+" rearranged/debug.log\n"+
			",0,sha256,"+fixtureSHA256FF+" rearranged/literal*.txt\n"+
			",4,md5,"+fixtureMD5AA+" algorithm/a.txt\n"+
			",,md5,"+fixtureMD5AB+" algorithm/nested/b.txt\n"+
			",2,sha256,"+fixtureSHA256EF+" algorithm/debug.log\n"+
			",0,sha256,"+fixtureSHA256FF+" algorithm/literal*.txt\n")

	copy := importFiles("copy",
		"30,4,sha256,"+fixtureSHA256AB+" renamed/a.txt\n"+
			"31,,sha256,"+fixtureSHA256CD+" renamed/nested/b.txt\n"+
			"32,2,sha256,"+fixtureSHA256EE+" renamed/debug.log\n"+
			"33,0,sha256,"+fixtureSHA256FF+" renamed/literal*.txt\n")

	fullCoverage := importFiles("coverage",
		",4,sha256,"+fixtureSHA256AB+" elsewhere/one\n"+
			",,sha256,"+fixtureSHA256CD+" unrelated/two\n"+
			",0,sha256,"+fixtureSHA256FF+" wildcard/three\n")

	partial := importFiles("partial", ",4,sha256,"+fixtureSHA256AB+" only/a\n")

	page, err := a.ListDirectoryReplicas(t.Context(), ListDirectoryComparisonsRequest{
		Filters: base.DirectoryComparisonFilters{SnapshotID: source.Id, Path: "source"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(page.Items) != 1 || page.Items[0].Path != "whole-copy" ||
		!page.Items[0].SameDisk || !page.Items[0].WholeTreeEqual {
		t.Fatalf("whole-tree replicas: %+v", page)
	}

	page, err = a.ListDirectoryReplicas(t.Context(), ListDirectoryComparisonsRequest{
		Filters: base.DirectoryComparisonFilters{
			SnapshotID: source.Id,
			Path:       "source",
			Block:      []string{"**/*.log"},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(page.Items) != 2 || page.Selection.RetainedFileCount != 3 ||
		page.Selection.ExcludedFileCount != 1 || page.Items[1].Disk.Id != copy.DiskId ||
		page.Items[1].WholeTreeEqual || page.Items[1].ExcludedFileCount != 1 {
		t.Fatalf("filtered replicas: %+v", page)
	}

	escapedRequest := ListDirectoryComparisonsRequest{
		Filters: base.DirectoryComparisonFilters{
			SnapshotID: source.Id,
			Path:       "source",
			Allow:      []string{`literal\*.txt`},
		},
		Limit: 2,
	}

	foundRenamed := false
	itemCount := 0
	var escaped base.DirectoryReplicaPage
	for {
		escaped, err = a.ListDirectoryReplicas(t.Context(), escapedRequest)
		if err != nil {
			t.Fatal(err)
		}

		for _, item := range escaped.Items {
			foundRenamed = foundRenamed || item.Path == "renamed"
			itemCount++
		}

		escapedRequest.Cursor = escaped.NextCursor
		if escapedRequest.Cursor == "" {
			break
		}
	}
	if escaped.Selection.RetainedFileCount != 1 || !foundRenamed || itemCount != 5 {
		t.Fatalf("escaped wildcard: %+v", escaped)
	}

	empty, err := a.ListDirectoryReplicas(t.Context(), ListDirectoryComparisonsRequest{
		Filters: base.DirectoryComparisonFilters{
			SnapshotID: source.Id,
			Path:       "source",
			Allow:      []string{"absent/**"},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if !empty.Selection.EmptyComparison || len(empty.Items) != 0 {
		t.Fatalf("empty comparison: %+v", empty)
	}

	request := ListDirectoryComparisonsRequest{
		Filters: base.DirectoryComparisonFilters{
			SnapshotID: source.Id,
			Path:       "source",
			Block:      []string{"**/*.log"},
		},
		Limit: 2,
	}
	coverage, err := a.ListDirectoryCoverage(t.Context(), request)
	if err != nil {
		t.Fatal(err)
	}
	if len(coverage.Items) != 2 || coverage.NextCursor == "" ||
		coverage.Items[0].Disk.Id != copy.DiskId || !coverage.Items[0].Complete ||
		coverage.Items[1].Disk.Id != fullCoverage.DiskId || !coverage.Items[1].Complete {
		t.Fatalf("coverage first page: %+v", coverage)
	}
	firstCursor := coverage.NextCursor

	changed := request
	changed.Filters.Block = nil
	changed.Cursor = firstCursor
	_, err = a.ListDirectoryCoverage(t.Context(), changed)
	if !errors.Is(err, database.ErrValidation) {
		t.Fatalf("changed cursor filters: %v", err)
	}

	request.Cursor = coverage.NextCursor
	coverage, err = a.ListDirectoryCoverage(t.Context(), request)
	if err != nil {
		t.Fatal(err)
	}
	if len(coverage.Items) != 1 || coverage.Items[0].Disk.Id != partial.DiskId ||
		coverage.Items[0].Complete || coverage.Items[0].CoveredFileCount != 1 ||
		coverage.Items[0].MissingFileCount != 2 || coverage.Items[0].CoveredKnownBytes != 4 ||
		coverage.Items[0].MissingUnknownSizeFiles != 1 {
		t.Fatalf("partial coverage: %+v", coverage)
	}

	importFiles("new", ",4,sha256,"+fixtureSHA256AB+" new/a\n")
	request.Cursor = firstCursor
	_, err = a.ListDirectoryCoverage(t.Context(), request)
	if !errors.Is(err, database.ErrStaleCursor) {
		t.Fatalf("published import preserved cursor: %v", err)
	}
}
