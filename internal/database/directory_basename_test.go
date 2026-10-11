package database

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/omgitsmoe/coldcat/internal/base"
)

func TestDirectoryBasenameCompactPagination(t *testing.T) {
	source := filepath.Join(t.TempDir(), "v4.sqlite")
	db, err := Open(source)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if err := useV4DirectoryFiles(db); err != nil {
		t.Fatal(err)
	}
	if _, err := db.db.Exec(`INSERT INTO disk(id,label,capacity) VALUES(1,'disk',0);
INSERT INTO content(id,hash_type,hash,size) VALUES(1,'sha256',zeroblob(32),7);
PRAGMA user_version=4;`); err != nil {
		t.Fatal(err)
	}
	paths := []string{
		"root-猫", "a/!", "a/.", "a/..", "a/ space ", "a/back\\slash", "a/é",
		"a/é", "a/猫", "a/🐱", "a/branch", "a/branch/deep",
		"a/punctuation-%_[]'\"", "地域と資料/猫/猫.txt",
	}
	for id := 1; id <= 2; id++ {
		seedDirectorySnapshot(t, db, id, paths)
		if err := db.BuildDirectories(t.Context(), int64(id)); err != nil {
			t.Fatal(err)
		}
		if _, err := db.PublishImport(t.Context(), PublishImportRequest{
			SnapshotID: int64(id), AllowRepeat: true,
		}); err != nil {
			t.Fatal(err)
		}
	}
	before, err := os.ReadFile(source)
	if err != nil {
		t.Fatal(err)
	}
	output := filepath.Join(t.TempDir(), "v5.sqlite")
	if err := db.CompactInto(t.Context(), output); err != nil {
		t.Fatal(err)
	}
	after, err := os.ReadFile(source)
	if err != nil || !bytes.Equal(before, after) {
		t.Fatalf("source changed: %v", err)
	}
	target, err := Open(output)
	if err != nil {
		t.Fatal(err)
	}
	defer target.Close()
	if !target.basenameEntries {
		t.Fatal("compaction retained full-path entries")
	}
	var mismatches int
	if err := target.db.QueryRow(`SELECT COUNT(*) FROM directory_file df
JOIN observation o ON o.id=df.observation_id WHERE df.path!=o.path`).Scan(&mismatches); err != nil || mismatches != 0 {
		t.Fatalf("literal paths changed: %d, %v", mismatches, err)
	}
	for _, path := range []string{"", "a", "a/branch", "地域と資料/猫"} {
		for id := base.SnapshotId(1); id <= 2; id++ {
			want, err := db.GetDirectory(t.Context(), id, path)
			if err != nil {
				t.Fatal(err)
			}
			got, err := target.GetDirectory(t.Context(), id, path)
			if err != nil || !reflect.DeepEqual(want, got) {
				t.Fatalf("detail changed: %q, %v", path, err)
			}
			for _, mode := range []string{"direct", "recursive", "directories", "filtered"} {
				f := base.DirectoryFilters{
					SnapshotID: id, Path: path, ReplicaMetric: base.ReplicaLocations,
				}
				switch mode {
				case "recursive":
					f.Recursive = true
				case "directories":
					f.DirectoriesOnly = true
				case "filtered":
					zero := int64(0)
					f.OtherReplicas = &zero
				}
				var anchor base.DirectoryAnchor
				var state *base.CatalogState
				for pages := 0; ; pages++ {
					if pages > len(paths)*2 {
						t.Fatal("pagination did not advance")
					}
					want, err := db.ListDirectoryEntries(t.Context(), f, 1, anchor, state)
					if err != nil {
						t.Fatal(err)
					}
					got, err := target.ListDirectoryEntries(t.Context(), f, 1, anchor, state)
					if err != nil || !reflect.DeepEqual(want, got) {
						t.Fatalf("page changed: %q %s %+v: %+v, %v", path, mode, anchor, got, err)
					}
					if len(want.Items) <= 1 {
						break
					}
					item := want.Items[0]
					anchor = base.DirectoryAnchor{ID: item.ID, Kind: item.Kind}
					state = &want.Catalog
				}
			}
		}
	}
}

func TestDirectoryBasenameCompactRejectsMismatchedParent(t *testing.T) {
	db, err := Open(filepath.Join(t.TempDir(), "v4.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if err := useV4DirectoryFiles(db); err != nil {
		t.Fatal(err)
	}
	if _, err := db.db.Exec(`INSERT INTO disk(id,label,capacity) VALUES(1,'disk',0);
INSERT INTO content(id,hash_type,hash) VALUES(1,'sha256',zeroblob(32));`); err != nil {
		t.Fatal(err)
	}
	seedDirectorySnapshot(t, db, 1, []string{"a/file"})
	if err := db.BuildDirectories(t.Context(), 1); err != nil {
		t.Fatal(err)
	}
	if _, err := db.db.Exec("UPDATE directory_entry SET path='wrong/file'"); err != nil {
		t.Fatal(err)
	}
	output := filepath.Join(t.TempDir(), "output.sqlite")
	if err := db.CompactInto(t.Context(), output); err == nil ||
		!strings.Contains(err.Error(), "parent does not match") {
		t.Fatalf("rewrote mismatched literal path: %v", err)
	}
	if _, err := os.Stat(output); !os.IsNotExist(err) {
		t.Fatalf("left failed output: %v", err)
	}
}

func TestDirectoryBasenamePublicationChecks(t *testing.T) {
	for name, mutation := range map[string]string{
		"missing": `DELETE FROM directory_entry WHERE observation_id=1`,
		"name":    `UPDATE directory_entry SET name='wrong' WHERE observation_id=1`,
		"parent": `UPDATE directory_entry SET directory_id=(
 SELECT id FROM directory WHERE snapshot_id=1 AND path='b') WHERE observation_id=1`,
		"snapshot": `UPDATE directory_entry SET directory_id=(
 SELECT id FROM directory WHERE snapshot_id=2 AND path='a') WHERE observation_id=1`,
	} {
		t.Run(name, func(t *testing.T) {
			db, err := Open(filepath.Join(t.TempDir(), "catalog.sqlite"))
			if err != nil {
				t.Fatal(err)
			}
			defer db.Close()
			if _, err := db.db.Exec(`INSERT INTO disk(id,label,capacity) VALUES(1,'disk',0);
INSERT INTO content(id,hash_type,hash) VALUES(1,'sha256',zeroblob(32));`); err != nil {
				t.Fatal(err)
			}
			for id := 1; id <= 2; id++ {
				seedDirectorySnapshot(t, db, id, []string{"a/file", "b/other"})
				if err := db.BuildDirectories(t.Context(), int64(id)); err != nil {
					t.Fatal(err)
				}
			}
			if _, err := db.db.Exec(mutation); err != nil {
				t.Fatal(err)
			}
			if _, err := db.PublishImport(t.Context(), PublishImportRequest{SnapshotID: 1}); !errors.Is(err, ErrConflict) || !strings.Contains(err.Error(), "directory index") {
				t.Fatalf("published incomplete entry: %v", err)
			}
		})
	}
}

func BenchmarkCatalogBasenameCompactUpgrade(b *testing.B) {
	db := buildCatalogStorageFixture(b, useV4DirectoryFiles)
	source, err := db.Storage(b.Context())
	if err != nil {
		b.Fatal(err)
	}
	b.Logf("v4 source=%d", source.PageCount*source.PageSize)
	b.ResetTimer()
	for b.Loop() {
		output := filepath.Join(b.TempDir(), "compact.sqlite")
		if err := db.CompactInto(b.Context(), output); err != nil {
			b.Fatal(err)
		}
		b.StopTimer()
		target, err := Open(output)
		if err != nil {
			b.Fatal(err)
		}
		report, err := target.Storage(b.Context())
		if err != nil {
			b.Fatal(err)
		}
		if report.Snapshots != 4 || report.HistoricalFiles != 50000 {
			b.Fatalf("history changed: %+v", report)
		}
		b.ReportMetric(float64(report.PageCount*report.PageSize), "catalog-bytes")
		if err := target.Close(); err != nil {
			b.Fatal(err)
		}
		b.StartTimer()
	}
}
