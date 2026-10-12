package database

import (
	"cmp"
	"errors"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/omgitsmoe/coldcat/internal/base"
)

func TestSearchMatchAll(t *testing.T) {
	db, err := Open(filepath.Join(t.TempDir(), "catalog.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	paths := []string{"a", "dir/*", "dir/é", "other/unrelated", "雪"}
	seedSearchPaths(t, db, paths)
	for _, mode := range []string{"syntax", "whole"} {
		for _, field := range []string{"name", "path"} {
			for _, query := range []string{"*", " \t*\n", "\u2003*\u2003"} {
				f := base.SearchFilters{Query: query, Field: field, Match: mode,
					ContentFilters: base.ContentFilters{Scope: base.ScopeCurrent,
						ReplicaMetric: base.ReplicaDisks}}
				page, err := db.Search(t.Context(), f, 50, base.SearchAnchor{}, nil)
				if err != nil {
					t.Fatal(err)
				}
				var got []string
				for _, item := range page.Items {
					got = append(got, item.Observation.Path)
					if item.Relevance != "match" {
						t.Fatalf("unexpected relevance: %s", item.Relevance)
					}
				}
				if !slices.Equal(got, paths) {
					t.Fatalf("%s/%s/%q: got %q want %q", mode, field, query, got, paths)
				}
				verifySearchWindowPages(t, db, f)
				querySQL, args, err := searchWindow(f, 50, base.SearchAnchor{})
				if err != nil {
					t.Fatal(err)
				}
				if plan := searchWindowPlan(t, db, querySQL, args); strings.Contains(plan, "VIRTUAL TABLE INDEX") {
					t.Fatalf("match-all used trigram lookup:\n%s", plan)
				}
			}
		}
	}
	f := base.SearchFilters{Query: "*", Field: "name", Match: "exact",
		ContentFilters: base.ContentFilters{Scope: base.ScopeCurrent,
			ReplicaMetric: base.ReplicaDisks}}
	page, err := db.Search(t.Context(), f, 50, base.SearchAnchor{}, nil)
	if err != nil || len(page.Items) != 1 || page.Items[0].Observation.Path != "dir/*" {
		t.Fatalf("literal exact asterisk: %+v, %v", page.Items, err)
	}
	for _, mode := range []string{"syntax", "whole", "wildcard", "expression", "substring"} {
		for _, query := range []string{"**", "?", "(*)", "* OR report", "* AND report", `"*"`, `\*`} {
			f.Match, f.Query = mode, query
			if mode == "wildcard" && strings.Contains(query, "report") {
				continue
			}
			if mode == "substring" && len(query) >= 3 {
				continue
			}
			if err := ValidateSearchFilters(f); !errors.Is(err, ErrValidation) {
				t.Fatalf("accepted %s/%q: %v", mode, query, err)
			}
		}
		if mode != "syntax" && mode != "whole" {
			f.Query = "*"
			if err := ValidateSearchFilters(f); !errors.Is(err, ErrValidation) {
				t.Fatalf("accepted %s match-all: %v", mode, err)
			}
		}
	}
}

func TestSearchMatchAllFilters(t *testing.T) {
	db, err := Open(filepath.Join(t.TempDir(), "catalog.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	seedSearchReplicaHistory(t, db, 12, 2, 2)
	for _, filters := range []base.SearchFilters{
		{ContentFilters: base.ContentFilters{Scope: base.ScopeCurrent}},
		{ContentFilters: base.ContentFilters{Scope: base.ScopeHistory}},
		{ContentFilters: base.ContentFilters{Scope: base.ScopeHistory, DiskID: 1}, SnapshotID: 1},
		{ContentFilters: base.ContentFilters{Scope: base.ScopeCurrent, DiskID: 1,
			Directory: "copy-1", OtherReplicas: new(int64(1))}},
		{ContentFilters: base.ContentFilters{Scope: base.ScopeCurrent,
			OtherReplicas: new(int64(0))}},
		{ContentFilters: base.ContentFilters{Scope: base.ScopeCurrent,
			MinOtherReplicas: new(int64(1)), MaxOtherReplicas: new(int64(3))}},
	} {
		for _, metric := range []base.ReplicaMetric{base.ReplicaDisks, base.ReplicaLocations} {
			filters.ReplicaMetric = metric
			filters.Query, filters.Field, filters.Match = "report", "name", "substring"
			want, err := db.Search(t.Context(), filters, 200, base.SearchAnchor{}, nil)
			if err != nil {
				t.Fatal(err)
			}
			slices.SortFunc(want.Items, func(a, b base.SearchItem) int {
				if order := strings.Compare(a.Observation.Path, b.Observation.Path); order != 0 {
					return order
				}
				return cmp.Compare(a.Observation.Id, b.Observation.Id)
			})
			for _, mode := range []string{"syntax", "whole"} {
				filters.Query, filters.Match = "*", mode
				got, err := db.Search(t.Context(), filters, 200, base.SearchAnchor{}, nil)
				if err != nil {
					t.Fatal(err)
				}
				if len(got.Items) != len(want.Items) {
					t.Fatalf("%+v: got %d items want %d", filters, len(got.Items), len(want.Items))
				}
				for i, item := range got.Items {
					if item.Observation != want.Items[i].Observation ||
						item.Content.CurrentDiskCount != want.Items[i].Content.CurrentDiskCount ||
						item.Content.CurrentLocationCount != want.Items[i].Content.CurrentLocationCount {
						t.Fatalf("different filtered observation/counts: %+v vs %+v", item, want.Items[i])
					}
				}
				verifySearchWindowPages(t, db, filters)
			}
		}
	}
}

func BenchmarkSearchMatchAll(b *testing.B) {
	db, err := Open(filepath.Join(b.TempDir(), "catalog.sqlite"))
	if err != nil {
		b.Fatal(err)
	}
	defer db.Close()
	seedSearchReplicaHistory(b, db, 50000, 2, 1)
	for _, test := range []struct {
		name   string
		metric base.ReplicaMetric
		bound  *int64
	}{
		{"unfiltered", base.ReplicaDisks, nil},
		{"one_other_disk", base.ReplicaDisks, new(int64(1))},
		{"no_matching_locations", base.ReplicaLocations, new(int64(1))},
	} {
		f := base.SearchFilters{Query: "*", Field: "name", Match: "syntax",
			ContentFilters: base.ContentFilters{Scope: base.ScopeCurrent,
				ReplicaMetric: test.metric, OtherReplicas: test.bound}}
		b.Run(test.name, func(b *testing.B) {
			b.ReportAllocs()
			for b.Loop() {
				if _, err := db.Search(b.Context(), f, 50, base.SearchAnchor{}, nil); err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}
