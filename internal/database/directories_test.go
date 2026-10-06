package database

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"testing"

	"github.com/omgitsmoe/coldcat/internal/base"
)

func seedDirectorySnapshot(t *testing.T, db *DB, id int, paths []string) {
	t.Helper()
	err := db.TransactionContext(t.Context(), func(tx *Tx) error {
		_, err := tx.ExecContext(t.Context(), `INSERT INTO snapshot
 (id,disk_id,state,captured_at,imported_at,capture_provenance,input_format)
 VALUES(?,1,'importing','2023-01-01T00:00:00.000000000Z',
 '2023-01-01T00:00:00.000000000Z','explicit','cshd')`, id)
		if err != nil {
			return err
		}
		for i, path := range paths {
			if _, err := tx.ExecContext(t.Context(), `INSERT INTO observation
 (snapshot_id,content_id,path,mtime) VALUES(?,1,?,?)`, id, path,
				fmt.Sprintf("2023-01-%02dT00:00:00.000000000Z", i+1)); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}

func TestDirectoryFingerprintsAndPublicationPrerequisite(t *testing.T) {
	db, err := Open(filepath.Join(t.TempDir(), "catalog.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if _, err := db.db.Exec(`INSERT INTO disk(id,label,capacity) VALUES(1,'disk',0);
 INSERT INTO content(id,hash_type,hash) VALUES(1,'sha256',zeroblob(32));`); err != nil {
		t.Fatal(err)
	}
	seedDirectorySnapshot(t, db, 1, []string{"source/a", "source/nested/b"})
	_, err = db.PublishImport(t.Context(), PublishImportRequest{SnapshotID: 1})
	if !errors.Is(err, ErrConflict) || !strings.Contains(err.Error(), "directory index") {
		t.Fatalf("published missing directory build: %v", err)
	}
	for id, paths := range [][]string{
		{"renamed/nested/b", "renamed/a"},
		{"extra/a", "extra/nested/b", "extra/c"},
		{"moved/nested/a", "moved/b"},
		{"missing/a"},
	} {
		seedDirectorySnapshot(t, db, id+2, paths)
	}
	for id := 1; id <= 5; id++ {
		if err := db.BuildDirectories(t.Context(), int64(id)); err != nil {
			t.Fatal(err)
		}
	}
	var source []byte
	if err := db.db.QueryRow(`SELECT fingerprint FROM directory WHERE path='source'`).
		Scan(&source); err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{"renamed", "extra", "moved", "missing"} {
		var fingerprint []byte
		if err := db.db.QueryRow(`SELECT fingerprint FROM directory WHERE path=?`, path).
			Scan(&fingerprint); err != nil {
			t.Fatal(err)
		}
		if bytes.Equal(source, fingerprint) != (path == "renamed") {
			t.Fatalf("fingerprint equality for %s", path)
		}
	}
	if _, err := db.db.Exec(`UPDATE observation SET path='renamed/changed'
 WHERE snapshot_id=2 AND path='renamed/a'`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.PublishImport(t.Context(), PublishImportRequest{
		SnapshotID: 2, AllowRepeat: true,
	}); !errors.Is(err, ErrConflict) || !strings.Contains(err.Error(), "directory index") {
		t.Fatalf("published stale directory build: %v", err)
	}
	if _, err := db.db.Exec(`INSERT INTO content(id,hash_type,hash)
 VALUES(2,'sha1',zeroblob(20));`); err != nil {
		t.Fatal(err)
	}
	seedDirectorySnapshot(t, db, 6, []string{"algorithm/a", "algorithm/nested/b"})
	if _, err := db.db.Exec(`UPDATE observation SET content_id=2 WHERE snapshot_id=6`); err != nil {
		t.Fatal(err)
	}
	if err := db.BuildDirectories(t.Context(), 6); err != nil {
		t.Fatal(err)
	}
	var changed []byte
	if err := db.db.QueryRow(`SELECT fingerprint FROM directory WHERE path='algorithm'`).
		Scan(&changed); err != nil || bytes.Equal(source, changed) {
		t.Fatalf("algorithm identity ignored: %v", err)
	}
	if _, err := db.PublishImport(t.Context(), PublishImportRequest{SnapshotID: 1}); err != nil {
		t.Fatal(err)
	}
	if err := db.BuildDirectories(t.Context(), 1); !errors.Is(err, ErrConflict) {
		t.Fatalf("rebuilt complete snapshot: %v", err)
	}
	if _, err := db.db.Exec(`UPDATE directory SET fingerprint=zeroblob(32)
 WHERE snapshot_id=1 AND path='source'`); err == nil {
		t.Fatal("changed completed tree fingerprint")
	}
	if _, err := db.db.Exec(`UPDATE directory SET id=100000
 WHERE snapshot_id=1 AND path='source'`); err == nil {
		t.Fatal("changed completed directory cursor anchor")
	}
}

func TestDirectoryQueryIndexes(t *testing.T) {
	db, err := Open(filepath.Join(t.TempDir(), "catalog.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	for _, test := range []struct {
		query string
		index string
	}{
		{`SELECT path FROM directory WHERE snapshot_id=1 AND path='foo'`,
			"sqlite_autoindex_directory_1"},
		{`SELECT path FROM directory WHERE snapshot_id=1 AND parent_path='' ORDER BY path LIMIT 50`,
			"directory_parent"},
		{`SELECT path FROM directory WHERE snapshot_id=1 AND parent_path='' AND path>'foo'
 ORDER BY path LIMIT 50`, "directory_parent"},
		{`SELECT path FROM directory_file WHERE snapshot_id=1 AND directory_path='foo'
 ORDER BY path LIMIT 50`, "directory_file_parent"},
		{`SELECT path FROM directory_file WHERE snapshot_id=1 AND directory_path='foo'
 AND path>'foo/a' ORDER BY path LIMIT 50`, "directory_file_parent"},
		{`SELECT id FROM observation WHERE snapshot_id=1 AND path>='foo/' AND path<'foo0'
 ORDER BY path LIMIT 50`, "sqlite_autoindex_observation_1"},
		{`SELECT directory_path FROM directory_content WHERE content_id=1`,
			"directory_content_reverse"},
		{`SELECT path FROM directory WHERE fingerprint_version=1 AND fingerprint=zeroblob(32)`,
			"directory_fingerprint"},
	} {
		rows, err := db.db.Query("EXPLAIN QUERY PLAN " + test.query)
		if err != nil {
			t.Fatal(err)
		}
		var plan []string
		for rows.Next() {
			var id, parent, unused int
			var detail string
			if err := rows.Scan(&id, &parent, &unused, &detail); err != nil {
				t.Fatal(err)
			}
			plan = append(plan, detail)
		}
		err = rows.Err()
		rows.Close()
		if err != nil || !strings.Contains(strings.Join(plan, "\n"), test.index) {
			t.Fatalf("%s: %v, %v", test.query, plan, err)
		}
	}
}

func TestStagedSizeDeletionInvalidatesDirectoryPublication(t *testing.T) {
	db, err := Open(filepath.Join(t.TempDir(), "catalog.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if _, err := db.db.Exec(`INSERT INTO disk(id,label,capacity) VALUES(1,'disk',0);
 INSERT INTO content(id,hash_type,hash) VALUES(1,'sha256',zeroblob(32));`); err != nil {
		t.Fatal(err)
	}
	seedDirectorySnapshot(t, db, 1, []string{"tree/file"})
	if _, err := db.db.Exec(`INSERT INTO pending_size VALUES(1,1,7)`); err != nil {
		t.Fatal(err)
	}
	if err := db.BuildDirectories(t.Context(), 1); err != nil {
		t.Fatal(err)
	}
	var known int64
	if err := db.db.QueryRow(`SELECT known_bytes FROM directory
 WHERE snapshot_id=1 AND path='tree'`).Scan(&known); err != nil || known != 7 {
		t.Fatalf("staged size not included in build: %d, %v", known, err)
	}
	if err := db.TransactionContext(t.Context(), func(tx *Tx) error {
		_, err := tx.ExecContext(t.Context(), "DELETE FROM pending_size WHERE snapshot_id=1")
		return err
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := db.PublishImport(t.Context(), PublishImportRequest{
		SnapshotID: 1,
	}); !errors.Is(err, ErrConflict) || !strings.Contains(err.Error(), "directory index") {
		t.Fatalf("published stale directory sizes: %v", err)
	}
	var unchanged bool
	if err := db.db.QueryRow(`SELECT s.state='importing' AND c.size IS NULL
 FROM snapshot s JOIN content c ON c.id=1 WHERE s.id=1`).Scan(&unchanged); err != nil ||
		!unchanged {
		t.Fatalf("published metadata despite invalid build: %v, %v", unchanged, err)
	}
	if err := db.CleanupImport(t.Context(), 1); err != nil {
		t.Fatal(err)
	}
}

func TestDirectoryHistoricalCountsValidationAndRecovery(t *testing.T) {
	path := filepath.Join(t.TempDir(), "catalog.sqlite")
	db, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { db.Close() }()
	if _, err := db.db.Exec(`INSERT INTO disk(id,label,capacity) VALUES(1,'disk',0);
 INSERT INTO content(id,hash_type,hash) VALUES(1,'sha256',zeroblob(32));`); err != nil {
		t.Fatal(err)
	}
	seedDirectorySnapshot(t, db, 1, []string{"foo", "foo/a", "foo/nested/a", "foobar/a"})
	if err := db.BuildDirectories(t.Context(), 1); err != nil {
		t.Fatal(err)
	}
	if _, err := db.PublishImport(t.Context(), PublishImportRequest{SnapshotID: 1}); err != nil {
		t.Fatal(err)
	}
	filters := base.DirectoryFilters{SnapshotID: 1, ReplicaMetric: base.ReplicaDisks}
	page, err := db.ListDirectoryEntries(t.Context(), filters, 1, base.DirectoryAnchor{}, nil)
	if err != nil || len(page.Items) != 2 || page.Items[0].Kind != "directory" ||
		page.Items[1].Kind != "file" || page.Items[0].Path != "foo" || page.Items[1].Path != "foo" {
		t.Fatalf("same-name entries: %+v, %v", page, err)
	}
	seedDirectorySnapshot(t, db, 2, []string{"interrupted/deep/file"})
	if err := db.BuildDirectories(t.Context(), 2); err != nil {
		t.Fatal(err)
	}
	if _, err := db.GetDirectory(t.Context(), 2, ""); !errors.Is(err, ErrNotFound) {
		t.Fatalf("importing snapshot visible: %v", err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	db, err = Open(path)
	if err != nil {
		t.Fatal(err)
	}
	for _, table := range []string{
		"directory", "directory_content", "directory_file", "directory_build",
	} {
		var count int
		if err := db.db.QueryRow("SELECT COUNT(*) FROM " + table + " WHERE snapshot_id=2").
			Scan(&count); err != nil || count != 0 {
			t.Fatalf("recovery retained %s: %d, %v", table, count, err)
		}
	}
	page, err = db.ListDirectoryEntries(t.Context(), filters, 1,
		base.DirectoryAnchor{ID: page.Items[0].ID, Kind: "directory"}, &page.Catalog)
	if err != nil || page.Items[0].Kind != "file" {
		t.Fatalf("recovery invalidated page: %+v, %v", page, err)
	}
	seedDirectorySnapshot(t, db, 2, nil)
	if err := db.BuildDirectories(t.Context(), 2); err != nil {
		t.Fatal(err)
	}
	if _, err := db.PublishImport(t.Context(), PublishImportRequest{
		SnapshotID: 2, SourceDigest: [32]byte{2},
	}); err != nil {
		t.Fatal(err)
	}
	detail, err := db.GetDirectory(t.Context(), 1, "foo")
	if err != nil || detail.IsCurrent || fmt.Sprint(detail.RedundancyHistogram) != "[{0 2}]" {
		t.Fatalf("historical detail: %+v, %v", detail, err)
	}
	filters.Path, filters.Recursive = "foo", true
	zero := int64(0)
	filters.OtherReplicas = &zero
	page, err = db.ListDirectoryEntries(t.Context(), filters, 50, base.DirectoryAnchor{}, nil)
	if err != nil || len(page.Items) != 2 || page.Items[0].OtherLocationCount != 0 ||
		page.Items[0].OtherDiskCount != 0 {
		t.Fatalf("historical other counts: %+v, %v", page, err)
	}
	for _, path := range []string{
		"/foo", "foo/", "foo//a", "../foo", "foo/..", "C:/foo", "\x00", "\xff",
	} {
		if _, err := db.GetDirectory(t.Context(), 1, path); !errors.Is(err, ErrValidation) {
			t.Fatalf("accepted path %q: %v", path, err)
		}
	}
	if _, err := db.GetDirectory(t.Context(), 1, "absent"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("missing directory: %v", err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if _, err := db.GetDirectory(ctx, 1, ""); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancellation: %v", err)
	}
	seedDirectorySnapshot(t, db, 3, []string{"cancelled/file"})
	if err := db.BuildDirectories(ctx, 3); !errors.Is(err, context.Canceled) {
		t.Fatalf("build cancellation: %v", err)
	}
	if err := db.CleanupImport(t.Context(), 3); err != nil {
		t.Fatal(err)
	}
}
