package database

import (
	"bytes"
	"context"
	"crypto/sha256"
	"errors"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/omgitsmoe/coldcat/internal/base"
)

func TestComparisonHashRecordCanonicalEncoding(t *testing.T) {
	records := []comparisonRecord{
		{path: "root/a.txt", algorithm: "sha256", digest: []byte{0, 1, 2}},
		{path: "root/界/" + strings.Repeat("x", 10000), algorithm: "md5", digest: []byte{3, 4}},
		{path: `root/literal\*.txt`, algorithm: "sha256", digest: []byte{5}},
	}
	got, want := sha256.New(), sha256.New()
	var buffer []byte
	for _, record := range records {
		buffer = comparisonHashRecord(got, "root", record, buffer)
		fingerprintField(want, []byte(comparisonRelative("root", record.path)))
		fingerprintField(want, []byte(record.algorithm))
		fingerprintField(want, record.digest)
	}
	if !bytes.Equal(got.Sum(nil), want.Sum(nil)) {
		t.Fatal("reused encoding buffer changed the canonical manifest")
	}
}

func TestComparisonStreamOrderedIndex(t *testing.T) {
	db, _ := directoryComparisonFixture(t, 100)
	query, lower := comparisonRecordQuery("tree", "", comparisonManifestColumns)
	rows, err := db.db.QueryContext(t.Context(), "EXPLAIN QUERY PLAN "+query,
		1, lower, directoryUpper("tree"), -1)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	var details []string
	for rows.Next() {
		var id, parent, unused int
		var detail string
		if err := rows.Scan(&id, &parent, &unused, &detail); err != nil {
			t.Fatal(err)
		}
		details = append(details, detail)
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	plan := strings.Join(details, "\n")
	if !strings.Contains(plan, "SEARCH o USING INDEX sqlite_autoindex_observation_1") ||
		strings.Contains(plan, "TEMP B-TREE") {
		t.Fatalf("manifest stream must use the ordered snapshot/path index: %s", plan)
	}
}

func TestComparisonStreamExactVerification(t *testing.T) {
	db, _ := directoryComparisonFixture(t, 100)
	for _, tc := range []struct {
		name     string
		snapshot base.SnapshotId
		root     string
		block    []string
		equal    bool
	}{
		{"renamed tree", 2, "copy", nil, true},
		{"changed identity and extra file", 3, "filtered", nil, false},
		{"excluded changes", 3, "filtered", []string{"**/*.log"}, true},
		{"relative path mismatch", 2, "copy/data", nil, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			err := db.TransactionContext(t.Context(), func(tx *Tx) error {
				equal, err := manifestsEqual(t.Context(), tx, base.DirectoryComparisonFilters{
					SnapshotID: 1, Path: "tree", Block: tc.block,
				}, tc.snapshot, tc.root)
				if err == nil && equal != tc.equal {
					t.Fatalf("exact verification: got %v, want %v", equal, tc.equal)
				}
				return err
			})
			if err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestComparisonStreamsIndependentRecordsAndCancellation(t *testing.T) {
	db, _ := directoryComparisonFixture(t, 100)
	err := db.TransactionContext(t.Context(), func(tx *Tx) error {
		ctx, cancel := context.WithCancel(t.Context())
		defer cancel()
		left, err := openComparisonStream(ctx, tx, 1, "tree")
		if err != nil {
			return err
		}
		defer left.rows.Close()
		right, err := openComparisonStream(ctx, tx, 2, "copy")
		if err != nil {
			return err
		}
		defer right.rows.Close()
		if !left.next() {
			t.Fatalf("missing first source record: %v", left.error())
		}
		path, algorithm := left.record.path, left.record.algorithm
		digest := bytes.Clone(left.record.digest)
		for i := 0; i < 2; i++ {
			if !right.next() {
				t.Fatalf("missing candidate record: %v", right.error())
			}
		}
		if left.record.path != path || left.record.algorithm != algorithm ||
			!bytes.Equal(left.record.digest, digest) {
			t.Fatal("advancing the candidate cursor invalidated the source record")
		}

		cancel()
		for _, stream := range []*comparisonStream{left, right} {
			for i := 0; stream.next(); i++ {
				if i > 100 {
					t.Fatal("cancelled stream did not stop")
				}
			}
			if !errors.Is(stream.error(), context.Canceled) {
				t.Fatalf("cancelled stream error: %v", stream.error())
			}
			stream.rows.Close()
		}
		var value int
		return tx.QueryRowContext(t.Context(), "SELECT 1").Scan(&value)
	})
	if err != nil {
		t.Fatal(err)
	}

	for _, tc := range directoryComparisonCases() {
		page, err := db.ListDirectoryReplicas(t.Context(), tc.filters, 50,
			base.DirectoryReplicaAnchor{}, nil)
		if err != nil {
			t.Fatal(err)
		}
		checkComparisonReplicas(t, page, 100, 20, tc.name)
	}
}

func TestDirectoryComparisonStreamMetadataAndMultiplicity(t *testing.T) {
	db, err := Open(filepath.Join(t.TempDir(), "catalog.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	_, err = db.db.ExecContext(t.Context(), `INSERT INTO disk(id,label,capacity)
 VALUES(1,'source',0),(2,'copy',0),(3,'partial',0);
 INSERT INTO snapshot(id,disk_id,state,captured_at,imported_at,capture_provenance)
 VALUES(1,1,'importing','2023-01-01T00:00:00.000000000Z',
 '2023-01-01T00:00:00.000000000Z','explicit'),
 (2,2,'importing','2023-01-02T00:00:00.000000000Z',
 '2023-01-02T00:00:00.000000000Z','explicit'),
 (3,3,'importing','2023-01-02T00:00:00.000000000Z',
 '2023-01-02T00:00:00.000000000Z','explicit'),
 (4,1,'importing','2023-01-02T00:00:00.000000000Z',
 '2023-01-02T00:00:00.000000000Z','explicit');
 INSERT INTO content(id,size,hash_type,hash) VALUES
 (1,4,'sha256',X'01'),(2,NULL,'sha256',X'02'),(3,0,'sha256',X'03'),
 (4,7,'sha256',X'04'),(5,7,'sha256',X'05');
 INSERT INTO observation(snapshot_id,content_id,path) VALUES
 (1,1,'tree/a.txt'),(1,1,'tree/alias.txt'),(1,2,'tree/unknown.txt'),
 (1,3,'tree/zero.txt'),(1,4,'tree/debug.log'),
 (2,1,'copy/a.txt'),(2,1,'copy/alias.txt'),(2,2,'copy/unknown.txt'),
 (2,3,'copy/zero.txt'),(2,5,'copy/debug.log'),
 (3,1,'renamed.bin'),(3,1,'duplicate.bin'),(3,3,'zero.bin');
 INSERT INTO observation(snapshot_id,content_id,path)
 SELECT 4,content_id,'current-tree/'||substr(path,6)
 FROM observation WHERE snapshot_id=2;`)
	if err != nil {
		t.Fatal(err)
	}
	for _, id := range []int64{1, 2, 3, 4} {
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

	filters := base.DirectoryComparisonFilters{SnapshotID: 1, Path: "tree",
		Allow: []string{"**"}, Block: []string{"**/*.log"}}
	replicas, err := db.ListDirectoryReplicas(t.Context(), filters, 50,
		base.DirectoryReplicaAnchor{}, nil)
	if err != nil {
		t.Fatal(err)
	}
	want := base.DirectorySelection{RetainedFileCount: 4, ExcludedFileCount: 1,
		ContentCount: 3, KnownBytes: 8, UnknownSizeFileCount: 1}
	if replicas.Selection != want || len(replicas.Items) != 2 {
		t.Fatalf("metadata replicas: %+v", replicas)
	}
	for i, item := range replicas.Items {
		if item.Disk.Id != base.DiskId(i+1) || item.SameDisk != (i == 0) ||
			item.WholeTreeEqual || item.RetainedFileCount != 4 || item.ExcludedFileCount != 1 {
			t.Fatalf("filtered current replica of historical source: %+v", item)
		}
	}
	coverage, err := db.ListDirectoryCoverage(t.Context(), filters, 50, 0, nil)
	if err != nil {
		t.Fatal(err)
	}
	if coverage.Selection != want || len(coverage.Items) != 2 {
		t.Fatalf("metadata coverage: %+v", coverage)
	}
	full, partial := coverage.Items[0], coverage.Items[1]
	if full.Disk.Id != 2 || !full.Complete || full.CoveredFileCount != 4 ||
		full.CoveredContentCount != 3 || full.CoveredKnownBytes != 8 ||
		full.CoveredUnknownSizeFiles != 1 {
		t.Fatalf("complete coverage: %+v", full)
	}
	if partial.Disk.Id != 3 || partial.Complete || partial.CoveredFileCount != 3 ||
		partial.CoveredContentCount != 2 || partial.CoveredKnownBytes != 8 ||
		partial.CoveredUnknownSizeFiles != 0 || partial.MissingFileCount != 1 ||
		partial.MissingContentCount != 1 || partial.MissingKnownBytes != 0 ||
		partial.MissingUnknownSizeFiles != 1 {
		t.Fatalf("partial multiplicity/unknown-size coverage: %+v", partial)
	}

	filters.Block = nil
	unfiltered, err := db.ListDirectoryReplicas(t.Context(), filters, 50,
		base.DirectoryReplicaAnchor{}, nil)
	if err != nil || len(unfiltered.Items) != 0 {
		t.Fatalf("changed log identity must prevent equality: %+v: %v", unfiltered, err)
	}
	filters.Block = []string{"**"}
	empty, err := db.ListDirectoryReplicas(t.Context(), filters, 50,
		base.DirectoryReplicaAnchor{}, nil)
	if err != nil || !empty.Selection.EmptyComparison || len(empty.Items) != 0 ||
		empty.Selection.ExcludedFileCount != 5 {
		t.Fatalf("empty stream comparison: %+v: %v", empty, err)
	}

	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if _, err := db.ListDirectoryReplicas(ctx, filters, 50,
		base.DirectoryReplicaAnchor{}, nil); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancelled replicas: %v", err)
	}
	if _, err := db.ListDirectoryCoverage(ctx, filters, 50, 0, nil); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancelled coverage: %v", err)
	}
	if _, err := db.ListDirectoryCoverage(t.Context(), filters, 50, 0, nil); err != nil {
		t.Fatalf("query after cancellation: %v", err)
	}
}

func TestComparisonRecordsBounds(t *testing.T) {
	db, err := Open(filepath.Join(t.TempDir(), "catalog.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	_, err = db.db.ExecContext(t.Context(), `INSERT INTO disk(id,label,capacity) VALUES(1,'source',0);
 INSERT INTO snapshot(id,disk_id,state,captured_at,imported_at,capture_provenance)
 VALUES(1,1,'importing','2023-01-01T00:00:00.000000000Z',
 '2023-01-01T00:00:00.000000000Z','explicit');
 INSERT INTO content(id,hash_type,hash) VALUES(1,'sha256',zeroblob(32));
 WITH RECURSIVE ids(n) AS (VALUES(1) UNION ALL SELECT n+1 FROM ids WHERE n<2103)
 INSERT INTO observation(snapshot_id,content_id,path)
 SELECT 1,1,'tree/'||printf('%04d.txt',n) FROM ids;
 INSERT INTO observation(snapshot_id,content_id,path) VALUES
 (1,1,'before/file'),(1,1,'tree'),(1,1,'tree/nested/file'),(1,1,'tree0/file'),
 (1,1,'wild*/literal?.txt'),(1,1,'界/é.txt'),(1,1,'界/😀.txt'),(1,1,'界0/file');`)
	if err != nil {
		t.Fatal(err)
	}

	for _, tc := range []struct{ root, after string }{
		{"", ""}, {"", "tree/2000.txt"}, {"tree", ""}, {"tree", "before/file"},
		{"tree", "tree/"}, {"tree", "tree/1000.txt"}, {"tree", "tree/1000.txu"},
		{"tree", "tree/0104.txt"},
		{"tree", "tree0"}, {"tree/nested", ""}, {"missing", ""},
		{"wild*", ""}, {"界", ""}, {"界", "界/é.txt"},
	} {
		t.Run(tc.root+"/"+tc.after, func(t *testing.T) {
			err := db.TransactionContext(t.Context(), func(tx *Tx) error {
				rows, err := tx.QueryContext(t.Context(), `SELECT path FROM observation
 WHERE snapshot_id=1 AND path>=? AND path<? AND path>? ORDER BY path`,
					directoryPrefix(tc.root), directoryUpper(tc.root), tc.after)
				if err != nil {
					return err
				}
				var want []string
				for rows.Next() {
					var path string
					if err := rows.Scan(&path); err != nil {
						rows.Close()
						return err
					}
					want = append(want, path)
				}
				err = rows.Err()
				rows.Close()
				if err != nil {
					return err
				}

				var got []string
				after := tc.after
				for {
					records, err := comparisonRecords(t.Context(), tx, 1, tc.root, after,
						comparisonBatchSize)
					if err != nil {
						return err
					}
					for _, record := range records {
						got = append(got, record.path)
						after = record.path
					}
					if len(got) > len(want) {
						t.Fatal("paged scan returned duplicate or out-of-range paths")
					}
					if len(records) < comparisonBatchSize {
						break
					}
				}
				if !reflect.DeepEqual(got, want) {
					t.Fatalf("paged paths differ: got %d, want %d", len(got), len(want))
				}
				return nil
			})
			if err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestComparisonRecordQuerySeeksPageBound(t *testing.T) {
	db, _ := directoryComparisonFixture(t, 100)
	for _, tc := range []struct {
		after, lower, seek string
	}{
		{"", "tree/", "SeekGE"},
		{"tree/", "tree/", "SeekGT"},
		{"tree/data/file-0000050.txt", "tree/data/file-0000050.txt", "SeekGT"},
	} {
		t.Run(tc.after, func(t *testing.T) {
			query, lower := comparisonRecordQuery("tree", tc.after, comparisonRecordColumns)
			if lower != tc.lower {
				t.Fatalf("seek lower bound %q, want %q", lower, tc.lower)
			}
			rows, err := db.db.QueryContext(t.Context(), "EXPLAIN "+query,
				1, lower, directoryUpper("tree"), comparisonBatchSize)
			if err != nil {
				t.Fatal(err)
			}
			defer rows.Close()
			var lowerRegister, seekRegister int
			for rows.Next() {
				var address, p1, p2, p3, p5 int
				var opcode string
				var p4, comment any
				if err := rows.Scan(&address, &opcode, &p1, &p2, &p3, &p4, &p5,
					&comment); err != nil {
					t.Fatal(err)
				}
				if opcode == "Variable" && p1 == 2 {
					lowerRegister = p2
				}
				if opcode == tc.seek {
					// The composite observation key is (snapshot_id, path).
					seekRegister = p3 + 1
				}
			}
			if err := rows.Err(); err != nil {
				t.Fatal(err)
			}
			if lowerRegister == 0 || seekRegister != lowerRegister {
				t.Fatalf("page bound register=%d, index seek register=%d",
					lowerRegister, seekRegister)
			}
		})
	}
}

func TestDirectoryComparisonMultipleBatches(t *testing.T) {
	const count = 2300
	db, contents := directoryComparisonFixture(t, count)
	for _, tc := range directoryComparisonCases() {
		t.Run(tc.name, func(t *testing.T) {
			replicas, err := db.ListDirectoryReplicas(t.Context(), tc.filters, 50,
				base.DirectoryReplicaAnchor{}, nil)
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

func TestDirectoryComparisonDoublestarFilters(t *testing.T) {
	tests := []struct {
		name     string
		allow    []string
		block    []string
		path     string
		selected bool
	}{
		{name: "star segment", allow: []string{"*.txt"}, path: "file.txt", selected: true},
		{name: "star boundary", allow: []string{"*.txt"}, path: "nested/file.txt"},
		{name: "doublestar root", block: []string{"**/*.log"}, path: "debug.log"},
		{name: "doublestar nested", block: []string{"**/*.log"}, path: "a/debug.log"},
		{name: "question", allow: []string{"file?.txt"}, path: "file1.txt", selected: true},
		{name: "class", allow: []string{"file[0-9].txt"}, path: "file4.txt", selected: true},
		{name: "alternative", allow: []string{"*.{jpg,png}"}, path: "image.png", selected: true},
		{name: "escaped star", allow: []string{`literal\*.txt`}, path: "literal*.txt", selected: true},
		{name: "case sensitive", allow: []string{"*.TXT"}, path: "file.txt"},
		{name: "block precedence", allow: []string{"**"}, block: []string{"private/**"},
			path: "private/file", selected: false},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			filters, err := NormalizeDirectoryComparisonFilters(base.DirectoryComparisonFilters{
				SnapshotID: 1,
				Allow:      test.allow,
				Block:      test.block,
			})
			if err != nil {
				t.Fatal(err)
			}

			if selected := comparisonSelected(filters, test.path); selected != test.selected {
				t.Fatalf("selected %v, want %v", selected, test.selected)
			}
		})
	}

	for _, pattern := range []string{"", "[", "/absolute/**", "bad\x00pattern"} {
		_, err := NormalizeDirectoryComparisonFilters(base.DirectoryComparisonFilters{
			SnapshotID: 1,
			Allow:      []string{pattern},
		})
		if !errors.Is(err, ErrValidation) {
			t.Fatalf("accepted invalid pattern %q: %v", pattern, err)
		}
	}

	tooMany := make([]string, maxDirectoryPatterns+1)
	for i := range tooMany {
		tooMany[i] = "a"
	}
	if _, err := NormalizeDirectoryComparisonFilters(base.DirectoryComparisonFilters{
		SnapshotID: 1,
		Allow:      tooMany,
	}); !errors.Is(err, ErrValidation) {
		t.Fatalf("accepted too many patterns: %v", err)
	}

	if _, err := NormalizeDirectoryComparisonFilters(base.DirectoryComparisonFilters{
		SnapshotID: 1,
		Allow:      []string{strings.Repeat("a", maxDirectoryPatternBytes+1)},
	}); !errors.Is(err, ErrValidation) {
		t.Fatalf("accepted oversized pattern: %v", err)
	}
}
