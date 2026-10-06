package database

import (
	"database/sql"
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

	// Holding connections simultaneously forces the pool to open distinct ones.
	var connections []*sql.Conn
	for i := range 4 {
		connection, err := db.db.Conn(t.Context())
		if err != nil {
			t.Fatal(err)
		}

		connections = append(connections, connection)

		var fk int
		if err := connection.QueryRowContext(t.Context(), "PRAGMA foreign_keys").Scan(&fk); err != nil {
			t.Fatal(err)
		}

		if fk != 1 {
			t.Fatalf("connection %d: foreign_keys=%d", i, fk)
		}
	}

	for _, connection := range connections {
		connection.Close()
	}
}

func TestCreateDisk(t *testing.T) {
	db, err := Open(filepath.Join(t.TempDir(), "coldcat.sqlite"))
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer db.Close()

	id, err := db.CreateDisk("archive", "important files", "ABC123", 2_000_000_000_000)
	if err != nil {
		t.Fatalf("create disk: %v", err)
	}

	var label string
	var notes, serial sql.NullString
	var capacity int64
	if err := db.db.QueryRow(
		"SELECT label, notes, serial, capacity FROM disk WHERE id = $1", id,
	).Scan(&label, &notes, &serial, &capacity); err != nil {
		t.Fatalf("query created disk: %v", err)
	}

	if label != "archive" || !notes.Valid || notes.String != "important files" || !serial.Valid ||
		serial.String != "ABC123" ||
		capacity != 2_000_000_000_000 {
		t.Fatalf(
			"unexpected disk row: label=%q notes=%+v serial=%+v capacity=%d",
			label,
			notes,
			serial,
			capacity,
		)
	}

	if _, err := db.CreateDisk("empty optional fields", "", "", 1); err != nil {
		t.Fatalf("create disk without optional fields: %v", err)
	}

	if err := db.db.QueryRow(
		"SELECT notes, serial FROM disk WHERE label = $1", "empty optional fields",
	).Scan(&notes, &serial); err != nil {
		t.Fatalf("query disk without optional fields: %v", err)
	}

	if notes.Valid || serial.Valid {
		t.Fatalf("expected optional fields to be NULL, got notes=%+v serial=%+v", notes, serial)
	}
}

func TestDiskIDByLabel(t *testing.T) {
	db, err := Open(filepath.Join(t.TempDir(), "coldcat.sqlite"))
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer db.Close()

	wantID, err := db.CreateDisk("archive", "", "", 1)
	if err != nil {
		t.Fatalf("create disk: %v", err)
	}

	gotID, err := db.DiskIDByLabel("archive")
	if err != nil {
		t.Fatalf("find disk by label: %v", err)
	}

	if gotID != wantID {
		t.Fatalf("disk ID = %d, want %d", gotID, wantID)
	}

	if _, err := db.DiskIDByLabel("missing"); err == nil ||
		!strings.Contains(err.Error(), `label "missing"`) {
		t.Fatalf("lookup for missing label returned %v; want a descriptive error", err)
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
			"INSERT INTO snapshot(disk_id,state,captured_at,imported_at,capture_provenance) VALUES ($1,'importing',$2,$2,'explicit')",
			int64(1),
			FormatTime(nowFixed()),
		)
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
