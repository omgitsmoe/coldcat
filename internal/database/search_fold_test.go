package database

import (
	"path/filepath"
	"testing"

	"github.com/omgitsmoe/coldcat/internal/base"
)

func TestFoldedIndexReusesUnchangedText(t *testing.T) {
	db, err := Open(filepath.Join(t.TempDir(), "catalog.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	seedFuzzyPaths(t, db, []string{"folder/report", "Folder/Report", "UPPER/report"})
	for _, test := range []struct {
		field string
		paths int
	}{{"name", 1}, {"path", 2}} {
		var paths int
		if err := db.db.QueryRow(`SELECT count(DISTINCT path_id) FROM search_fold_short
 WHERE field=?`, test.field).Scan(&paths); err != nil || paths != test.paths {
			t.Fatalf("folded %s postings: got %d want %d: %v", test.field, paths, test.paths, err)
		}
	}
	for _, query := range []string{"REPORT", "RE", "reprot", "upper/report"} {
		field, want := "name", 3
		if query == "upper/report" {
			field, want = "path", 1
		}
		page, err := db.Search(t.Context(), base.SearchFilters{
			Query: query, Field: field, Match: "fuzzy",
			ContentFilters: base.ContentFilters{Scope: base.ScopeCurrent, ReplicaMetric: base.ReplicaDisks},
		}, 50, base.SearchAnchor{}, nil)
		if err != nil || len(page.Items) != want {
			t.Fatalf("%q: got %d want %d: %v", query, len(page.Items), want, err)
		}
	}
	if _, err := db.db.Exec(`INSERT INTO search_fold_trigram(search_fold_trigram,rank)
 VALUES('integrity-check',1)`); err != nil {
		t.Fatal(err)
	}
}
