package database

import (
	"encoding/binary"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/omgitsmoe/coldcat/internal/base"
)

func TestFuzzyVerifierAndSignatureRecall(t *testing.T) {
	terms := []string{""}
	for depth := 0; depth < 3; depth++ {
		previous := slices.Clone(terms)
		for _, term := range previous {
			for _, r := range "abΣ" {
				terms = append(terms, term+string(r))
			}
		}
		slices.Sort(terms)
		terms = slices.Compact(terms)
	}
	for _, a := range terms {
		queryKeys := fuzzySignatures(a, true)
		for _, b := range terms {
			want := spikeDistance(a, b) <= 1
			if got := fuzzyOne(a, b); got != want {
				t.Fatalf("verifier %q/%q: got %v want %v", a, b, got, want)
			}
			if want {
				matched := false
				for _, key := range fuzzySignatures(b, false) {
					matched = matched || slices.Contains(queryKeys, key)
				}
				if !matched {
					t.Fatalf("signature recall lost %q/%q", a, b)
				}
			}
		}
	}
	for _, pair := range [][2]string{{"Σςσ", "σσσ"}, {"KKk", "kkk"}, {"É", "é"}} {
		if fuzzyFold(pair[0]) != fuzzyFold(pair[1]) {
			t.Fatalf("fold mismatch: %q", pair)
		}
	}
	if fuzzyFold("ß") == fuzzyFold("ss") || fuzzyFold("é") == fuzzyFold("e\u0301") {
		t.Fatal("unexpected multi-rune or canonical normalization")
	}
}

func seedFuzzyPaths(tb testing.TB, db *DB, paths []string) {
	tb.Helper()
	_, err := db.db.Exec(`INSERT INTO disk(id,label,capacity) VALUES(1,'disk',0);
 INSERT INTO snapshot(id,disk_id,state,captured_at,imported_at,capture_provenance)
 VALUES(1,1,'importing','2023-01-01T00:00:00.000000000Z',
 '2023-01-01T00:00:00.000000000Z','explicit');`)
	if err != nil {
		tb.Fatal(err)
	}
	err = db.TransactionContext(tb.Context(), func(tx *Tx) error {
		for i, path := range paths {
			var hash [32]byte
			binary.BigEndian.PutUint64(hash[:], uint64(i+1))
			if _, err := tx.ExecContext(tb.Context(),
				`INSERT INTO content(id,hash_type,hash) VALUES(?,'sha256',?)`, i+1, hash[:]); err != nil {
				return err
			}
			if _, err := tx.ExecContext(tb.Context(),
				`INSERT INTO observation(snapshot_id,content_id,path) VALUES(1,?,?)`, i+1, path); err != nil {
				return err
			}
		}
		_, err := tx.ExecContext(tb.Context(), `UPDATE snapshot SET state='complete',
 source_digest=zeroblob(32),file_count=?,content_count=? WHERE id=1`, len(paths), len(paths))
		return err
	})
	if err != nil {
		tb.Fatal(err)
	}
}

func TestPersistedFuzzySearchMatchesOracle(t *testing.T) {
	db, err := Open(filepath.Join(t.TempDir(), "catalog.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	paths := []string{"a/Report", "b/report", "c/report.txt", "d/xreport",
		"e/reprot", "f/repor", "g/rexort", "h/rexxrt", "unicode/Σ.txt",
		"unicode/É猫%_\\\"[?]", "unicode/e\u0301", "short/a", "short/ab", "short/ac",
		"long/" + strings.Repeat("猫", 4096) + "REPORT"}
	seedFuzzyPaths(t, db, paths)
	for _, field := range []string{"name", "path"} {
		for _, query := range []string{"REPORT", "reprot", "repor", "a", "ac", "ς.txt",
			"é猫%_\\\"[?]", "e\u0301", "a/Repotr", "not-found"} {
			f := base.SearchFilters{Query: query, Field: field, Match: "fuzzy",
				ContentFilters: base.ContentFilters{Scope: base.ScopeCurrent, ReplicaMetric: base.ReplicaDisks}}
			var want []base.SearchAnchor
			folded := fuzzyFold(query)
			for i, path := range paths {
				term := path
				if field == "name" {
					term = path[strings.LastIndexByte(path, '/')+1:]
				}
				term = fuzzyFold(term)
				rank := ""
				switch {
				case term == folded:
					rank = "exact"
				case strings.HasPrefix(term, folded):
					rank = "prefix"
				case strings.Contains(term, folded):
					rank = "substring"
				case spikeDistance(folded, term) <= 1:
					rank = "typo"
				}
				if rank != "" {
					want = append(want, base.SearchAnchor{Relevance: rank, Path: path,
						ID: base.FileObservationId(i + 1)})
				}
			}
			slices.SortFunc(want, func(a, b base.SearchAnchor) int {
				if diff := searchRank(a.Relevance) - searchRank(b.Relevance); diff != 0 {
					return diff
				}
				return strings.Compare(a.Path, b.Path)
			})
			var got []base.SearchAnchor
			after := base.SearchAnchor{}
			for {
				page, err := db.Search(t.Context(), f, 1, after, nil)
				if err != nil {
					t.Fatal(err)
				}
				if len(page.Items) == 0 {
					break
				}
				item := page.Items[0]
				after = base.SearchAnchor{Relevance: item.Relevance, ID: item.Observation.Id}
				got = append(got, base.SearchAnchor{Relevance: item.Relevance,
					Path: item.Observation.Path, ID: item.Observation.Id})
			}
			if !reflect.DeepEqual(got, want) {
				t.Fatalf("%s/%q:\ngot %+v\nwant %+v", field, query, got, want)
			}
		}
	}
	for _, table := range []string{"search_trigram", "search_fold_trigram"} {
		if _, err := db.db.Exec("INSERT INTO " + table + "(" + table +
			",rank) VALUES('integrity-check',1)"); err != nil {
			t.Fatal(err)
		}
	}
}

func BenchmarkSearchFuzzyPersisted(b *testing.B) {
	for _, count := range []int{50000, 1000000} {
		b.Run(fmt.Sprint(count), func(b *testing.B) {
			path := filepath.Join(b.TempDir(), "catalog.sqlite")
			started := time.Now()
			db, err := Open(path)
			if err != nil {
				b.Fatal(err)
			}
			defer db.Close()
			paths := make([]string, count)
			for i := range paths {
				paths[i] = fmt.Sprintf("docs/report-%07d.txt", i)
			}
			seedFuzzyPaths(b, db, paths)
			info, err := os.Stat(path)
			if err != nil {
				b.Fatal(err)
			}
			var signatures int
			if err := db.db.QueryRow(`SELECT count(*) FROM search_fuzzy_signature`).
				Scan(&signatures); err != nil {
				b.Fatal(err)
			}
			b.Logf("fixture: %d unique paths, %d bytes, %d signatures, built in %s",
				count, info.Size(), signatures, time.Since(started))
			for _, query := range []string{"reprot-0000005.txt", "REPORT", "a", "xy"} {
				b.Run(query, func(b *testing.B) {
					f := base.SearchFilters{Query: query, Field: "name", Match: "fuzzy",
						ContentFilters: base.ContentFilters{Scope: base.ScopeCurrent,
							ReplicaMetric: base.ReplicaDisks}}
					b.ReportAllocs()
					for b.Loop() {
						if _, err := db.Search(b.Context(), f, 50, base.SearchAnchor{}, nil); err != nil {
							b.Fatal(err)
						}
					}
				})
			}
		})
	}
}
