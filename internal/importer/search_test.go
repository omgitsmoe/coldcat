package importer

import (
	"strings"
	"testing"

	"github.com/omgitsmoe/coldcat/internal/base"
)

func TestSearchIndexFailureCleansCommittedBatchesAndPreservesSharedPaths(t *testing.T) {
	db, raw, disk := testDB(t)
	_, err := ImportReader(t.Context(), db, request(disk),
		strings.NewReader(",sha256,"+fixtureSHA25600000000+" tree/file-0\n"))
	assertNoErr(t, err)
	assertNoErr(t, execSQL(raw, `CREATE TRIGGER fail_search BEFORE INSERT ON search_path
 WHEN NEW.path='tree/file-1001' BEGIN SELECT RAISE(ABORT,'search index failed'); END;`))
	_, err = ImportReader(t.Context(), db, request(disk), strings.NewReader(manyFiles(2005, false)))
	if err == nil || !strings.Contains(err.Error(), "search index failed") {
		t.Fatalf("index failure: %v", err)
	}
	assertEqual(t, count(t, raw, "search_path"), 1)
	var matches int
	assertNoErr(t, raw.QueryRow(`SELECT COUNT(*) FROM search_trigram
 WHERE search_trigram MATCH 'path:"tree/file"'`).Scan(&matches))
	assertEqual(t, matches, 1)
	assertNoErr(t, execSQL(raw,
		`INSERT INTO search_trigram(search_trigram,rank) VALUES('integrity-check',1)`))
	page, err := db.Search(t.Context(), base.SearchFilters{
		Query: "file-0", Field: "name", Match: "exact",
		ContentFilters: base.ContentFilters{Scope: base.ScopeCurrent, ReplicaMetric: base.ReplicaDisks},
	}, 50, base.SearchAnchor{}, nil)
	assertNoErr(t, err)
	assertEqual(t, len(page.Items), 1)
}
