package database

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/omgitsmoe/coldcat/internal/base"
)

const diskSnapshotsQuery = "SELECT " + snapshotColumns + ` FROM snapshot
	WHERE disk_id=? AND state='complete'
	ORDER BY captured_at DESC,id DESC LIMIT ?`

const diskSnapshotsAfterQuery = "SELECT " + snapshotColumns + ` FROM snapshot
	WHERE disk_id=? AND state='complete' AND (captured_at,id)<(?,?)
	ORDER BY captured_at DESC,id DESC LIMIT ?`

func (db *DB) ListDiskSnapshots(
	ctx context.Context,
	id base.DiskId,
	limit int,
	afterID base.SnapshotId,
	afterCapturedAt time.Time,
	expected *base.CatalogState,
) (base.SnapshotPage, error) {
	result := base.SnapshotPage{DiskID: id, Items: []base.Snapshot{}}
	if id <= 0 || limit < 1 || limit > 200 || afterID < 0 ||
		(afterID == 0) != afterCapturedAt.IsZero() {
		return result, fmt.Errorf("%w: invalid snapshot list request", ErrValidation)
	}

	release, err := db.readAccess()
	if err != nil {
		return result, err
	}
	defer release()
	result.Catalog, err = db.catalogState(ctx)
	if err != nil {
		return result, err
	}

	if expected != nil && *expected != result.Catalog {
		return result, ErrStaleCursor
	}

	var exists bool
	if err := db.db.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM disk WHERE id=?)`, id).
		Scan(&exists); err != nil {
		return result, err
	}

	if !exists {
		return result, ErrNotFound
	}

	query := diskSnapshotsQuery
	args := []any{id, limit + 1}
	if afterID > 0 {
		anchor, err := scanSnapshot(db.db.QueryRowContext(ctx,
			"SELECT "+snapshotColumns+" FROM snapshot WHERE id=? AND state='complete'", afterID))
		if errors.Is(err, ErrNotFound) {
			return result, fmt.Errorf("%w: invalid cursor anchor", ErrValidation)
		}
		if err != nil {
			return result, err
		}

		if anchor.DiskId != id || !anchor.CapturedAt.Equal(afterCapturedAt) {
			return result, fmt.Errorf("%w: invalid cursor anchor", ErrValidation)
		}

		query = diskSnapshotsAfterQuery
		args = []any{id, FormatTime(afterCapturedAt), afterID, limit + 1}
	}

	rows, err := db.db.QueryContext(ctx, query, args...)
	if err != nil {
		return result, err
	}
	defer rows.Close()
	for rows.Next() {
		item, err := scanSnapshot(rows)
		if err != nil {
			return result, err
		}

		result.Items = append(result.Items, item)
	}

	return result, rows.Err()
}
