package database

import (
	"context"
	"database/sql"
	"fmt"

	"github.com/omgitsmoe/coldcat/internal/base"
)

func (db *DB) RecoverImports(ctx context.Context) error {
	rows, err := db.db.QueryContext(ctx, "SELECT id FROM snapshot WHERE state='importing' ORDER BY id")
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

func (db *DB) PublishImport(ctx context.Context, id int64) (base.Snapshot, error) {
	var result base.Snapshot
	err := db.TransactionContext(ctx, func(tx *Tx) error {
		var state string
		if err := tx.QueryRowContext(ctx, "SELECT state FROM snapshot WHERE id=?", id).Scan(&state); err != nil {
			return err
		}
		if state != "importing" {
			return fmt.Errorf("%w: snapshot is not importing", ErrConflict)
		}
		var conflict int
		if err := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM pending_size p JOIN content c ON c.id=p.content_id WHERE p.snapshot_id=? AND c.size IS NOT NULL AND c.size!=p.size`, id).Scan(&conflict); err != nil {
			return err
		}
		if conflict != 0 {
			return fmt.Errorf("%w: conflicting known content sizes", ErrConflict)
		}
		if _, err := tx.ExecContext(ctx, `UPDATE content SET size=(SELECT size FROM pending_size WHERE snapshot_id=? AND content_id=content.id)
 WHERE size IS NULL AND id IN(SELECT content_id FROM pending_size WHERE snapshot_id=?)`, id, id); err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, "DELETE FROM pending_size WHERE snapshot_id=?", id); err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, "DELETE FROM import_content WHERE snapshot_id=?", id); err != nil {
			return err
		}
		_, err := tx.ExecContext(ctx, `UPDATE snapshot SET state='complete',file_count=(SELECT COUNT(*) FROM observation WHERE snapshot_id=?),
 content_count=(SELECT COUNT(DISTINCT content_id) FROM observation WHERE snapshot_id=?) WHERE id=?`, id, id, id)
		if err != nil {
			return err
		}
		result, err = scanSnapshot(tx.QueryRowContext(ctx, "SELECT "+snapshotColumns+" FROM snapshot WHERE id=?", id))
		return err
	})
	return result, err
}
