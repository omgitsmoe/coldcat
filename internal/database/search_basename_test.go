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

func useStoredSearchNames(db *DB) error {
	schema := strings.Replace(searchSchema,
		"name TEXT GENERATED ALWAYS AS (coldcat_basename(path)) VIRTUAL",
		"name TEXT NOT NULL", 1)
	schema = strings.ReplaceAll(schema, "INSERT INTO search_path(path) VALUES(NEW.path)",
		"INSERT INTO search_path(path,name) VALUES(NEW.path,coldcat_basename(NEW.path))")
	_, err := db.db.Exec(dropSearchTriggers + `DROP TABLE search_trigram;
DROP TABLE search_path;` + schema)
	return err
}

func TestSearchBasenameCompactPagination(t *testing.T) {
	source := filepath.Join(t.TempDir(), "v5.sqlite")
	db, err := Open(source)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if err := useStoredSearchNames(db); err != nil {
		t.Fatal(err)
	}
	if _, err := db.db.Exec(`INSERT INTO disk(id,label,capacity) VALUES(1,'disk',0);
INSERT INTO content(id,hash_type,hash,size) VALUES(1,'sha256',zeroblob(32),7);
PRAGMA user_version=5;`); err != nil {
		t.Fatal(err)
	}
	paths := []string{
		"ROOT-猫.txt", "a/DOCUMENT-猫.txt", "a/document-猫.txt", "a/.", "a/..",
		"a/back\\slash", "a/é.txt", "a/é.txt", "a/🐱.txt", "a/ space.txt ",
		"地域と資料/猫/猫.txt", "a/punctuation-%_[]'\".txt",
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
	var oldSchema string
	if err := db.db.QueryRow(`SELECT sql FROM sqlite_schema WHERE name='search_path'`).
		Scan(&oldSchema); err != nil {
		t.Fatal(err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	db, err = Open(source)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	var openedSchema string
	if err := db.db.QueryRow(`SELECT sql FROM sqlite_schema WHERE name='search_path'`).
		Scan(&openedSchema); err != nil || oldSchema != openedSchema {
		t.Fatalf("open rewrote historical search storage: %v", err)
	}
	before, err := os.ReadFile(source)
	if err != nil {
		t.Fatal(err)
	}
	output := filepath.Join(t.TempDir(), "v6.sqlite")
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
	var hidden int
	if err := target.db.QueryRow(`SELECT hidden FROM pragma_table_xinfo('search_path')
 WHERE name='name'`).Scan(&hidden); err != nil || hidden != 2 {
		t.Fatalf("basename not virtual: %d, %v", hidden, err)
	}
	for _, scope := range []base.Scope{base.ScopeCurrent, base.ScopeHistory} {
		for _, field := range []string{"name", "path"} {
			for _, match := range []string{"exact", "substring"} {
				for _, query := range []string{"DOCUMENT-猫.txt", "猫.txt", "é.txt", "é.txt", "txt", "absent"} {
					f := base.SearchFilters{Query: query, Field: field, Match: match,
						ContentFilters: base.ContentFilters{Scope: scope, ReplicaMetric: base.ReplicaDisks}}
					compareSearchBasenamePages(t, db, target, f)
					f.SnapshotID, f.Directory = 1, "a"
					compareSearchBasenamePages(t, db, target, f)
					if scope == base.ScopeCurrent {
						f.SnapshotID, f.Directory = 0, ""
						zero := int64(0)
						f.MinOtherReplicas = &zero
						compareSearchBasenamePages(t, db, target, f)
					}
				}
			}
		}
	}
	if err := target.CompactInto(t.Context(), filepath.Join(t.TempDir(), "repeat.sqlite")); err != nil {
		t.Fatal(err)
	}
	seedDirectorySnapshot(t, target, 3, []string{"new/temporary.txt", "a/DOCUMENT-猫.txt"})
	if err := target.CleanupImport(t.Context(), 3); err != nil {
		t.Fatal(err)
	}
	if err := target.Check(t.Context()); err != nil {
		t.Fatal(err)
	}
	for _, query := range []string{
		`UPDATE search_path SET path='changed'`,
		`UPDATE search_path SET id=id+100`,
		`UPDATE search_path SET sealed=0`,
		`DELETE FROM search_path`,
	} {
		if _, err := target.db.Exec(query); err == nil {
			t.Fatalf("completed search index mutation accepted: %s", query)
		}
	}
}

func compareSearchBasenamePages(t *testing.T, source, target *DB, f base.SearchFilters) {
	t.Helper()
	var anchor base.SearchAnchor
	var state *base.CatalogState
	for pages := 0; ; pages++ {
		if pages > 50 {
			t.Fatal("pagination did not advance")
		}
		want, err := source.Search(t.Context(), f, 1, anchor, state)
		if err != nil {
			t.Fatal(err)
		}
		got, err := target.Search(t.Context(), f, 1, anchor, state)
		if err != nil || !reflect.DeepEqual(want, got) {
			t.Fatalf("search changed: %+v %+v: %+v, %v", f, anchor, got, err)
		}
		if len(want.Items) <= 1 {
			break
		}
		item := want.Items[0]
		anchor = base.SearchAnchor{ID: item.Observation.Id, Path: item.Observation.Path,
			Relevance: item.Relevance}
		state = &want.Catalog
	}
}

func TestSearchBasenameCompactRejectsMismatch(t *testing.T) {
	db, err := Open(filepath.Join(t.TempDir(), "corrupt.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if err := useStoredSearchNames(db); err != nil {
		t.Fatal(err)
	}
	if _, err := db.db.Exec(`INSERT INTO search_path(path,name) VALUES('a/file','wrong')`); err != nil {
		t.Fatal(err)
	}
	output := filepath.Join(t.TempDir(), "output.sqlite")
	if err := db.CompactInto(t.Context(), output); err == nil ||
		!strings.Contains(err.Error(), "basename does not match") {
		t.Fatalf("accepted mismatched basename: %v", err)
	}
	if _, err := os.Stat(output); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("left invalid compact output: %v", err)
	}
}

func BenchmarkCatalogSearchBasenameCompactUpgrade(b *testing.B) {
	db := buildCatalogStorageFixture(b, useStoredSearchNames)
	source, err := db.Storage(b.Context())
	if err != nil {
		b.Fatal(err)
	}
	b.Logf("source bytes=%d", source.PageSize*source.PageCount)
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
		if err != nil || report.Snapshots != 4 || report.HistoricalFiles != 50000 {
			b.Fatalf("copy history: %+v, %v", report, err)
		}
		b.Logf("copy bytes=%d", report.PageSize*report.PageCount)
		if err := target.Close(); err != nil {
			b.Fatal(err)
		}
		b.StartTimer()
	}
}

func TestSearchBasenameCandidatePlan(t *testing.T) {
	db, err := Open(filepath.Join(t.TempDir(), "plan.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	for _, match := range []string{"exact", "substring"} {
		f := base.SearchFilters{Query: "猫.txt", Field: "name", Match: match,
			ContentFilters: base.ContentFilters{Scope: base.ScopeCurrent, ReplicaMetric: base.ReplicaDisks}}
		query, args := searchWindow(f, 50, base.SearchAnchor{})
		rows, err := db.db.Query("EXPLAIN "+query+"SELECT * FROM eligible", args...)
		if err != nil {
			t.Fatal(err)
		}
		for rows.Next() {
			var address, p1, p2, p3, p5 int
			var opcode string
			var p4, comment any
			if err := rows.Scan(&address, &opcode, &p1, &p2, &p3, &p4, &p5, &comment); err != nil {
				t.Fatal(err)
			}
			if text, ok := p4.(string); ok && strings.Contains(text, "coldcat_basename") {
				t.Fatal("candidate selection evaluates virtual basename")
			}
		}
		if err := rows.Err(); err != nil {
			t.Fatal(err)
		}
		if err := rows.Close(); err != nil {
			t.Fatal(err)
		}
	}
	rows, err := db.db.Query(`EXPLAIN QUERY PLAN SELECT name FROM search_path WHERE id=?`, 1)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	for rows.Next() {
		var id, parent, unused int
		var detail string
		if err := rows.Scan(&id, &parent, &unused, &detail); err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(detail, "USING INTEGER PRIMARY KEY") {
			t.Fatalf("basename retrieval is not an ID seek: %s", detail)
		}
		t.Log(detail)
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
}
