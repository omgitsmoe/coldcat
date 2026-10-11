package database

import (
	"fmt"
	"path/filepath"
	"testing"
	"time"

	"github.com/omgitsmoe/coldcat/internal/base"
)

func BenchmarkSearchSyntax(b *testing.B) {
	for _, count := range []int{50000, 1000000} {
		b.Run(fmt.Sprint(count), func(b *testing.B) {
			db, err := Open(filepath.Join(b.TempDir(), "catalog.sqlite"))
			if err != nil {
				b.Fatal(err)
			}
			defer db.Close()
			started := time.Now()
			paths := make([]string, count)
			for i := range paths {
				if i%10 == 0 {
					paths[i] = fmt.Sprintf("personal/é猫Σ/report-%07d-foo.txt", i)
				} else {
					paths[i] = fmt.Sprintf("archive/report-%07d.txt", i)
				}
			}
			seedSearchPaths(b, db, paths)
			b.Logf("fixture: %d distinct paths, built in %s", count, time.Since(started))
			for _, test := range []struct{ name, mode, field, query string }{
				{"baseline_selective", "substring", "name", "0000005"},
				{"baseline_broad", "substring", "name", "report"},
				{"wildcard_selective", "wildcard", "path", "archive/*0000005*"},
				{"wildcard_ordered", "wildcard", "path", "personal*foo.txt"},
				{"wildcard_leading", "wildcard", "name", "*report*txt"},
				{"wildcard_unicode", "wildcard", "path", "*é猫σ*foo*"},
				{"wildcard_absent", "wildcard", "path", "personal*missing*"},
				{"and_selective", "expression", "path", "archive AND 0000005"},
				{"or_selective", "expression", "name", "0000005 OR 0000006"},
				{"or_broad", "expression", "path", "archive OR personal"},
				{"syntax_selective", "syntax", "path", "archive*0000005 OR personal*0000010"},
				{"syntax_broad", "syntax", "path", "archive*txt OR personal*foo"},
				{"whole_combined", "whole", "path", "(archive* OR personal*) AND *report*txt"},
			} {
				f := base.SearchFilters{Query: test.query, Field: test.field, Match: test.mode,
					ContentFilters: base.ContentFilters{Scope: base.ScopeCurrent,
						ReplicaMetric: base.ReplicaDisks}}
				first, err := db.Search(b.Context(), f, 50, base.SearchAnchor{}, nil)
				if err != nil {
					b.Fatal(err)
				}
				for _, next := range []bool{false, true} {
					var after base.SearchAnchor
					if next {
						if len(first.Items) <= 50 {
							continue
						}
						last := first.Items[49]
						after = base.SearchAnchor{ID: last.Observation.Id,
							Path: last.Observation.Path, Relevance: last.Relevance}
					}
					b.Run(fmt.Sprintf("%s/next=%t", test.name, next), func(b *testing.B) {
						b.ReportAllocs()
						for b.Loop() {
							if _, err := db.Search(b.Context(), f, 50, after, nil); err != nil {
								b.Fatal(err)
							}
						}
					})
				}
			}
			for _, pattern := range []string{"personal*foo.txt", "*report*txt", "*0000005*"} {
				_, fragments, err := searchWildcard(pattern)
				if err != nil {
					b.Fatal(err)
				}
				e := &searchExpression{literal: fragments[0]}
				for _, fragment := range fragments[1:] {
					e = &searchExpression{op: "AND", left: e,
						right: &searchExpression{literal: fragment}}
				}
				match, _, _ := e.compile("path")
				for _, native := range []bool{false, true} {
					query := `SELECT COUNT(*) FROM search_trigram t
 JOIN search_path p ON p.id=t.rowid WHERE search_trigram MATCH ? AND p.path_fold GLOB ?`
					args := []any{match, pattern}
					if native {
						query = `SELECT COUNT(*) FROM search_trigram WHERE path_fold GLOB ?`
						args = []any{pattern}
					}
					b.Run(fmt.Sprintf("verification/%s/native=%t", pattern, native), func(b *testing.B) {
						var matched int
						b.ReportAllocs()
						for b.Loop() {
							if err := db.db.QueryRowContext(b.Context(), query, args...).Scan(&matched); err != nil {
								b.Fatal(err)
							}
						}
						b.ReportMetric(float64(matched), "matches")
					})
				}
			}
		})
	}
}

func BenchmarkSearchSyntaxHistory(b *testing.B) {
	for _, count := range []int{50000, 1000000} {
		b.Run(fmt.Sprint(count), func(b *testing.B) {
			db, err := Open(filepath.Join(b.TempDir(), "catalog.sqlite"))
			if err != nil {
				b.Fatal(err)
			}
			defer db.Close()
			started := time.Now()
			seedSearchReplicaHistory(b, db, count, 3, 5)
			b.Logf("fixture: %d current, %d historical observations across 3 disks; built in %s",
				count, 5*count, time.Since(started))
			for _, scope := range []base.Scope{base.ScopeCurrent, base.ScopeHistory} {
				for _, test := range []struct{ mode, query string }{
					{"substring", "report"},
					{"wildcard", "disk-01*report*txt"},
					{"expression", "disk-01 AND report"},
					{"expression", "keepsake OR retired"},
				} {
					f := base.SearchFilters{Query: test.query, Field: "path", Match: test.mode,
						ContentFilters: base.ContentFilters{Scope: scope,
							ReplicaMetric: base.ReplicaDisks}}
					first, err := db.Search(b.Context(), f, 50, base.SearchAnchor{}, nil)
					if err != nil {
						b.Fatal(err)
					}
					for _, next := range []bool{false, true} {
						var after base.SearchAnchor
						if next {
							if len(first.Items) <= 50 {
								continue
							}
							last := first.Items[49]
							after = base.SearchAnchor{ID: last.Observation.Id,
								Path: last.Observation.Path, Relevance: last.Relevance}
						}
						name := fmt.Sprintf("%s/%s/%s/next=%t", scope, test.mode, test.query, next)
						b.Run(name, func(b *testing.B) {
							b.ReportAllocs()
							for b.Loop() {
								if _, err := db.Search(b.Context(), f, 50, after, nil); err != nil {
									b.Fatal(err)
								}
							}
						})
					}
				}
			}
		})
	}
}
