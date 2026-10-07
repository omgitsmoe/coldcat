package importer

import (
	"strings"
	"testing"

	"github.com/omgitsmoe/coldcat/internal/base"
)

func TestSearchIndexFailureCleansCommittedBatchesAndPreservesSharedPaths(t *testing.T) {
	for _, index := range []string{"literal", "fuzzy"} {
		t.Run(index, func(t *testing.T) { testSearchIndexFailure(t, index) })
	}
}

func testSearchIndexFailure(t *testing.T, index string) {
	db, raw, disk := testDB(t)
	_, err := ImportReader(t.Context(), db, request(disk),
		strings.NewReader(",sha256,"+fixtureSHA25600000000+" tree/file-0\n"+
			",sha256,"+fixtureSHA25600000000+" tree/FILE-0\n"))
	assertNoErr(t, err)
	failure := `CREATE TRIGGER fail_search BEFORE INSERT ON search_path
 WHEN NEW.path='tree/file-1001' BEGIN SELECT RAISE(ABORT,'search index failed'); END;`
	if index == "fuzzy" {
		failure = `CREATE TRIGGER fail_search BEFORE INSERT ON search_fuzzy_signature
 WHEN NEW.path_id=(SELECT id FROM search_path WHERE path='tree/file-1001')
 BEGIN SELECT RAISE(ABORT,'search index failed'); END;`
	}
	assertNoErr(t, execSQL(raw, failure))
	_, err = ImportReader(t.Context(), db, request(disk), strings.NewReader(manyFiles(2005, false)))
	if err == nil || !strings.Contains(err.Error(), "search index failed") {
		t.Fatalf("index failure: %v", err)
	}
	assertEqual(t, count(t, raw, "search_path"), 2)
	for _, table := range []string{"search_fuzzy_signature", "search_fold_short"} {
		var paths int
		assertNoErr(t, raw.QueryRow(`SELECT COUNT(DISTINCT path_id) FROM `+table).Scan(&paths))
		want := 2
		if table == "search_fold_short" {
			want = 1
		}
		assertEqual(t, paths, want)
	}
	var matches int
	assertNoErr(t, raw.QueryRow(`SELECT COUNT(*) FROM search_trigram
 WHERE search_trigram MATCH 'path:"tree/file"'`).Scan(&matches))
	assertEqual(t, matches, 1)
	assertNoErr(t, execSQL(raw,
		`INSERT INTO search_trigram(search_trigram,rank) VALUES('integrity-check',1)`))
	assertNoErr(t, execSQL(raw,
		`INSERT INTO search_fold_trigram(search_fold_trigram,rank) VALUES('integrity-check',1)`))
	page, err := db.Search(t.Context(), base.SearchFilters{
		Query: "file-0", Field: "name", Match: "exact",
		ContentFilters: base.ContentFilters{Scope: base.ScopeCurrent, ReplicaMetric: base.ReplicaDisks},
	}, 50, base.SearchAnchor{}, nil)
	assertNoErr(t, err)
	assertEqual(t, len(page.Items), 1)
	page, err = db.Search(t.Context(), base.SearchFilters{
		Query: "fiel-0", Field: "name", Match: "fuzzy",
		ContentFilters: base.ContentFilters{Scope: base.ScopeCurrent, ReplicaMetric: base.ReplicaDisks},
	}, 50, base.SearchAnchor{}, nil)
	assertNoErr(t, err)
	assertEqual(t, len(page.Items), 2)
}
