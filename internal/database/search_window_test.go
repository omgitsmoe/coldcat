package database

import (
	"errors"
	"fmt"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/omgitsmoe/coldcat/internal/base"
)

func TestSearchWindowExhaustivePagination(t *testing.T) {
	db, err := Open(filepath.Join(t.TempDir(), "catalog.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	seedSearchPaths(t, db, []string{
		"a/REPORT", "a/report-extra", "b/report", "c/pre-report", "d/Report",
		"e/σσσ", "f/Σςσ", "g/other", "old/report", "z/report",
	})
	_, err = db.db.Exec(`INSERT INTO disk(id,label,capacity) VALUES(2,'second',0);
 INSERT INTO snapshot(id,disk_id,state,captured_at,imported_at,capture_provenance)
 VALUES(2,2,'importing','2024-01-01T00:00:00.000000000Z',
 '2024-01-01T00:00:00.000000000Z','explicit'),
 (3,1,'importing','2025-01-01T00:00:00.000000000Z',
 '2025-01-01T00:00:00.000000000Z','explicit'),
 (4,2,'importing','2026-01-01T00:00:00.000000000Z',
 '2026-01-01T00:00:00.000000000Z','explicit');
 INSERT INTO observation(snapshot_id,content_id,path)
 VALUES(2,1,'a/REPORT'),(2,1,'b/report'),(2,3,'c/pre-report'),
 (3,1,'a/REPORT'),(3,1,'b/report'),(3,5,'d/Report'),(3,6,'e/σσσ'),
 (4,1,'unfinished/report');
 UPDATE snapshot SET state='complete',source_digest=zeroblob(32),
 file_count=(SELECT COUNT(*) FROM observation WHERE snapshot_id=snapshot.id),
 content_count=(SELECT COUNT(DISTINCT content_id) FROM observation WHERE snapshot_id=snapshot.id)
 WHERE id IN (2,3);`)
	if err != nil {
		t.Fatal(err)
	}
	for _, match := range []string{"exact", "substring"} {
		for _, scope := range []base.Scope{base.ScopeCurrent, base.ScopeHistory} {
			for _, query := range []string{"REPORT", "σσσ"} {
				f := base.SearchFilters{Query: query, Field: "name", Match: match,
					ContentFilters: base.ContentFilters{Scope: scope, ReplicaMetric: base.ReplicaDisks}}
				verifySearchWindowPages(t, db, f)
				f.DiskID = 1
				verifySearchWindowPages(t, db, f)
				f.Directory = "a"
				verifySearchWindowPages(t, db, f)
				f.Directory, f.DiskID, f.SnapshotID = "", 0, 1
				verifySearchWindowPages(t, db, f)
			}
		}
		for _, metric := range []base.ReplicaMetric{base.ReplicaDisks, base.ReplicaLocations} {
			for _, bound := range []int64{0, 1, 3, 99} {
				f := base.SearchFilters{Query: "REPORT", Field: "name", Match: match,
					ContentFilters: base.ContentFilters{Scope: base.ScopeCurrent,
						ReplicaMetric: metric, OtherReplicas: &bound}}
				verifySearchWindowPages(t, db, f)
				f.OtherReplicas, f.MinOtherReplicas = nil, &bound
				verifySearchWindowPages(t, db, f)
				f.MinOtherReplicas, f.MaxOtherReplicas = nil, &bound
				verifySearchWindowPages(t, db, f)
			}
		}
	}
}

func verifySearchWindowPages(t *testing.T, db *DB, f base.SearchFilters) {
	t.Helper()
	query, args := searchCandidates(f)
	rows, err := db.db.QueryContext(t.Context(), query+`SELECT id FROM eligible ORDER BY rank,path,id`, args...)
	if err != nil {
		t.Fatal(err)
	}
	var want []base.FileObservationId
	for rows.Next() {
		var id base.FileObservationId
		if err := rows.Scan(&id); err != nil {
			t.Fatal(err)
		}
		want = append(want, id)
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	rows.Close()
	for _, limit := range []int{1, 2, 5} {
		var after base.SearchAnchor
		var got []base.FileObservationId
		for {
			page, err := db.Search(t.Context(), f, limit, after, nil)
			if err != nil {
				t.Fatalf("filters %+v anchor %+v: %v", f, after, err)
			}
			items := page.Items
			if len(items) > limit {
				items = items[:limit]
			}
			for _, item := range items {
				got = append(got, item.Observation.Id)
			}
			if len(got) > len(want) {
				t.Fatalf("pagination repeated observations: %+v", got)
			}
			if len(page.Items) <= limit {
				break
			}
			last := items[len(items)-1]
			after = base.SearchAnchor{ID: last.Observation.Id,
				Path: last.Observation.Path, Relevance: last.Relevance}
		}
		if !slices.Equal(got, want) {
			t.Fatalf("filters %+v limit %d: got %v want %v", f, limit, got, want)
		}
	}
}

func TestSearchAnchorValidationIsPathBounded(t *testing.T) {
	db, err := Open(filepath.Join(t.TempDir(), "catalog.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	seedSearchPaths(t, db, []string{"a/report", "b/report-extra", "c/unrelated"})
	f := base.SearchFilters{Query: "report", Field: "name", Match: "substring",
		ContentFilters: base.ContentFilters{Scope: base.ScopeCurrent, ReplicaMetric: base.ReplicaDisks}}
	after := base.SearchAnchor{ID: 1, Path: "a/report", Relevance: "exact"}
	query, args := searchWindow(f, 0, after)
	text := searchWindowPlan(t, db, query, args)
	if strings.Contains(text, "VIRTUAL TABLE INDEX") ||
		!strings.Contains(text, "sqlite_autoindex_search_path_1 (path=?)") {
		t.Fatalf("anchor validation is not path bounded:\n%s", text)
	}
	for _, match := range []string{"exact", "substring"} {
		f.Match = match
		for _, invalid := range []base.SearchAnchor{
			{ID: 1, Path: "a/report", Relevance: "prefix"},
			{ID: 1, Path: "b/report-extra", Relevance: "prefix"},
			{ID: 3, Path: "c/unrelated", Relevance: "substring"},
		} {
			if _, err := db.Search(t.Context(), f, 1, invalid, nil); !errors.Is(err, ErrValidation) {
				t.Fatalf("accepted invalid anchor %+v (%s): %v", invalid, match, err)
			}
		}
		if _, err := db.Search(t.Context(), f, 1, after, nil); err != nil {
			t.Fatal(err)
		}
		after.Path = ""
		if _, err := db.Search(t.Context(), f, 1, after, nil); err != nil {
			t.Fatal(err)
		}
		after.Path = "a/report"
	}
	f.Match, f.Query = "substring", "unrelated"
	if _, err := db.Search(t.Context(), f, 1, after, nil); !errors.Is(err, ErrValidation) {
		t.Fatalf("accepted anchor outside query: %v", err)
	}
	f.Query, f.OtherReplicas = "report", new(int64(99))
	if _, err := db.Search(t.Context(), f, 1, after, nil); !errors.Is(err, ErrValidation) {
		t.Fatalf("accepted anchor outside replica bounds: %v", err)
	}
}

func TestSearchExactWindowUsesOrderedIndex(t *testing.T) {
	db, err := Open(filepath.Join(t.TempDir(), "catalog.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	seedSearchPaths(t, db, []string{"a/report", "b/report", "c/report"})
	f := base.SearchFilters{Query: "REPORT", Field: "name", Match: "exact",
		ContentFilters: base.ContentFilters{Scope: base.ScopeCurrent, ReplicaMetric: base.ReplicaDisks}}
	for _, after := range []base.SearchAnchor{{}, {ID: 2, Path: "b/report", Relevance: "exact"}} {
		query, args := searchWindow(f, 1, after)
		plan := searchWindowPlan(t, db, query, args)
		if strings.Contains(plan, "USE TEMP B-TREE FOR ORDER BY") ||
			!strings.Contains(plan, "search_name (name_fold=?") {
			t.Fatalf("exact paths are not index ordered:\n%s", plan)
		}
		if after.ID > 0 && !strings.Contains(plan, "search_name (name_fold=? AND path>?)") {
			t.Fatalf("exact keyset did not seek to anchor:\n%s", plan)
		}
	}
}

func searchWindowPlan(t *testing.T, db *DB, query string, args []any) string {
	t.Helper()
	rows, err := db.db.Query("EXPLAIN QUERY PLAN "+query+"SELECT * FROM eligible", args...)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	var plan []string
	for rows.Next() {
		var id, parent, unused int
		var detail string
		if err := rows.Scan(&id, &parent, &unused, &detail); err != nil {
			t.Fatal(err)
		}
		plan = append(plan, detail)
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	return strings.Join(plan, "\n")
}

func BenchmarkSearchBroadWindow(b *testing.B) {
	for _, count := range []int{50000, 1000000} {
		b.Run(fmt.Sprint(count), func(b *testing.B) {
			db, err := Open(filepath.Join(b.TempDir(), "catalog.sqlite"))
			if err != nil {
				b.Fatal(err)
			}
			defer db.Close()
			paths := make([]string, count)
			for i := range paths {
				paths[i] = fmt.Sprintf("docs/%07d/report.txt", i)
			}
			seedSearchPaths(b, db, paths)
			for _, match := range []string{"exact", "substring"} {
				for _, deep := range []bool{false, true} {
					b.Run(fmt.Sprintf("%s/deep=%t", match, deep), func(b *testing.B) {
						f := base.SearchFilters{Query: "REPORT.TXT", Field: "name", Match: match,
							ContentFilters: base.ContentFilters{Scope: base.ScopeCurrent,
								ReplicaMetric: base.ReplicaDisks}}
						var after base.SearchAnchor
						if deep {
							after = base.SearchAnchor{ID: base.FileObservationId(count / 2),
								Path: paths[count/2-1], Relevance: "exact"}
						}
						b.ReportAllocs()
						for b.Loop() {
							page, err := db.Search(b.Context(), f, 50, after, nil)
							if err != nil || len(page.Items) != 51 {
								b.Fatalf("page has %d observations: %v", len(page.Items), err)
							}
						}
					})
				}
			}
		})
	}
}
