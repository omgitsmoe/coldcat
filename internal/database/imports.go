package database

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"errors"
	"fmt"

	"github.com/omgitsmoe/coldcat/internal/base"
)

func (db *DB) RecoverImports(ctx context.Context) error {
	rows, err := db.db.QueryContext(
		ctx,
		"SELECT id FROM snapshot WHERE state='importing' ORDER BY id",
	)
	if err != nil {
		return err
	}

	var ids []int64
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			rows.Close()
			return err
		}

		ids = append(ids, id)
	}

	err = rows.Err()
	rows.Close()
	if err != nil {
		return err
	}

	for _, id := range ids {
		if err := db.CleanupImport(ctx, id); err != nil {
			return fmt.Errorf("snapshot %d: %w", id, err)
		}
	}

	return nil
}

func (db *DB) CleanupImport(ctx context.Context, id int64) error {
	return db.TransactionContext(ctx, func(tx *Tx) error {
		var state string
		err := tx.QueryRowContext(ctx, "SELECT state FROM snapshot WHERE id=?", id).Scan(&state)
		if err == sql.ErrNoRows {
			return nil
		}

		if err != nil {
			return err
		}

		if state != "importing" {
			return fmt.Errorf("%w: snapshot %d is not importing", ErrConflict, id)
		}

		if _, err := tx.ExecContext(ctx, "DELETE FROM directory WHERE snapshot_id=?", id); err != nil {
			return err
		}

		if _, err := tx.ExecContext(ctx, "DELETE FROM observation WHERE snapshot_id=?", id); err != nil {
			return err
		}

		if _, err := tx.ExecContext(ctx, "DELETE FROM pending_size WHERE snapshot_id=?", id); err != nil {
			return err
		}
		// Ownership limits orphan cleanup to content introduced by this import.
		if _, err := tx.ExecContext(ctx, `DELETE FROM content WHERE id IN (SELECT content_id FROM import_content WHERE snapshot_id=?)
 AND NOT EXISTS(SELECT 1 FROM observation WHERE content_id=content.id)
 AND NOT EXISTS(SELECT 1 FROM pending_size WHERE content_id=content.id)
 AND NOT EXISTS(SELECT 1 FROM import_content WHERE content_id=content.id AND snapshot_id!=?)`, id, id); err != nil {
			return err
		}

		_, err = tx.ExecContext(ctx, "DELETE FROM snapshot WHERE id=?", id)
		return err
	})
}

type PublishImportRequest struct {
	SnapshotID   int64
	SourceDigest [sha256.Size]byte
	AllowRepeat  bool
}

type DuplicateImportError struct {
	SnapshotID base.SnapshotId
	DiskID     base.DiskId
}

func (e *DuplicateImportError) Error() string {
	return fmt.Sprintf(
		"inventory already imported as snapshot %d for disk %d; use --allow-repeat to record another snapshot explicitly",
		e.SnapshotID,
		e.DiskID,
	)
}

func (e *DuplicateImportError) Unwrap() error { return ErrConflict }

func (db *DB) PublishImport(ctx context.Context, req PublishImportRequest) (base.Snapshot, error) {
	id := req.SnapshotID

	var result base.Snapshot
	err := db.TransactionContext(ctx, func(tx *Tx) error {
		var state string
		var diskID base.DiskId
		var capturedAt, format string
		if err := tx.QueryRowContext(ctx, "SELECT state,disk_id,captured_at,input_format FROM snapshot WHERE id=?", id).Scan(&state, &diskID, &capturedAt, &format); err != nil {
			return err
		}

		if state != "importing" {
			return fmt.Errorf("%w: snapshot is not importing", ErrConflict)
		}
		var missingSearchPaths bool
		if err := tx.QueryRowContext(ctx, `SELECT EXISTS(
 SELECT 1 FROM observation o LEFT JOIN search_path p ON p.path=o.path
 WHERE o.snapshot_id=? AND p.id IS NULL)`,
			id).Scan(&missingSearchPaths); err != nil {
			return err
		}
		if missingSearchPaths {
			return fmt.Errorf("%w: snapshot search index is incomplete", ErrConflict)
		}

		var directoriesBuilt bool
		if err := tx.QueryRowContext(ctx,
			`SELECT EXISTS(SELECT 1 FROM directory_build WHERE snapshot_id=?)
 AND EXISTS(SELECT 1 FROM directory WHERE snapshot_id=? AND path='')
 AND NOT EXISTS(SELECT 1 FROM directory WHERE snapshot_id=?
 AND (fingerprint_version IS NULL OR fingerprint IS NULL))
 AND NOT EXISTS(SELECT 1 FROM observation o
 LEFT JOIN directory_file f ON f.observation_id=o.id
 WHERE o.snapshot_id=? AND (f.observation_id IS NULL OR f.snapshot_id!=o.snapshot_id
 OR f.path!=o.path))`, id, id, id, id).
			Scan(&directoriesBuilt); err != nil {
			return err
		}
		if !directoriesBuilt {
			return fmt.Errorf("%w: snapshot directory index is incomplete", ErrConflict)
		}

		if !req.AllowRepeat {
			var existing base.SnapshotId
			err := tx.QueryRowContext(ctx, `SELECT id FROM snapshot WHERE state='complete'
 AND disk_id=? AND input_format=? AND captured_at=? AND source_digest=? ORDER BY id LIMIT 1`, diskID, format, capturedAt, req.SourceDigest[:]).Scan(&existing)
			if err == nil {
				return &DuplicateImportError{SnapshotID: existing, DiskID: diskID}
			}

			if !errors.Is(err, sql.ErrNoRows) {
				return err
			}
		}

		var conflict int
		if err := tx.QueryRowContext(
			ctx,
			`SELECT COUNT(*)
			FROM pending_size AS p
			JOIN content AS c ON c.id = p.content_id
			WHERE p.snapshot_id = ?
			  AND c.size IS NOT NULL
			  AND c.size != p.size`,
			id,
		).Scan(&conflict); err != nil {
			return err
		}

		if conflict != 0 {
			return fmt.Errorf("%w: conflicting known content sizes", ErrConflict)
		}

		if err := enrichDirectories(ctx, tx, id); err != nil {
			return fmt.Errorf("enrich directories: %w", err)
		}

		if _, err := tx.ExecContext(ctx, `UPDATE content SET size=(SELECT size FROM pending_size WHERE snapshot_id=? AND content_id=content.id)
 WHERE size IS NULL AND id IN(SELECT content_id FROM pending_size WHERE snapshot_id=?)`, id, id); err != nil {
			return err
		}

		if _, err := tx.ExecContext(ctx, "DELETE FROM import_content WHERE snapshot_id=?", id); err != nil {
			return err
		}

		_, err := tx.ExecContext(
			ctx,
			`UPDATE snapshot SET state='complete',source_digest=?,file_count=(SELECT COUNT(*) FROM observation WHERE snapshot_id=?),
 content_count=(SELECT COUNT(DISTINCT content_id) FROM observation WHERE snapshot_id=?) WHERE id=?`,
			req.SourceDigest[:],
			id,
			id,
			id,
		)
		if err != nil {
			return err
		}

		// Staging deletion invalidates unfinished builds; clear it only after marking complete.
		if _, err := tx.ExecContext(ctx, "DELETE FROM pending_size WHERE snapshot_id=?", id); err != nil {
			return err
		}

		result, err = scanSnapshot(
			tx.QueryRowContext(ctx, "SELECT "+snapshotColumns+" FROM snapshot WHERE id=?", id),
		)
		return err
	})
	return result, err
}
