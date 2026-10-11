package database

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"

	"github.com/omgitsmoe/coldcat/internal/base"
)

func TestCleanOpenDefersFullForeignKeyCheck(t *testing.T) {
	path := filepath.Join(t.TempDir(), "catalog.sqlite")
	db, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.db.Exec(`PRAGMA user_version=1;
PRAGMA foreign_keys=OFF;
INSERT INTO snapshot(disk_id,state,captured_at,imported_at,capture_provenance,
source_digest,file_count,content_count)
VALUES(999,'complete','2023','2023','explicit',zeroblob(32),0,0);`); err != nil {
		t.Fatal(err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	db, err = Open(path)
	if err != nil {
		t.Fatalf("clean open performed exhaustive validation: %v", err)
	}
	defer db.Close()
	if err := db.Check(t.Context()); err == nil || !strings.Contains(err.Error(), "foreign key") {
		t.Fatalf("explicit checker missed corruption: %v", err)
	}
	if _, err := db.db.Exec(`INSERT INTO observation(snapshot_id,content_id,path)
VALUES(999,999,'dangling')`); err == nil {
		t.Fatal("foreign keys disabled after reopen")
	}
}

func TestCompactIntoPreservesCatalog(t *testing.T) {
	source := filepath.Join(t.TempDir(), "source.sqlite")
	db, err := Open(source)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	legacySchema := strings.Replace(directoryContentSchema, ") WITHOUT ROWID;", ");", 1)
	legacyFTS := strings.Replace(searchTrigramSchema, ", detail=column, columnsize=0", "", 1)
	if _, err := db.db.Exec(`DROP TABLE directory_content;` + legacySchema + `
DROP TABLE search_trigram;` + legacyFTS + `
INSERT INTO disk(id,label,capacity) VALUES(1,'disk',0);
INSERT INTO content(id,hash_type,hash,size) VALUES(1,'sha256',zeroblob(32),7);`); err != nil {
		t.Fatal(err)
	}
	seedDirectorySnapshot(t, db, 1, []string{"a/file", "a/deep/猫", "other/file"})
	if err := db.BuildDirectories(t.Context(), 1); err != nil {
		t.Fatal(err)
	}
	if _, err := db.PublishImport(t.Context(), PublishImportRequest{SnapshotID: 1}); err != nil {
		t.Fatal(err)
	}
	seedDirectorySnapshot(t, db, 2, []string{"a/file", "history/old"})
	if err := db.BuildDirectories(t.Context(), 2); err != nil {
		t.Fatal(err)
	}
	if _, err := db.PublishImport(t.Context(), PublishImportRequest{
		SnapshotID: 2, AllowRepeat: true,
	}); err != nil {
		t.Fatal(err)
	}
	before, err := os.ReadFile(source)
	if err != nil {
		t.Fatal(err)
	}
	output := filepath.Join(t.TempDir(), "compact.sqlite")
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
	var schema string
	if err := target.db.QueryRow("SELECT sql FROM sqlite_schema WHERE name='directory_content'").
		Scan(&schema); err != nil || !strings.Contains(schema, "WITHOUT ROWID") {
		t.Fatalf("membership layout: %s, %v", schema, err)
	}
	for _, path := range []string{"", "a", "a/deep", "other"} {
		want, err := db.GetDirectory(t.Context(), 1, path)
		if err != nil {
			t.Fatal(err)
		}
		got, err := target.GetDirectory(t.Context(), 1, path)
		if err != nil || !reflect.DeepEqual(want, got) {
			t.Fatalf("directory %q changed: %+v, %v", path, got, err)
		}
	}
	filter := base.SearchFilters{Query: "file", Field: "name", Match: "substring",
		ContentFilters: base.ContentFilters{Scope: base.ScopeCurrent, ReplicaMetric: base.ReplicaDisks}}
	want, err := db.Search(t.Context(), filter, 50, base.SearchAnchor{}, nil)
	if err != nil {
		t.Fatal(err)
	}
	got, err := target.Search(t.Context(), filter, 50, base.SearchAnchor{}, nil)
	if err != nil || !reflect.DeepEqual(want, got) {
		t.Fatalf("search changed: %+v, %v", got, err)
	}
	for _, table := range []string{
		"disk", "snapshot", "content", "observation", "directory", "directory_content",
		"directory_file", "search_path", "pending_size", "import_content", "directory_build",
	} {
		read := func(db *DB) []string {
			t.Helper()
			rows, err := db.db.Query("SELECT * FROM " + table)
			if err != nil {
				t.Fatal(err)
			}
			defer rows.Close()
			columns, err := rows.Columns()
			if err != nil {
				t.Fatal(err)
			}
			var records []string
			for rows.Next() {
				values := make([]any, len(columns))
				pointers := make([]any, len(columns))
				for i := range values {
					pointers[i] = &values[i]
				}
				if err := rows.Scan(pointers...); err != nil {
					t.Fatal(err)
				}
				records = append(records, fmt.Sprintf("%#v", values))
			}
			if err := rows.Err(); err != nil {
				t.Fatal(err)
			}
			slices.Sort(records)
			return records
		}
		if !reflect.DeepEqual(read(db), read(target)) {
			t.Fatalf("%s records changed", table)
		}
	}
	seedDirectorySnapshot(t, target, 3, []string{"a/new"})
	if err := target.BuildDirectories(t.Context(), 3); err != nil {
		t.Fatal(err)
	}
	if err := target.CleanupImport(t.Context(), 3); err != nil {
		t.Fatal(err)
	}
	if err := target.Check(t.Context()); err != nil {
		t.Fatal(err)
	}
}

func TestCompactIntoRefusesExistingOutput(t *testing.T) {
	source := filepath.Join(t.TempDir(), "source.sqlite")
	db, err := Open(source)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	output := filepath.Join(t.TempDir(), "keep.sqlite")
	if err := os.WriteFile(output, []byte("user data"), 0600); err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{output, source} {
		if err := db.CompactInto(t.Context(), path); err == nil {
			t.Fatal("overwrote existing output")
		}
	}
	contents, err := os.ReadFile(output)
	if err != nil || string(contents) != "user data" {
		t.Fatalf("existing output changed: %s, %v", contents, err)
	}
}

func TestCompactIntoCancellationAndLocks(t *testing.T) {
	db, err := Open(filepath.Join(t.TempDir(), "source.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	output := filepath.Join(t.TempDir(), "output.sqlite")
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if err := db.CompactInto(ctx, output); err == nil {
		t.Fatal("ignored cancellation")
	}
	if _, err := os.Stat(output); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("left failed output: %v", err)
	}
	_, lock, err := acquireCatalogLock(output)
	if err != nil {
		t.Fatal(err)
	}
	if err := db.CompactInto(t.Context(), output); !errors.Is(err, ErrBusy) {
		t.Fatalf("ignored output lock: %v", err)
	}
	if err := lock.Close(); err != nil {
		t.Fatal(err)
	}
	db.access.Lock()
	err = db.CompactInto(t.Context(), output)
	db.access.Unlock()
	if !errors.Is(err, ErrBusy) {
		t.Fatalf("ignored source access lock: %v", err)
	}
}

func TestStorageAccountsForPages(t *testing.T) {
	db, err := Open(filepath.Join(t.TempDir(), "source.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	report, err := db.Storage(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	total := report.FreePages * report.PageSize
	for _, object := range report.Objects {
		total += object.Bytes
	}
	if len(report.Objects) == 0 || total != report.PageSize*report.PageCount {
		t.Fatalf("page accounting: %+v, total %d", report, total)
	}
}

func BenchmarkDirectoryMembershipLayout(b *testing.B) {
	for _, rowid := range []bool{true, false} {
		b.Run(fmt.Sprintf("rowid=%v", rowid), func(b *testing.B) {
			db, err := Open(filepath.Join(b.TempDir(), "catalog.sqlite"))
			if err != nil {
				b.Fatal(err)
			}
			defer db.Close()
			if rowid {
				schema := strings.Replace(directoryContentSchema, ") WITHOUT ROWID;", ");", 1)
				if _, err := db.db.Exec("DROP TABLE directory_content;" + schema); err != nil {
					b.Fatal(err)
				}
			}
			_, err = db.db.Exec(`INSERT INTO disk VALUES(1,'disk',NULL,NULL,0);
INSERT INTO snapshot(id,disk_id,state,captured_at,imported_at,capture_provenance)
VALUES(1,1,'importing','2023','2023','explicit');
WITH RECURSIVE n(i) AS (VALUES(1) UNION ALL SELECT i+1 FROM n WHERE i<1000)
INSERT INTO content(id,hash_type,hash) SELECT i,'sha256',CAST(printf('%032d',i) AS BLOB) FROM n;
WITH RECURSIVE n(i) AS (VALUES(1) UNION ALL SELECT i+1 FROM n WHERE i<50)
INSERT INTO directory(snapshot_id,path,parent_path)
SELECT 1,printf('photos/archive/2026/collection/%04d/long-directory-name',i),'' FROM n;
INSERT INTO directory_content SELECT 1,d.path,c.id,1 FROM directory d CROSS JOIN content c;`)
			if err != nil {
				b.Fatal(err)
			}
			report, err := db.Storage(b.Context())
			if err != nil {
				b.Fatal(err)
			}
			var membershipBytes int64
			for _, object := range report.Objects {
				if strings.Contains(object.Name, "directory_content") {
					membershipBytes += object.Bytes
				}
			}
			b.ReportAllocs()
			for b.Loop() {
				var total int64
				if err := db.db.QueryRow(`SELECT SUM(occurrences) FROM directory_content
WHERE snapshot_id=1 AND directory_path='photos/archive/2026/collection/0025/long-directory-name'`).
					Scan(&total); err != nil || total != 1000 {
					b.Fatalf("membership summary: %d, %v", total, err)
				}
			}
			b.ReportMetric(float64(membershipBytes), "membership-bytes")
		})
	}
}
