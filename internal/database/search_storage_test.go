package database

import (
	"encoding/binary"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/omgitsmoe/coldcat/internal/base"
)

func seedSearchPaths(tb testing.TB, db *DB, paths []string) {
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

func TestSearchCaseFoldingAndMinimumLength(t *testing.T) {
	db, err := Open(filepath.Join(t.TempDir(), "catalog.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	seedSearchPaths(t, db, []string{
		"docs/Report.txt", "unicode/Σςσ", "unicode/KKk", "unicode/É猫文",
		"unicode/ß", "unicode/e\u0301", "punct/[?]*.txt", "short/a",
	})
	for _, test := range []struct {
		query, field, match string
		want                int
	}{
		{"REPORT", "name", "substring", 1},
		{"DOCS/REPORT.TXT", "path", "exact", 1},
		{"σσσ", "name", "substring", 1},
		{"kkk", "name", "exact", 1},
		{"é猫文", "name", "substring", 1},
		{"[?]*", "name", "substring", 1},
		{"ss", "name", "exact", 0},
		{"é", "name", "exact", 0},
		{"e\u0301", "name", "exact", 1},
		{"A", "name", "exact", 1},
		{"reprot", "name", "substring", 0},
	} {
		f := base.SearchFilters{Query: test.query, Field: test.field, Match: test.match,
			ContentFilters: base.ContentFilters{Scope: base.ScopeCurrent,
				ReplicaMetric: base.ReplicaDisks}}
		page, err := db.Search(t.Context(), f, 50, base.SearchAnchor{}, nil)
		if err != nil || len(page.Items) != test.want {
			t.Fatalf("%+v: got %d want %d: %v", test, len(page.Items), test.want, err)
		}
	}
	for _, query := range []string{"a", "xy", "é猫", "e\u0301"} {
		f := base.SearchFilters{Query: query, Field: "name", Match: "substring",
			ContentFilters: base.ContentFilters{Scope: base.ScopeCurrent,
				ReplicaMetric: base.ReplicaDisks}}
		if err := ValidateSearchFilters(f); err == nil {
			t.Fatalf("accepted short substring query %q", query)
		}
	}
	var removed int
	if err := db.db.QueryRow(`SELECT count(*) FROM sqlite_schema
 WHERE name IN ('search_short','search_fold_short','search_fold_trigram',
 'search_fuzzy_signature')`).Scan(&removed); err != nil || removed != 0 {
		t.Fatalf("removed indexes remain: %d %v", removed, err)
	}
	if _, err := db.db.Exec(`INSERT INTO search_trigram(search_trigram,rank)
 VALUES('integrity-check',1)`); err != nil {
		t.Fatal(err)
	}
}

func BenchmarkSearchPersisted(b *testing.B) {
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
			seedSearchPaths(b, db, paths)
			info, err := os.Stat(path)
			if err != nil {
				b.Fatal(err)
			}
			b.Logf("fixture: %d unique paths, %d bytes, built in %s",
				count, info.Size(), time.Since(started))
			for _, query := range []string{"report-0000005.txt", "REPORT", "absent"} {
				b.Run(query, func(b *testing.B) {
					f := base.SearchFilters{Query: query, Field: "name", Match: "substring",
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
