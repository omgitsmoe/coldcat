package database

import (
	"fmt"
	"testing"

	"github.com/omgitsmoe/coldcat/internal/base"
)

func directoryHistoryFixture(t testing.TB, count, generations int) (*DB, int, int) {
	t.Helper()
	if generations < 1 || generations > 5 {
		t.Fatal("fixture requires one to five generations")
	}
	db, contents := directoryComparisonFixture(t, count)
	for generation := 1; generation < generations; generation++ {
		offset := generation * 5
		captured := fmt.Sprintf("2023-01-%02dT00:00:00.000000000Z", generation+1)
		err := db.TransactionContext(t.Context(), func(tx *Tx) error {
			_, err := tx.ExecContext(t.Context(), `INSERT INTO snapshot
 (id,disk_id,state,captured_at,imported_at,capture_provenance,input_format)
 SELECT id+?,disk_id,'importing',?,?,'explicit','cshd'
 FROM snapshot WHERE id<=5;
 INSERT INTO observation(snapshot_id,content_id,path)
 SELECT snapshot_id+?,content_id,path FROM observation WHERE snapshot_id<=5;`,
				offset, captured, captured, offset)
			return err
		})
		if err != nil {
			t.Fatal(err)
		}
		for disk := 1; disk <= 5; disk++ {
			id := int64(offset + disk)
			if err := db.BuildDirectories(t.Context(), id); err != nil {
				t.Fatal(err)
			}
		}
		_, err = db.db.ExecContext(t.Context(), `UPDATE snapshot SET state='complete',
 source_digest=zeroblob(32),
 file_count=(SELECT COUNT(*) FROM observation WHERE snapshot_id=snapshot.id),
 content_count=(SELECT COUNT(DISTINCT content_id) FROM observation
 WHERE snapshot_id=snapshot.id) WHERE id>? AND id<=?`, offset, offset+5)
		if err != nil {
			t.Fatal(err)
		}
	}

	var total, current, snapshots int
	err := db.db.QueryRowContext(t.Context(), currentSnapshots+`SELECT
 (SELECT COUNT(*) FROM observation),
 (SELECT COUNT(*) FROM observation o JOIN current_snapshot cs ON cs.id=o.snapshot_id),
 (SELECT COUNT(*) FROM snapshot WHERE state='complete')`).Scan(&total, &current, &snapshots)
	wantCurrent := 3*count + 1 + contents
	if err != nil || total != wantCurrent*generations || current != wantCurrent ||
		snapshots != 5*generations {
		t.Fatalf("fixture counts: total=%d current=%d snapshots=%d: %v",
			total, current, snapshots, err)
	}
	for disk := 1; disk <= 5; disk++ {
		var id int
		err := db.db.QueryRowContext(t.Context(),
			currentSnapshots+"SELECT id FROM current_snapshot WHERE disk_id=?", disk).Scan(&id)
		if err != nil || id != 5*(generations-1)+disk {
			t.Fatalf("disk %d current snapshot=%d: %v", disk, id, err)
		}
	}
	return db, contents, total
}

func TestDirectoryHistoryComparisonBenchmarkFixture(t *testing.T) {
	for _, generations := range []int{1, 5} {
		t.Run(fmt.Sprint(generations), func(t *testing.T) {
			const count = 100
			db, contents, _ := directoryHistoryFixture(t, count, generations)
			offset := 5 * (generations - 1)
			for _, source := range []int{offset + 1, 1} {
				for _, tc := range directoryComparisonCases() {
					filters := tc.filters
					filters.SnapshotID = base.SnapshotId(source)
					replicas, err := db.ListDirectoryReplicas(
						t.Context(), filters, 50, base.DirectoryReplicaAnchor{}, nil,
					)
					if err != nil {
						t.Fatal(err)
					}
					checkComparisonReplicasAt(t, replicas, count, contents, tc.name,
						offset, source != offset+1)
					coverage, err := db.ListDirectoryCoverage(t.Context(), filters, 50, 0, nil)
					if err != nil {
						t.Fatal(err)
					}
					checkComparisonCoverageAt(t, coverage, count, contents, tc.name, offset)
				}
			}
		})
	}
}

func TestDirectoryHistoryComparisonOldOnlyDestination(t *testing.T) {
	db, _, _ := directoryHistoryFixture(t, 100, 5)
	err := db.TransactionContext(t.Context(), func(tx *Tx) error {
		_, err := tx.ExecContext(t.Context(), `INSERT INTO snapshot
 (id,disk_id,state,captured_at,imported_at,capture_provenance,input_format)
 VALUES(26,1,'importing','2022-01-01T00:00:00.000000000Z',
 '2023-01-06T00:00:00.000000000Z','explicit','cshd'),
 (27,2,'importing','2022-01-01T00:00:00.000000000Z',
 '2023-01-06T00:00:00.000000000Z','explicit','cshd');
 INSERT INTO content(id,size,hash_type,hash)
 VALUES(100,4,'sha256',zeroblob(32));
 INSERT INTO observation(snapshot_id,content_id,path)
 VALUES(26,100,'old-only/file.txt'),(27,100,'obsolete-copy/file.txt');`)
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	for _, id := range []int64{26, 27} {
		if err := db.BuildDirectories(t.Context(), id); err != nil {
			t.Fatal(err)
		}
	}
	_, err = db.db.ExecContext(t.Context(), `UPDATE snapshot SET state='complete',
 source_digest=zeroblob(32),file_count=1,content_count=1 WHERE id IN (26,27)`)
	if err != nil {
		t.Fatal(err)
	}
	for _, block := range [][]string{nil, {"**/*.log"}} {
		filters := base.DirectoryComparisonFilters{SnapshotID: 26, Path: "old-only", Block: block}
		replicas, err := db.ListDirectoryReplicas(
			t.Context(), filters, 50, base.DirectoryReplicaAnchor{}, nil,
		)
		if err != nil || len(replicas.Items) != 0 || replicas.Selection.RetainedFileCount != 1 {
			t.Fatalf("old-only replicas: %+v: %v", replicas, err)
		}
		coverage, err := db.ListDirectoryCoverage(t.Context(), filters, 50, 0, nil)
		if err != nil || len(coverage.Items) != 4 || coverage.Selection.ContentCount != 1 {
			t.Fatalf("old-only coverage: %+v: %v", coverage, err)
		}
		for i, item := range coverage.Items {
			if item.Disk.Id != base.DiskId(i+2) || item.Snapshot.Id != base.SnapshotId(i+22) ||
				item.CoveredFileCount != 0 || item.CoveredContentCount != 0 ||
				item.MissingFileCount != 1 || item.MissingContentCount != 1 ||
				item.CoveredKnownBytes != 0 || item.MissingKnownBytes != 4 || item.Complete {
				t.Fatalf("historical destination inflated coverage: %+v", item)
			}
		}
	}
}

func BenchmarkDirectoryHistoryComparisons(b *testing.B) {
	const count = 50000
	b.Run(fmt.Sprint(count), func(b *testing.B) {
		for _, generations := range []int{1, 5} {
			b.Run(fmt.Sprintf("snapshots_%d", generations), func(b *testing.B) {
				db, contents, total := directoryHistoryFixture(b, count, generations)
				offset := 5 * (generations - 1)
				for _, source := range []string{"current", "oldest"} {
					for _, tc := range directoryComparisonCases()[:2] {
						filters := tc.filters
						filters.SnapshotID = base.SnapshotId(offset + 1)
						if source == "oldest" {
							filters.SnapshotID = 1
						}
						for _, operation := range []string{"replicas", "coverage"} {
							b.Run(source+"/"+tc.name+"/"+operation, func(b *testing.B) {
								var replicas base.DirectoryReplicaPage
								var coverage base.DirectoryCoveragePage
								var err error
								query := func() {
									if operation == "replicas" {
										replicas, err = db.ListDirectoryReplicas(
											b.Context(), filters, 50, base.DirectoryReplicaAnchor{}, nil)
									} else {
										coverage, err = db.ListDirectoryCoverage(b.Context(), filters, 50, 0, nil)
									}
								}
								check := func() {
									if err != nil {
										b.Fatal(err)
									}
									if operation == "replicas" {
										checkComparisonReplicasAt(b, replicas, count, contents, tc.name,
											offset, int(filters.SnapshotID) != offset+1)
									} else {
										checkComparisonCoverageAt(b, coverage, count, contents, tc.name, offset)
									}
								}
								query()
								check()
								b.ReportAllocs()
								for b.Loop() {
									query()
									b.StopTimer()
									check()
									b.StartTimer()
								}
								b.ReportMetric(float64(count), "source-files")
								b.ReportMetric(float64(contents), "source-contents")
								b.ReportMetric(float64(total), "historical-observations")
								b.ReportMetric(float64(total/generations), "current-observations")
								b.ReportMetric(5, "disks")
								b.ReportMetric(float64(5*generations), "snapshots")
							})
						}
					}
				}
			})
		}
	})
}
