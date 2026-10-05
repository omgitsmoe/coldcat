package database

import (
	"context"
	"database/sql"
	"fmt"
)

const diskSchema = `CREATE TABLE disk (
 id INTEGER PRIMARY KEY, label TEXT NOT NULL UNIQUE, notes TEXT, serial TEXT,
 capacity INTEGER NOT NULL CHECK(capacity >= 0)
);`

const inventorySchema = `
CREATE TABLE snapshot (
 id INTEGER PRIMARY KEY, disk_id INTEGER NOT NULL REFERENCES disk(id),
 state TEXT NOT NULL CHECK(state IN ('importing','complete')),
 captured_at TEXT NOT NULL, imported_at TEXT NOT NULL,
 capture_provenance TEXT NOT NULL CHECK(capture_provenance IN ('explicit','source_mtime')),
 input_path TEXT, input_format TEXT,
 file_count INTEGER CHECK(file_count >= 0), content_count INTEGER CHECK(content_count >= 0),
 CHECK(state != 'complete' OR (file_count IS NOT NULL AND content_count IS NOT NULL))
);
CREATE TABLE content (
 id INTEGER PRIMARY KEY, size INTEGER CHECK(size >= 0),
 hash_type TEXT NOT NULL, hash BLOB NOT NULL, UNIQUE(hash_type,hash)
);
CREATE TABLE observation (
 id INTEGER PRIMARY KEY, snapshot_id INTEGER NOT NULL REFERENCES snapshot(id) ON DELETE CASCADE,
 content_id INTEGER NOT NULL REFERENCES content(id), path TEXT NOT NULL, mtime TEXT,
 UNIQUE(snapshot_id,path)
);
CREATE TABLE pending_size (
 snapshot_id INTEGER NOT NULL REFERENCES snapshot(id) ON DELETE CASCADE,
 content_id INTEGER NOT NULL REFERENCES content(id), size INTEGER NOT NULL CHECK(size >= 0),
 PRIMARY KEY(snapshot_id,content_id)
);
CREATE TABLE import_content (
 snapshot_id INTEGER NOT NULL REFERENCES snapshot(id) ON DELETE CASCADE,
 content_id INTEGER NOT NULL REFERENCES content(id) ON DELETE CASCADE, PRIMARY KEY(snapshot_id,content_id)
);
CREATE INDEX snapshot_current ON snapshot(disk_id,captured_at DESC,id DESC) WHERE state='complete';
CREATE INDEX observation_content_snapshot ON observation(content_id,snapshot_id);
CREATE INDEX observation_path_snapshot ON observation(path,snapshot_id);
CREATE TRIGGER immutable_snapshot_update BEFORE UPDATE ON snapshot WHEN OLD.state='complete'
 BEGIN SELECT RAISE(ABORT,'complete snapshot is immutable'); END;
CREATE TRIGGER immutable_snapshot_delete BEFORE DELETE ON snapshot WHEN OLD.state='complete'
 BEGIN SELECT RAISE(ABORT,'complete snapshot is immutable'); END;
CREATE TRIGGER immutable_observation_insert BEFORE INSERT ON observation
 WHEN (SELECT state FROM snapshot WHERE id=NEW.snapshot_id)='complete'
 BEGIN SELECT RAISE(ABORT,'complete snapshot is immutable'); END;
CREATE TRIGGER immutable_observation_update BEFORE UPDATE ON observation
 WHEN (SELECT state FROM snapshot WHERE id=OLD.snapshot_id)='complete' OR (SELECT state FROM snapshot WHERE id=NEW.snapshot_id)='complete'
 BEGIN SELECT RAISE(ABORT,'complete snapshot is immutable'); END;
CREATE TRIGGER immutable_observation_delete BEFORE DELETE ON observation
 WHEN (SELECT state FROM snapshot WHERE id=OLD.snapshot_id)='complete'
 BEGIN SELECT RAISE(ABORT,'complete snapshot is immutable'); END;
CREATE TRIGGER immutable_content_identity BEFORE UPDATE ON content
 WHEN NEW.hash_type != OLD.hash_type OR NEW.hash != OLD.hash OR (OLD.size IS NOT NULL AND (NEW.size IS NULL OR NEW.size != OLD.size))
 BEGIN SELECT RAISE(ABORT,'content identity and known size are immutable'); END;
`

type migration func(context.Context, *Tx) error

var migrations = []migration{migrateInitial}

func (db *DB) migrate(ctx context.Context) error {
	return db.applyMigrations(ctx, migrations)
}

func (db *DB) applyMigrations(ctx context.Context, steps []migration) error {
	schemaVersion := len(steps)
	var version int
	if err := db.db.QueryRowContext(ctx, "PRAGMA user_version").Scan(&version); err != nil {
		return err
	}
	if version < 0 || version > schemaVersion {
		return fmt.Errorf("unsupported schema version %d (supported: %d)", version, schemaVersion)
	}
	for version < schemaVersion {
		next := version + 1
		if err := db.TransactionContext(ctx, func(tx *Tx) error {
			if err := steps[version](ctx, tx); err != nil {
				return err
			}
			if err := checkForeignKeys(ctx, tx); err != nil {
				return err
			}
			_, err := tx.ExecContext(ctx, fmt.Sprintf("PRAGMA user_version=%d", next))
			return err
		}); err != nil {
			return fmt.Errorf("migration %d: %w", next, err)
		}
		version = next
	}
	return db.TransactionContext(ctx, func(tx *Tx) error { return checkForeignKeys(ctx, tx) })
}

func checkForeignKeys(ctx context.Context, tx *Tx) error {
	rows, err := tx.QueryContext(ctx, "PRAGMA foreign_key_check")
	if err != nil {
		return err
	}
	defer rows.Close()
	if rows.Next() {
		var table, parent string
		var rowID sql.NullInt64
		var constraint int
		if err := rows.Scan(&table, &rowID, &parent, &constraint); err != nil {
			return err
		}
		return fmt.Errorf("foreign key violation: table %s row %v references %s", table, rowID, parent)
	}
	return rows.Err()
}

func migrateInitial(ctx context.Context, tx *Tx) error {
	var objects int
	if err := tx.QueryRowContext(ctx, "SELECT COUNT(*) FROM sqlite_schema WHERE name NOT LIKE 'sqlite_%'").Scan(&objects); err != nil {
		return err
	}
	if objects != 0 {
		return fmt.Errorf("initial schema requires an empty database; recreate the development catalog")
	}
	_, err := tx.ExecContext(ctx, diskSchema+inventorySchema)
	return err
}
