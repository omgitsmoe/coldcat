package database

import (
	"fmt"
	"path/filepath"
	"testing"

	"github.com/omgitsmoe/coldcat/internal/base"
)

func directoryComparisonFixture(t testing.TB, count int) (*DB, int) {
	t.Helper()
	if count < 100 || count%100 != 0 {
		t.Fatal("fixture size must be a positive multiple of 100")
	}
	contents := min(count/5, 10000)
	db, err := Open(filepath.Join(t.TempDir(), "catalog.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := db.Close(); err != nil {
			t.Error(err)
		}
	})

	err = db.TransactionContext(t.Context(), func(tx *Tx) error {
		_, err := tx.ExecContext(t.Context(), `DROP TRIGGER observation_search_insert;
 DROP TRIGGER observation_search_delete; DROP TRIGGER observation_search_update;
 INSERT INTO disk(id,label,capacity) VALUES
 (1,'source',0),(2,'exact',0),(3,'filtered',0),(4,'partial-a',0),(5,'partial-b',0);
 INSERT INTO snapshot
 (id,disk_id,state,captured_at,imported_at,capture_provenance,input_format)
 SELECT id,id,'importing','2023-01-01T00:00:00.000000000Z',
 '2023-01-01T00:00:00.000000000Z','explicit','cshd' FROM disk;
 WITH RECURSIVE ids(n) AS (VALUES(1) UNION ALL SELECT n+1 FROM ids WHERE n<?)
 INSERT INTO content(id,size,hash_type,hash)
 SELECT n,4,'sha256',CAST(printf('%032d',n) AS BLOB) FROM ids;`, 2*contents+1)
		if err != nil {
			return err
		}

		_, err = tx.ExecContext(t.Context(), `WITH RECURSIVE ids(n) AS
 (VALUES(1) UNION ALL SELECT n+1 FROM ids WHERE n<?),
 files AS (SELECT n,1+((n-1)%?) AS content_id,
 CASE WHEN n%10=0 THEN printf('log-%07d.log',n)
 WHEN n%5=0 THEN printf('nested/log-%07d.log',n)
 ELSE printf('data/file-%07d.txt',n) END AS path FROM ids)
 INSERT INTO observation(snapshot_id,content_id,path)
 SELECT 1,content_id,'tree/'||path FROM files
 UNION ALL SELECT 2,content_id,'copy/'||path FROM files
 UNION ALL SELECT 3,content_id+CASE WHEN n%5=0 THEN ? ELSE 0 END,
 'filtered/'||path FROM files;`, count, contents, contents)
		if err != nil {
			return err
		}

		_, err = tx.ExecContext(t.Context(), `INSERT INTO observation(snapshot_id,content_id,path)
 VALUES(3,?,'filtered/extra.log')`, 2*contents+1)
		if err != nil {
			return err
		}

		_, err = tx.ExecContext(t.Context(), `INSERT INTO observation(snapshot_id,content_id,path)
 SELECT CASE WHEN id%2=0 THEN 4 ELSE 5 END,id,
 'unrelated/'||printf('renamed-%07d.bin',id) FROM content WHERE id<=?`, contents)
		return err
	})
	if err != nil {
		t.Fatal(err)
	}

	for id := int64(1); id <= 5; id++ {
		if err := db.BuildDirectories(t.Context(), id); err != nil {
			t.Fatal(err)
		}
	}
	_, err = db.db.ExecContext(t.Context(), `UPDATE snapshot SET state='complete',
 source_digest=zeroblob(32),
 file_count=(SELECT COUNT(*) FROM observation WHERE snapshot_id=snapshot.id),
 content_count=(SELECT COUNT(DISTINCT content_id) FROM observation
 WHERE snapshot_id=snapshot.id)`)
	if err != nil {
		t.Fatal(err)
	}
	return db, contents
}

func directoryComparisonCases() []struct {
	name    string
	filters base.DirectoryComparisonFilters
} {
	return []struct {
		name    string
		filters base.DirectoryComparisonFilters
	}{
		{"unfiltered", base.DirectoryComparisonFilters{SnapshotID: 1, Path: "tree"}},
		{"block_logs", base.DirectoryComparisonFilters{
			SnapshotID: 1, Path: "tree", Block: []string{"**/*.log"},
		}},
		{"allow_text", base.DirectoryComparisonFilters{
			SnapshotID: 1, Path: "tree", Allow: []string{"**/*.txt"},
		}},
		{"empty", base.DirectoryComparisonFilters{
			SnapshotID: 1, Path: "tree", Block: []string{"**"},
		}},
	}
}

func checkComparisonSelection(
	t testing.TB, selection base.DirectorySelection, count, contents int, name string,
) {
	t.Helper()
	retained, distinct := int64(count), int64(contents)
	if name == "block_logs" || name == "allow_text" {
		retained = retained * 4 / 5
		distinct = distinct * 4 / 5
	} else if name == "empty" {
		retained, distinct = 0, 0
	}
	want := base.DirectorySelection{
		RetainedFileCount: retained, ExcludedFileCount: int64(count) - retained,
		ContentCount: distinct, KnownBytes: retained * 4, EmptyComparison: retained == 0,
	}
	if selection != want {
		t.Fatalf("selection: %+v, want %+v", selection, want)
	}
}

func checkComparisonReplicas(
	t testing.TB, page base.DirectoryReplicaPage, count, contents int, name string,
) {
	t.Helper()
	checkComparisonReplicasAt(t, page, count, contents, name, 0, false)
}

func checkComparisonReplicasAt(
	t testing.TB, page base.DirectoryReplicaPage, count, contents int, name string,
	offset int, historical bool,
) {
	t.Helper()
	checkComparisonSelection(t, page.Selection, count, contents, name)
	if historical && name != "empty" {
		if len(page.Items) == 0 {
			t.Fatal("missing current same-disk replica of historical source")
		}
		item := page.Items[0]
		if item.Disk.Id != 1 || item.Snapshot.Id != base.SnapshotId(offset+1) ||
			item.Path != "tree" || !item.SameDisk || !item.WholeTreeEqual ||
			item.RetainedFileCount != page.Selection.RetainedFileCount ||
			item.ExcludedFileCount != page.Selection.ExcludedFileCount {
			t.Fatalf("unexpected same-disk replica: %+v", item)
		}
		page.Items = page.Items[1:]
	}
	want := 2
	if name == "unfiltered" {
		want = 1
	} else if name == "empty" {
		want = 0
	}
	if len(page.Items) != want {
		t.Fatalf("replicas: %+v, want %d", page.Items, want)
	}
	for i, item := range page.Items {
		path := "copy"
		excluded := page.Selection.ExcludedFileCount
		if i == 1 {
			path = "filtered"
			excluded++
		}
		if item.Disk.Id != base.DiskId(i+2) || item.Snapshot.Id != base.SnapshotId(offset+i+2) ||
			item.Path != path || item.SameDisk || item.WholeTreeEqual != (i == 0) ||
			item.RetainedFileCount != page.Selection.RetainedFileCount ||
			item.ExcludedFileCount != excluded {
			t.Fatalf("unexpected replica: %+v", item)
		}
	}
}

func checkComparisonCoverage(
	t testing.TB, page base.DirectoryCoveragePage, count, contents int, name string,
) {
	t.Helper()
	checkComparisonCoverageAt(t, page, count, contents, name, 0)
}

func checkComparisonCoverageAt(
	t testing.TB, page base.DirectoryCoveragePage, count, contents int, name string, offset int,
) {
	t.Helper()
	checkComparisonSelection(t, page.Selection, count, contents, name)
	want := 4
	if name == "empty" {
		want = 0
	}
	if len(page.Items) != want {
		t.Fatalf("coverage: %+v, want %d disks", page.Items, want)
	}
	for i, item := range page.Items {
		files, distinct := page.Selection.RetainedFileCount, page.Selection.ContentCount
		if i >= 2 {
			files, distinct = files/2, distinct/2
		} else if i == 1 && name == "unfiltered" {
			files, distinct = files*4/5, distinct*4/5
		}
		missing := page.Selection.RetainedFileCount - files
		if item.Disk.Id != base.DiskId(i+2) || item.Snapshot.Id != base.SnapshotId(offset+i+2) ||
			item.CoveredFileCount != files || item.MissingFileCount != missing ||
			item.CoveredContentCount != distinct ||
			item.MissingContentCount != page.Selection.ContentCount-distinct ||
			item.CoveredKnownBytes != files*4 || item.MissingKnownBytes != missing*4 ||
			item.CoveredUnknownSizeFiles != 0 || item.MissingUnknownSizeFiles != 0 ||
			item.Complete != (missing == 0) {
			t.Fatalf("unexpected coverage: %+v", item)
		}
	}
}

func TestDirectoryComparisonBenchmarkFixture(t *testing.T) {
	const count = 100
	db, contents := directoryComparisonFixture(t, count)
	var observations int
	if err := db.db.QueryRowContext(t.Context(),
		"SELECT COUNT(*) FROM observation").Scan(&observations); err != nil {
		t.Fatal(err)
	}
	if observations != 3*count+1+contents {
		t.Fatalf("catalog observations: %d", observations)
	}

	for _, tc := range directoryComparisonCases() {
		t.Run(tc.name, func(t *testing.T) {
			replicas, err := db.ListDirectoryReplicas(
				t.Context(), tc.filters, 50, base.DirectoryReplicaAnchor{}, nil,
			)
			if err != nil {
				t.Fatal(err)
			}
			checkComparisonReplicas(t, replicas, count, contents, tc.name)
			coverage, err := db.ListDirectoryCoverage(t.Context(), tc.filters, 50, 0, nil)
			if err != nil {
				t.Fatal(err)
			}
			checkComparisonCoverage(t, coverage, count, contents, tc.name)
		})
	}
}

func BenchmarkDirectoryFilteredComparisons(b *testing.B) {
	for _, count := range []int{50000, 1000000} {
		b.Run(fmt.Sprint(count), func(b *testing.B) {
			db, contents := directoryComparisonFixture(b, count)
			b.Logf("source observations=%d; catalog observations=%d; source contents=%d",
				count, 3*count+1+contents, contents)
			for _, tc := range directoryComparisonCases() {
				for _, operation := range []string{"replicas", "coverage"} {
					b.Run(tc.name+"/"+operation, func(b *testing.B) {
						query := func() {
							if operation == "replicas" {
								page, err := db.ListDirectoryReplicas(
									b.Context(), tc.filters, 50, base.DirectoryReplicaAnchor{}, nil,
								)
								if err != nil {
									b.Fatal(err)
								}
								checkComparisonReplicas(b, page, count, contents, tc.name)
							} else {
								page, err := db.ListDirectoryCoverage(
									b.Context(), tc.filters, 50, 0, nil,
								)
								if err != nil {
									b.Fatal(err)
								}
								checkComparisonCoverage(b, page, count, contents, tc.name)
							}
						}
						query()
						b.ReportAllocs()
						for b.Loop() {
							query()
						}
						b.ReportMetric(float64(count), "source-files")
						b.ReportMetric(float64(3*count+1+contents), "catalog-observations")
						b.ReportMetric(float64(contents), "source-contents")
					})
				}
			}
		})
	}
}
