package database

import (
	"fmt"
	"path/filepath"
	"strings"
	"testing"

	"github.com/omgitsmoe/coldcat/internal/base"
)

func TestCompactSearchMatchesFullSubstring(t *testing.T) {
	for _, full := range []bool{true, false} {
		t.Run(fmt.Sprintf("full=%v", full), func(t *testing.T) {
			db, err := Open(filepath.Join(t.TempDir(), "catalog.sqlite"))
			if err != nil {
				t.Fatal(err)
			}
			defer db.Close()
			if full {
				schema := strings.Replace(searchTrigramSchema, ", detail=column, columnsize=0", "", 1)
				if _, err := db.db.Exec("DROP TABLE search_trigram;" + schema); err != nil {
					t.Fatal(err)
				}
			}
			long := strings.Repeat("abcde", 200)
			paths := []string{
				"prefix/abcd", "prefix/abc--bcd", "prefix/aaaa", "prefix/aaa--aaa",
				"prefix/a\"b c", "prefix/Σ猫文É", "prefix/ababa", "prefix/a_b%c",
				"prefix/a  b", "prefix/" + long,
			}
			seedSearchPaths(t, db, paths)
			for _, query := range []string{
				"abcd", "aaaa", "ababa", "a\"b c", "σ猫文é", "a_b%c", "a  b",
				"prefix", "prefix/abcd", long,
			} {
				for _, field := range []string{"name", "path"} {
					filter := base.SearchFilters{Query: query, Field: field, Match: "substring",
						ContentFilters: base.ContentFilters{Scope: base.ScopeCurrent,
							ReplicaMetric: base.ReplicaDisks}}
					page, err := db.Search(t.Context(), filter, 50, base.SearchAnchor{}, nil)
					if err != nil {
						t.Fatalf("%s %q: %v", field, query, err)
					}
					want := make(map[string]bool)
					for _, path := range paths {
						text := path
						if field == "name" {
							text = path[strings.LastIndexByte(path, '/')+1:]
						}
						if strings.Contains(searchFold(text), searchFold(query)) {
							want[path] = true
						}
					}
					if len(page.Items) != len(want) {
						t.Fatalf("%s %q: %d matches, want %d", field, query, len(page.Items), len(want))
					}
					for _, item := range page.Items {
						if !want[item.Observation.Path] {
							t.Fatalf("false substring match: %s", item.Observation.Path)
						}
					}
				}
			}
		})
	}
}

func BenchmarkSearchTrigramLayout(b *testing.B) {
	for _, full := range []bool{true, false} {
		b.Run(fmt.Sprintf("full=%v", full), func(b *testing.B) {
			db, err := Open(filepath.Join(b.TempDir(), "catalog.sqlite"))
			if err != nil {
				b.Fatal(err)
			}
			defer db.Close()
			if full {
				schema := strings.Replace(searchTrigramSchema, ", detail=column, columnsize=0", "", 1)
				if _, err := db.db.Exec("DROP TABLE search_trigram;" + schema); err != nil {
					b.Fatal(err)
				}
			}
			paths := make([]string, 50000)
			for i := range paths {
				paths[i] = fmt.Sprintf("photos/archive/2026/collection/%04d/report-%07d.txt", i/100, i)
			}
			seedSearchPaths(b, db, paths)
			report, err := db.Storage(b.Context())
			if err != nil {
				b.Fatal(err)
			}
			var ftsBytes int64
			for _, object := range report.Objects {
				if strings.HasPrefix(object.Name, "search_trigram_") {
					ftsBytes += object.Bytes
				}
			}
			b.Logf("50,000 paths: FTS storage %d bytes", ftsBytes)
			for _, query := range []string{"report", "report-0000025.txt", "absent"} {
				b.Run(query+"/candidate-baseline", func(b *testing.B) {
					match := searchTrigramMatch("name", query)
					if full {
						match = `name_fold:"` + query + `"`
					}
					b.ReportAllocs()
					for b.Loop() {
						var count int
						if err := db.db.QueryRow(`SELECT COUNT(*) FROM search_trigram t
JOIN search_path p ON p.id=t.rowid WHERE search_trigram MATCH ? AND instr(p.name_fold,?)>0`,
							match, query).Scan(&count); err != nil {
							b.Fatal(err)
						}
					}
				})
				b.Run(query, func(b *testing.B) {
					filter := base.SearchFilters{Query: query, Field: "name", Match: "substring",
						ContentFilters: base.ContentFilters{Scope: base.ScopeCurrent,
							ReplicaMetric: base.ReplicaDisks}}
					b.ReportAllocs()
					for b.Loop() {
						if _, err := db.Search(b.Context(), filter, 50, base.SearchAnchor{}, nil); err != nil {
							b.Fatal(err)
						}
					}
					b.ReportMetric(float64(ftsBytes), "fts-bytes")
				})
			}
		})
	}
}
