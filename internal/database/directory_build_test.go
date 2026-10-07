package database

import (
	"database/sql"
	"path/filepath"
	"testing"
)

func TestDirectoryNameFunction(t *testing.T) {
	db, err := Open(filepath.Join(t.TempDir(), "catalog.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	for _, test := range []struct{ path, parent string }{
		{"", ""}, {"file", ""}, {"dir/file", "dir"},
		{"α/日本語/file", "α/日本語"}, {" spaced /name", " spaced "},
	} {
		var parent string
		if err := db.db.QueryRow("SELECT coldcat_dirname(?)", test.path).Scan(&parent); err != nil || parent != test.parent {
			t.Fatalf("dirname(%q): %q, %v", test.path, parent, err)
		}
	}
	for _, invalid := range []any{nil, 1, []byte("path"), "\xff"} {
		var parent string
		if err := db.db.QueryRow("SELECT coldcat_dirname(?)", invalid).Scan(&parent); err == nil {
			t.Fatalf("accepted invalid directory path: %v", invalid)
		}
	}
}

func TestDirectoryBuildPropagatesMTimeFromImmediateEntries(t *testing.T) {
	db, err := Open(filepath.Join(t.TempDir(), "catalog.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if _, err := db.db.Exec(`INSERT INTO disk(id,label,capacity) VALUES(1,'disk',0);
 INSERT INTO content(id,hash_type,hash,size) VALUES(1,'sha256',zeroblob(32),7)`); err != nil {
		t.Fatal(err)
	}
	seedDirectorySnapshot(t, db, 1, []string{
		"foo", "foo/a", "foo/nested/b", "foobar/c", "α/日本語/d", "unknown/e",
	})
	if _, err := db.db.Exec(`UPDATE observation SET mtime=NULL
 WHERE snapshot_id=1 AND path IN('foo/a','unknown/e')`); err != nil {
		t.Fatal(err)
	}
	if err := db.BuildDirectories(t.Context(), 1); err != nil {
		t.Fatal(err)
	}
	for _, test := range []struct {
		path  string
		files int
		day   string
	}{
		{"", 6, "05"}, {"foo", 2, "03"}, {"foo/nested", 1, "03"},
		{"foobar", 1, "04"}, {"α", 1, "05"}, {"α/日本語", 1, "05"},
		{"unknown", 1, ""},
	} {
		var count, contents, knownBytes, uniqueBytes int
		var mtime sql.NullString
		if err := db.db.QueryRow(`SELECT file_count,content_count,known_bytes,
 unique_content_known_bytes,max_known_mtime FROM directory
 WHERE snapshot_id=1 AND path=?`, test.path).
			Scan(&count, &contents, &knownBytes, &uniqueBytes, &mtime); err != nil {
			t.Fatal(err)
		}
		if count != test.files || contents != 1 || knownBytes != test.files*7 || uniqueBytes != 7 {
			t.Fatalf("directory %q: counts %d/%d, bytes %d/%d",
				test.path, count, contents, knownBytes, uniqueBytes)
		}
		if mtime.Valid != (test.day != "") ||
			mtime.Valid && mtime.String != "2023-01-"+test.day+"T00:00:00.000000000Z" {
			t.Fatalf("directory %q: mtime %+v", test.path, mtime)
		}
	}
	var parent sql.NullString
	if err := db.db.QueryRow("SELECT parent_path FROM directory WHERE snapshot_id=1 AND path=''").
		Scan(&parent); err != nil || parent.Valid {
		t.Fatalf("root parent: %+v, %v", parent, err)
	}
	var misplaced int
	if err := db.db.QueryRow(`SELECT COUNT(*) FROM directory_file
 WHERE (path='foo' AND directory_path!='') OR
 (path='α/日本語/d' AND directory_path!='α/日本語')`).Scan(&misplaced); err != nil || misplaced != 0 {
		t.Fatalf("misplaced direct file: %d, %v", misplaced, err)
	}
}
