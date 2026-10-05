package database

import (
	"database/sql"
	"fmt"

	_ "modernc.org/sqlite"
)

type DB struct {
	db *sql.DB
}

type Tx struct {
	*sql.Tx
}

// dsnParams are appended to the DSN of every connection.
//
// foreign_keys is off by default in SQLite and the pragma is per-connection,
// so it cannot be enabled once on the pool; the driver applies this to each
// connection it hands out. Without it the FOREIGN KEY clauses below are
// parsed and then never enforced.
const dsnParams = "?_foreign_keys=on"

func Open(path string) (*DB, error) {
	db, err := sql.Open("sqlite", path+dsnParams)
	if err != nil {
		return &DB{}, fmt.Errorf("failed to open database at %v: %w", path, err)
	}

	// Open() doesn't necessarily open the connection right away
	// force with Ping()
	if err := db.Ping(); err != nil {
		db.Close()
		return nil, err
	}

	result := &DB{db}
	err = result.createTables()
	if err != nil {
		db.Close()
		return &DB{}, fmt.Errorf("failed to initialize DB schema: %w", err)
	}

	return result, nil
}

func (db *DB) createTables() error {
	_, err := db.db.Exec(`
		CREATE TABLE IF NOT EXISTS disk (
			id INTEGER PRIMARY KEY,
			label TEXT NOT NULL UNIQUE,
			notes TEXT,
			serial TEXT,
			capacity INTEGER NOT NULL
		);

		CREATE TABLE IF NOT EXISTS snapshot (
			id INTEGER PRIMARY KEY,
			disk_id INTEGER NOT NULL,
			created_at TEXT NOT NULL,

			FOREIGN KEY (disk_id) REFERENCES disk(id)
		);

		CREATE TABLE IF NOT EXISTS content (
			id INTEGER PRIMARY KEY,
			size INTEGER NOT NULL,
			hash_type TEXT NOT NULL,
			hash BLOB NOT NULL,

			-- The hash alone is not the identity of a content: the same
			-- bytes hashed with a different algorithm are a different
			-- content. A UNIQUE on hash alone would collapse them.
			UNIQUE (hash_type, hash)
		);

		CREATE TABLE IF NOT EXISTS observation (
			id INTEGER PRIMARY KEY,
			snapshot_id INTEGER NOT NULL,
			content_id INTEGER NOT NULL,

			path TEXT NOT NULL,
			mtime TEXT NOT NULL,

			FOREIGN KEY (snapshot_id) REFERENCES snapshot(id),
			FOREIGN KEY (content_id) REFERENCES content(id)
		);
	`)
	return err
}

func (db *DB) Transaction(fn func(*Tx) error) error {
	tx, err := db.db.Begin()
	if err != nil {
		return err
	}

	wrapped := &Tx{tx}

	if err := fn(wrapped); err != nil {
		_ = tx.Rollback()
		return err
	}

	return tx.Commit()
}

func (db *DB) CreateDisk(label, notes, serial string, capacity int64) (int64, error) {
	var notesValue, serialValue *string
	if notes != "" {
		notesValue = &notes
	}
	if serial != "" {
		serialValue = &serial
	}

	var id int64
	err := db.Transaction(func(tx *Tx) error {
		result, err := tx.Exec(
			"INSERT INTO disk(label, notes, serial, capacity) VALUES ($1, $2, $3, $4)",
			label, notesValue, serialValue, capacity,
		)
		if err != nil {
			return err
		}

		id, err = result.LastInsertId()
		return err
	})
	if err != nil {
		return 0, fmt.Errorf("failed to create disk: %w", err)
	}
	return id, nil
}

func (db *DB) DiskIDByLabel(label string) (int64, error) {
	var id int64
	err := db.db.QueryRow("SELECT id FROM disk WHERE label = $1", label).Scan(&id)
	if err != nil {
		return 0, fmt.Errorf("failed to find disk with label %q: %w", label, err)
	}
	return id, nil
}

func (db *DB) Close() error {
	return db.db.Close()
}
