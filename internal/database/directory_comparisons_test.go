package database

import (
	"errors"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/omgitsmoe/coldcat/internal/base"
)

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
			query, lower := comparisonRecordQuery("tree", tc.after)
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
