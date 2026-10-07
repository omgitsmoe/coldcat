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
	"strings"
	"testing"
	"time"

	"github.com/omgitsmoe/coldcat/internal/base"
	"github.com/omgitsmoe/coldcat/internal/database"
)

func TestSearchMatchingScopesAndReplicas(t *testing.T) {
	db, err := database.Open(filepath.Join(t.TempDir(), "catalog.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	a := New(db)
	var disks []base.DiskId
	for _, label := range []string{"one", "two", "three"} {
		id, err := a.CreateDisk(t.Context(), label, "", "", 0)
		if err != nil {
			t.Fatal(err)
		}
		disks = append(disks, id)
	}
	importFiles := func(disk base.DiskId, captured int64, files string) base.Snapshot {
		t.Helper()
		file := filepath.Join(t.TempDir(), "input.cshd")
		if err := os.WriteFile(file, []byte("# version 1\n"+files), 0600); err != nil {
			t.Fatal(err)
		}
		snapshot, err := a.Import(t.Context(), ImportRequest{
			DiskID: disk, CapturedAt: time.Unix(captured, 0), Path: file,
		})
		if err != nil {
			t.Fatal(err)
		}
		return snapshot
	}
	old := importFiles(disks[0], 10, ",,sha256,"+fixtureSHA256BB+" gone/report\n")
	files := ",0,sha256," + fixtureSHA256AA + " foo/report\n" +
		",0,sha256," + fixtureSHA256AA + " backup/report\n" +
		",,sha256," + fixtureSHA256CC + " foo/report.txt\n" +
		",,sha256," + fixtureSHA256CC + " foobar/xreport\n" +
		",,sha256," + fixtureSHA256DD + " foo%_/é猫%_\\\"[*]?\n" +
		",,sha256," + fixtureSHA256FF + " unicode/e\u0301\n" +
		",,sha256," + fixtureSHA256FF + " unicode/É\n"
	importFiles(disks[0], 20, files)
	latest := importFiles(disks[0], 20, files+",,sha256,"+fixtureSHA256EE+" foo/Report\n")
	importFiles(disks[0], 15, ",,sha256,"+fixtureSHA256BB+" late/report\n")
	importFiles(disks[1], 20, ",0,sha256,"+fixtureSHA256AA+" other/renamed\n")
	importFiles(disks[2], 20, ",0,sha256,"+fixtureSHA256AA+" copies/report\n")

	search := func(f base.SearchFilters) base.SearchPage {
		t.Helper()
		page, err := a.Search(t.Context(), SearchRequest{Filters: f})
		if err != nil {
			t.Fatal(err)
		}
		for _, item := range page.Items {
			want, err := a.GetContentSummary(t.Context(), item.Content.Content.Id, page.Filters.Scope)
			if err != nil || !reflect.DeepEqual(want, item.Content) {
				t.Fatalf("summary: %+v != %+v: %v", want, item.Content, err)
			}
		}
		return page
	}
	current := search(base.SearchFilters{Query: "report"})
	var paths, relevance []string
	for _, item := range current.Items {
		paths = append(paths, item.Observation.Path)
		relevance = append(relevance, item.Relevance)
		if !item.IsCurrent {
			t.Fatal("historical current match")
		}
	}
	if !reflect.DeepEqual(paths, []string{
		"backup/report", "copies/report", "foo/Report", "foo/report", "foo/report.txt", "foobar/xreport",
	}) || !reflect.DeepEqual(relevance, []string{"exact", "exact", "exact", "exact", "prefix", "substring"}) {
		t.Fatalf("ranking: %v %v", paths, relevance)
	}
	if current.Items[0].Content.CurrentDiskCount != 3 ||
		current.Items[0].Content.CurrentLocationCount != 4 {
		t.Fatalf("replicas: %+v", current.Items[0])
	}
	for _, test := range []struct {
		f    base.SearchFilters
		want int
	}{
		{base.SearchFilters{Query: "REPORT"}, 6},
		{base.SearchFilters{Query: "REPORT", ContentFilters: base.ContentFilters{
			Scope: base.ScopeHistory}}, 12},
		{base.SearchFilters{Query: "REPORT", ContentFilters: base.ContentFilters{
			DiskID: disks[0], Directory: "foo"}}, 3},
		{base.SearchFilters{Query: "REPORT", SnapshotID: old.Id}, 0},
		{base.SearchFilters{Query: "REPORT", SnapshotID: old.Id,
			ContentFilters: base.ContentFilters{Scope: base.ScopeHistory}}, 1},
	} {
		page := search(test.f)
		if len(page.Items) != test.want {
			t.Fatalf("folded %+v: got %d want %d", test.f, len(page.Items), test.want)
		}
		if test.f.SnapshotID == old.Id && len(page.Items) > 0 &&
			page.Items[0].Content.CurrentLocationCount != 0 {
			t.Fatal("historical-only match has current locations")
		}
	}
	for _, metric := range []base.ReplicaMetric{base.ReplicaDisks, base.ReplicaLocations} {
		bound := int64(2)
		if metric == base.ReplicaLocations {
			bound = 3
		}
		page := search(base.SearchFilters{Query: "REPORT",
			ContentFilters: base.ContentFilters{ReplicaMetric: metric, OtherReplicas: &bound}})
		if len(page.Items) != 3 {
			t.Fatalf("folded replica filter %s: %+v", metric, page.Items)
		}
	}
	for _, scope := range []base.Scope{base.ScopeCurrent, base.ScopeHistory} {
		request := SearchRequest{Filters: base.SearchFilters{Query: "report",
			ContentFilters: base.ContentFilters{Scope: scope}}, Limit: 1}
		var paged []base.SearchItem
		for {
			page, err := a.Search(t.Context(), request)
			if err != nil {
				t.Fatal(err)
			}
			paged = append(paged, page.Items...)
			if page.NextCursor == "" {
				break
			}
			request.Cursor = page.NextCursor
		}
		whole := search(request.Filters)
		if !reflect.DeepEqual(paged, whole.Items) {
			t.Fatalf("%s: paginated results differ from whole result", scope)
		}
	}
	for _, test := range []struct {
		f    base.SearchFilters
		want int
	}{
		{base.SearchFilters{Query: "report", Match: "exact"}, 4},
		{base.SearchFilters{Query: "foo/report", Field: "path", Match: "exact"}, 2},
		{base.SearchFilters{Query: "foo/report", Field: "path"}, 3},
		{base.SearchFilters{Query: "Report"}, 6},
		{base.SearchFilters{Query: "é", Match: "exact"}, 1},
		{base.SearchFilters{Query: "é猫%"}, 1},
		{base.SearchFilters{Query: "e\u0301", Match: "exact"}, 1},
		{base.SearchFilters{Query: "É", Match: "exact"}, 1},
		{base.SearchFilters{Query: "%_\\"}, 1},
		{base.SearchFilters{Query: "\\\"["}, 1},
		{base.SearchFilters{Query: "[*]?"}, 1},
		{base.SearchFilters{Query: " foo"}, 0},
		{base.SearchFilters{Query: "no-such-path"}, 0},
		{base.SearchFilters{Query: "report", ContentFilters: base.ContentFilters{
			DiskID: disks[0], Directory: "foo"}}, 3},
		{base.SearchFilters{Query: "report", SnapshotID: latest.Id,
			ContentFilters: base.ContentFilters{Directory: "foo"}}, 3},
		{base.SearchFilters{Query: "report", SnapshotID: old.Id}, 0},
		{base.SearchFilters{Query: "report", SnapshotID: old.Id,
			ContentFilters: base.ContentFilters{Scope: base.ScopeHistory}}, 1},
		{base.SearchFilters{Query: "report", ContentFilters: base.ContentFilters{
			Scope: base.ScopeHistory}}, 12},
	} {
		page := search(test.f)
		if len(page.Items) != test.want {
			t.Fatalf("%+v: got %d want %d", test.f, len(page.Items), test.want)
		}
	}
	history := search(base.SearchFilters{Query: "gone/report", Field: "path",
		ContentFilters: base.ContentFilters{Scope: base.ScopeHistory}})
	if history.Items[0].IsCurrent || history.Items[0].Content.CurrentLocationCount != 0 {
		t.Fatal("historical-only content has current locations")
	}
	for _, test := range []struct {
		metric base.ReplicaMetric
		n      int64
		want   int
	}{
		{base.ReplicaDisks, 2, 3}, {base.ReplicaLocations, 3, 3},
		{base.ReplicaDisks, 0, 3}, {base.ReplicaLocations, 0, 1},
	} {
		page := search(base.SearchFilters{Query: "report", ContentFilters: base.ContentFilters{
			ReplicaMetric: test.metric, OtherReplicas: &test.n,
		}})
		if len(page.Items) != test.want {
			t.Fatalf("%s/%d: %+v", test.metric, test.n, page)
		}
		page = search(base.SearchFilters{Query: "report", ContentFilters: base.ContentFilters{
			ReplicaMetric: test.metric, MinOtherReplicas: &test.n, MaxOtherReplicas: &test.n,
		}})
		if len(page.Items) != test.want {
			t.Fatal("inclusive bounds")
		}
	}
}

func TestSearchCursorDoesNotEmbedLongImportedPaths(t *testing.T) {
	for _, match := range []string{"substring", "exact"} {
		t.Run(match, func(t *testing.T) { testSearchLongPaths(t, match) })
	}
}

func testSearchLongPaths(t *testing.T, match string) {
	db, err := database.Open(filepath.Join(t.TempDir(), "catalog.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	a := New(db)
	disk, err := a.CreateDisk(t.Context(), "disk", "", "", 0)
	if err != nil {
		t.Fatal(err)
	}
	longPath := strings.Repeat("a", 20000) + "/report"
	input := ",sha256," + fixtureSHA256AA + " " + longPath + "\n" +
		",sha256," + fixtureSHA256AA + " b/report\n"
	file := filepath.Join(t.TempDir(), "input.cshd")
	if err := os.WriteFile(file, []byte(input), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := a.Import(t.Context(), ImportRequest{
		DiskID: disk, Path: file, CapturedAt: time.Unix(10, 0),
	}); err != nil {
		t.Fatal(err)
	}
	req := SearchRequest{Filters: base.SearchFilters{Query: "report", Match: match}, Limit: 1}
	first, err := a.Search(t.Context(), req)
	if err != nil || first.Items[0].Observation.Path != longPath ||
		len(first.NextCursor) > 16384 || first.NextCursor == "" {
		t.Fatalf("long-path cursor: %v", err)
	}
	req.Cursor = first.NextCursor
	second, err := a.Search(t.Context(), req)
	if err != nil || len(second.Items) != 1 || second.Items[0].Observation.Path != "b/report" {
		t.Fatalf("long-path pagination: %+v %v", second, err)
	}
}

func TestSearchPaginationRecoveryAndValidation(t *testing.T) {
	for _, match := range []string{"substring"} {
		t.Run(match, func(t *testing.T) { testSearchRecovery(t, match) })
	}
}

func testSearchRecovery(t *testing.T, match string) {
	path := filepath.Join(t.TempDir(), "catalog.sqlite")
	db, err := database.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	a := New(db)
	disk, err := a.CreateDisk(t.Context(), "disk", "", "", 0)
	if err != nil {
		t.Fatal(err)
	}
	file := filepath.Join(t.TempDir(), "input.cshd")
	input := "# version 1\n,,sha256," + fixtureSHA256AA + " a/report\n" +
		",,sha256," + fixtureSHA256AA + " b/report\n" +
		",,sha256," + fixtureSHA256BB + " c/report.txt\n"
	if err := os.WriteFile(file, []byte(input), 0600); err != nil {
		t.Fatal(err)
	}
	reqImport := ImportRequest{DiskID: disk, Path: file, CapturedAt: time.Unix(10, 0)}
	if _, err := a.Import(t.Context(), reqImport); err != nil {
		t.Fatal(err)
	}
	req := SearchRequest{Filters: base.SearchFilters{Query: "report", Match: match}, Limit: 1}
	first, err := a.Search(t.Context(), req)
	if err != nil || first.NextCursor == "" {
		t.Fatalf("first: %+v %v", first, err)
	}
	req.Cursor = first.NextCursor
	second, err := a.Search(t.Context(), req)
	if err != nil || second.Items[0].Observation.Path != "b/report" {
		t.Fatalf("second: %+v %v", second, err)
	}
	thirdReq := req
	thirdReq.Cursor = second.NextCursor
	third, err := a.Search(t.Context(), thirdReq)
	if err != nil || third.Items[0].Observation.Path != "c/report.txt" || third.NextCursor != "" {
		t.Fatalf("third: %+v %v", third, err)
	}
	for _, f := range []base.SearchFilters{
		{}, {Query: "x"}, {Query: "xy"}, {Query: "é猫"},
		{Query: "report", Match: "fuzzy"},
		{Query: "report", Field: "invalid"}, {Query: "report", Match: "invalid"},
		{Query: "\xff"}, {Query: "x\x00"}, {Query: "x", SnapshotID: -1},
		{Query: "x", ContentFilters: base.ContentFilters{Directory: "foo"}},
		{Query: "x", ContentFilters: base.ContentFilters{DiskID: disk, Directory: "../foo"}},
		{Query: "x", ContentFilters: base.ContentFilters{Scope: base.ScopeHistory,
			OtherReplicas: new(int64)}},
	} {
		if _, err := a.Search(t.Context(), SearchRequest{Filters: f}); !errors.Is(err, database.ErrValidation) {
			t.Fatalf("accepted %+v: %v", f, err)
		}
	}
	changed := req
	changed.Filters.Query = "report.txt"
	if _, err := a.Search(t.Context(), changed); !errors.Is(err, database.ErrValidation) {
		t.Fatalf("changed context: %v", err)
	}
	data, _ := base64.RawURLEncoding.DecodeString(req.Cursor)
	var cursor searchCursor
	if err := json.Unmarshal(data, &cursor); err != nil {
		t.Fatal(err)
	}
	for _, version := range []int{1, 2} {
		obsolete := cursor
		obsolete.Version = version
		obsolete.Kind = "search:rank:path:id"
		if version == 2 {
			obsolete.Kind = "search:fuzzy-v1:rank:path:id"
		}
		obsoleteData, err := json.Marshal(obsolete)
		if err != nil {
			t.Fatal(err)
		}
		obsoleteReq := req
		obsoleteReq.Cursor = base64.RawURLEncoding.EncodeToString(obsoleteData)
		if _, err := a.Search(t.Context(), obsoleteReq); !errors.Is(err, database.ErrValidation) {
			t.Fatalf("accepted obsolete search cursor v%d: %v", version, err)
		}
	}
	cursor.After.Path = "forged"
	data, _ = json.Marshal(cursor)
	changed = req
	changed.Cursor = base64.RawURLEncoding.EncodeToString(data)
	if _, err := a.Search(t.Context(), changed); !errors.Is(err, database.ErrValidation) {
		t.Fatalf("forged anchor: %v", err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if _, err := a.Search(ctx, SearchRequest{Filters: req.Filters}); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancellation: %v", err)
	}
	if _, err := a.Import(t.Context(), reqImport); !errors.Is(err, database.ErrConflict) {
		t.Fatalf("duplicate: %v", err)
	}
	if _, err := a.Search(t.Context(), req); err != nil {
		t.Fatalf("duplicate invalidated cursor: %v", err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	raw, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	_, err = raw.Exec(`PRAGMA foreign_keys=ON;
 INSERT INTO snapshot(id,disk_id,state,captured_at,imported_at,capture_provenance)
 VALUES(100,1,'importing','2023-01-01T00:00:00.000000000Z',
 '2023-01-01T00:00:00.000000000Z','explicit');
 INSERT INTO observation(snapshot_id,content_id,path) VALUES(100,1,'unfinished/report');`)
	raw.Close()
	if err != nil {
		t.Fatal(err)
	}
	db, err = database.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	a = New(db)
	restarted, err := a.Search(t.Context(), req)
	if err != nil || !reflect.DeepEqual(second, restarted) {
		t.Fatalf("restart/recovery: %+v %v", restarted, err)
	}
	reqImport.AllowRepeat = true
	if _, err := a.Import(t.Context(), reqImport); err != nil {
		t.Fatal(err)
	}
	if _, err := a.Search(t.Context(), req); !errors.Is(err, database.ErrStaleCursor) {
		t.Fatalf("publication: %v", err)
	}
}
