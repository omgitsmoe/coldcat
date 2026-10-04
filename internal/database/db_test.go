package database

import (
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func nowFixed() time.Time {
	return time.Unix(1673815645, 0)
}

// The pool hands out connections lazily and reuses them, so a pragma set on
// one connection says nothing about the next one.
func TestForeignKeysAreEnforcedOnEveryConnection(t *testing.T) {
	db, err := Open(filepath.Join(t.TempDir(), "coldcat.sqlite"))
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer db.Close()

	// Take the single connection the pool has and release it each time, so
	// a query here cannot be reusing the same one.
	for i := range 4 {
		if err := db.Transaction(func(tx *Tx) error {
			var fk int
			if err := tx.QueryRow("PRAGMA foreign_keys").Scan(&fk); err != nil {
				return err
			}
			if fk != 1 {
				t.Fatalf("round %d: foreign_keys=%d, want 1", i, fk)
			}
			return nil
		}); err != nil {
			t.Fatalf("round %d: %v", i, err)
		}
	}
}

func TestForeignKeysRejectDanglingReferences(t *testing.T) {
	db, err := Open(filepath.Join(t.TempDir(), "coldcat.sqlite"))
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer db.Close()

	// A snapshot for a disk that was never inserted.
	err = db.Transaction(func(tx *Tx) error {
		_, err := tx.Exec(
			"INSERT INTO snapshot(disk_id, created_at) VALUES ($1, $2)",
			int64(1), FormatTime(nowFixed()))
		return err
	})
	if err == nil {
		t.Fatal("expected a foreign key error for a missing disk")
	}
	if !strings.Contains(err.Error(), "FOREIGN KEY") {
		t.Fatalf("expected a foreign key error, got: %v", err)
	}

	// An observation for a snapshot and content that were never inserted.
	err = db.Transaction(func(tx *Tx) error {
		_, err := tx.Exec(
			"INSERT INTO observation(snapshot_id, content_id, path, mtime)"+
				" VALUES ($1, $2, $3, $4)",
			int64(1), int64(1), "foo/bar", FormatTime(nowFixed()))
		return err
	})
	if err == nil {
		t.Fatal("expected a foreign key error for missing references")
	}
	if !strings.Contains(err.Error(), "FOREIGN KEY") {
		t.Fatalf("expected a foreign key error, got: %v", err)
	}
}

// A content is identified by its hash type together with its hash, so the
// same bytes under a different hash type must be storable.
func TestContentUniquenessCoversHashType(t *testing.T) {
	db, err := Open(filepath.Join(t.TempDir(), "coldcat.sqlite"))
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer db.Close()

	err = db.Transaction(func(tx *Tx) error {
		for _, hashType := range []string{"sha256", "md5", "sha1"} {
			if _, err := tx.Exec(
				"INSERT INTO content(size, hash_type, hash) VALUES ($1, $2, $3)",
				int64(1), hashType, []byte{0xde, 0xad, 0xbe, 0xef}); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		t.Fatalf("same hash under three hash types must be allowed: %v", err)
	}

	// But the same pair twice must be rejected.
	err = db.Transaction(func(tx *Tx) error {
		_, err := tx.Exec(
			"INSERT INTO content(size, hash_type, hash) VALUES ($1, $2, $3)",
			int64(1), "sha256", []byte{0xde, 0xad, 0xbe, 0xef})
		return err
	})
	if err == nil {
		t.Fatal("expected a uniqueness error for a repeated hash type and hash")
	}
	if !strings.Contains(err.Error(), "UNIQUE") {
		t.Fatalf("expected a uniqueness error, got: %v", err)
	}
}
