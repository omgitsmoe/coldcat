package app

import (
	"context"
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"testing"
	"time"

	"github.com/omgitsmoe/coldcat/internal/base"
	"github.com/omgitsmoe/coldcat/internal/database"
)

func TestContentListsAndRedundancy(t *testing.T) {
	path := filepath.Join(t.TempDir(), "catalog.sqlite")
	db, err := database.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	a := New(db)
	var disks []base.DiskId
	for _, label := range []string{"source", "second", "third", "empty"} {
		id, err := a.CreateDisk(t.Context(), label, "", "", 0)
		if err != nil {
			t.Fatal(err)
		}
		disks = append(disks, id)
	}
	importFiles := func(disk base.DiskId, capture int64, input string, success bool) {
		t.Helper()
		file := filepath.Join(t.TempDir(), "fixture.cshd")
		if err := os.WriteFile(file, []byte(input), 0600); err != nil {
			t.Fatal(err)
		}
		_, err := a.Import(t.Context(), ImportRequest{
			DiskID: disk, CapturedAt: time.Unix(capture, 0), Path: file,
		})
		if (err == nil) != success {
			t.Fatalf("import: %v", err)
		}
	}
	importFiles(disks[0], 10, ",sha256,ee foo/gone\n,sha256,dd old\n", true)
	files := "# version 1\n,,sha256,aa foo/a\n,,sha256,bb foo/b\n" +
		",,sha256,bb backup/b\n,,sha256,cc foo/nested/c\n,0,sha256,dd foo/d\n" +
		",0,sha256,dd backup/d\n,,md5,aa foobar/other\n" +
		",,sha256,ff foo%_/é\\name\n"
	importFiles(disks[0], 20, files, true)
	importFiles(disks[1], 20, ",sha256,cc elsewhere/c\n,sha256,dd elsewhere/d\n", true)
	importFiles(disks[2], 20, ",sha256,dd other/d\n", true)
	importFiles(disks[0], 15, ",sha256,ee foo/older\n", true)
	importFiles(disks[0], 20, files+",,sha256,aa second/a\n", true)
	list := func(f base.ContentFilters) base.ContentPage {
		t.Helper()
		page, err := a.ListContents(t.Context(), ListContentsRequest{Filters: f})
		if err != nil {
			t.Fatal(err)
		}
		for _, item := range page.Items {
			summary, err := a.GetContentSummary(t.Context(), item.Content.Id, page.Filters.Scope)
			if err != nil || !reflect.DeepEqual(item, summary) {
				t.Fatalf("summary mismatch: %+v %+v %v", item, summary, err)
			}
		}
		return page
	}
	current := list(base.ContentFilters{})
	if len(current.Items) != 6 {
		t.Fatalf("current: %+v", current)
	}
	history := list(base.ContentFilters{Scope: base.ScopeHistory})
	if len(history.Items) != 7 {
		t.Fatalf("history: %+v", history)
	}
	for _, item := range history.Items {
		if item.Content.Hash[0] == 0xee && item.CurrentLocationCount != 0 {
			t.Fatal("historical-only content has current locations")
		}
	}
	for _, test := range []struct {
		metric base.ReplicaMetric
		n      int64
		want   int
	}{
		{base.ReplicaDisks, 0, 4}, {base.ReplicaDisks, 1, 1}, {base.ReplicaDisks, 2, 1},
		{base.ReplicaLocations, 0, 2}, {base.ReplicaLocations, 1, 3},
		{base.ReplicaLocations, 3, 1},
	} {
		page := list(base.ContentFilters{ReplicaMetric: test.metric, OtherReplicas: &test.n})
		if len(page.Items) != test.want {
			t.Fatalf("%s=%d: %+v", test.metric, test.n, page)
		}
	}
	min, max := int64(1), int64(2)
	if len(list(base.ContentFilters{MinOtherReplicas: &min, MaxOtherReplicas: &max}).Items) != 2 {
		t.Fatal("inclusive bounds")
	}
	page := list(base.ContentFilters{DiskID: disks[0], Directory: "foo", OtherReplicas: &max})
	if len(page.Items) != 1 || page.Items[0].DiskCount != 3 || page.Items[0].LocationCount != 4 ||
		page.Items[0].Content.Size == nil || *page.Items[0].Content.Size != 0 {
		t.Fatalf("filtered global counts: %+v", page)
	}
	if len(list(base.ContentFilters{DiskID: disks[0], Directory: "foo"}).Items) != 4 {
		t.Fatal("directory segment boundary")
	}
	literal := list(base.ContentFilters{DiskID: disks[0], Directory: "foo%_"})
	if len(literal.Items) != 1 || literal.Items[0].Content.Size != nil {
		t.Fatalf("literal path/unknown size: %+v", literal)
	}
	for _, f := range []base.ContentFilters{
		{DiskID: disks[3]}, {DiskID: disks[0], Directory: "missing"},
	} {
		if len(list(f).Items) != 0 {
			t.Fatal("expected empty page")
		}
	}
	if _, err := a.ListContents(t.Context(), ListContentsRequest{
		Filters: base.ContentFilters{DiskID: 999},
	}); !errors.Is(err, database.ErrNotFound) {
		t.Fatalf("missing disk: %v", err)
	}
	for _, f := range []base.ContentFilters{
		{Scope: "invalid"}, {ReplicaMetric: "invalid"}, {DiskID: -1},
		{Directory: "foo"}, {DiskID: disks[0], Directory: "../foo"},
		{DiskID: disks[0], Directory: "foo//bar"},
		{Scope: base.ScopeHistory, OtherReplicas: &min},
		{OtherReplicas: &min, MaxOtherReplicas: &max},
		{MinOtherReplicas: &max, MaxOtherReplicas: &min},
	} {
		if _, err := a.ListContents(t.Context(), ListContentsRequest{Filters: f}); !errors.Is(err, database.ErrValidation) {
			t.Fatalf("accepted %+v: %v", f, err)
		}
	}
	cancelled, cancel := context.WithCancel(t.Context())
	cancel()
	if _, err := a.ListContents(cancelled, ListContentsRequest{}); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancellation: %v", err)
	}

	req := ListContentsRequest{Limit: 1}
	first, err := a.ListContents(t.Context(), req)
	if err != nil || first.NextCursor == "" {
		t.Fatalf("first: %+v %v", first, err)
	}
	req.Cursor = first.NextCursor
	bad := req
	bad.Filters.Scope = base.ScopeHistory
	if _, err := a.ListContents(t.Context(), bad); !errors.Is(err, database.ErrValidation) {
		t.Fatalf("filter mismatch: %v", err)
	}
	var cursor contentCursor
	data, err := base64.RawURLEncoding.DecodeString(req.Cursor)
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(data, &cursor); err != nil {
		t.Fatal(err)
	}
	for _, mutate := range []func(*contentCursor){
		func(c *contentCursor) { c.After = 999 },
		func(c *contentCursor) { c.Kind = "observations" },
		func(c *contentCursor) { c.Version = 2 },
		func(c *contentCursor) { c.After = 0 },
	} {
		copy := cursor
		mutate(&copy)
		data, err := json.Marshal(copy)
		if err != nil {
			t.Fatal(err)
		}
		bad := req
		bad.Cursor = base64.RawURLEncoding.EncodeToString(data)
		if _, err := a.ListContents(t.Context(), bad); !errors.Is(err, database.ErrValidation) {
			t.Fatalf("invalid cursor: %v", err)
		}
	}
	for _, text := range []string{"!", "{}", string(data) + " {}",
		string(data[:len(data)-1]) + `,"unknown":1}`} {
		bad := req
		bad.Cursor = base64.RawURLEncoding.EncodeToString([]byte(text))
		if _, err := a.ListContents(t.Context(), bad); !errors.Is(err, database.ErrValidation) {
			t.Fatalf("malformed cursor accepted: %v", err)
		}
	}
	importFiles(disks[0], 30, ",sha256,12 failed\ninvalid\n", false)
	raw, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	_, seedErr := raw.Exec(`INSERT INTO snapshot(
 id,disk_id,state,captured_at,imported_at,capture_provenance)
 VALUES(1000,?,'importing','2024-01-01T00:00:00.000000000Z',
 '2024-01-01T00:00:00.000000000Z','explicit');
 INSERT INTO content(id,hash_type,hash) VALUES(1000,'sha256',X'99');
 INSERT INTO observation(snapshot_id,content_id,path) VALUES(1000,1000,'unfinished');
 INSERT INTO import_content(snapshot_id,content_id) VALUES(1000,1000);`, disks[0])
	closeErr := raw.Close()
	if seedErr != nil || closeErr != nil {
		t.Fatalf("seed interruption: %v %v", seedErr, closeErr)
	}
	db.Close()
	db, err = database.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	a = New(db)
	seen := map[base.ContentId]bool{first.Items[0].Content.Id: true}
	for req.Cursor != "" {
		page, err := a.ListContents(t.Context(), req)
		if err != nil || len(page.Items) != 1 {
			t.Fatalf("next: %+v %v", page, err)
		}
		id := page.Items[0].Content.Id
		if seen[id] {
			t.Fatal("duplicate content")
		}
		seen[id] = true
		req.Cursor = page.NextCursor
	}
	if len(seen) != 6 {
		t.Fatalf("pagination: %v", seen)
	}
	importFiles(disks[0], 5, ",sha256,13 older-only\n", true)
	req.Cursor = first.NextCursor
	if _, err := a.ListContents(t.Context(), req); !errors.Is(err, database.ErrStaleCursor) {
		t.Fatalf("older import did not invalidate cursor: %v", err)
	}
}
