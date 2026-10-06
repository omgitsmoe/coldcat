package database

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"github.com/omgitsmoe/coldcat/internal/base"
)

const diskColumns = `id,label,COALESCE(notes,''),COALESCE(serial,''),capacity`

const diskPageQuery = "SELECT " + diskColumns + ` FROM disk
	WHERE id>? AND id<=? ORDER BY id LIMIT ?`

const latestDiskSnapshotQuery = "SELECT " + snapshotColumns + ` FROM snapshot
	WHERE disk_id=? AND state='complete' ORDER BY captured_at DESC,id DESC LIMIT 1`

const diskSizeQuery = `SELECT COALESCE(SUM(c.size),0),
	COUNT(*) FILTER (WHERE c.size IS NULL)
	FROM observation o JOIN content c ON c.id=o.content_id WHERE o.snapshot_id=?`

func scanDisk(row scanner) (base.Disk, error) {
	var disk base.Disk
	var capacity int64
	err := row.Scan(&disk.Id, &disk.Label, &disk.Notes, &disk.Serial, &capacity)
	if errors.Is(err, sql.ErrNoRows) {
		return disk, ErrNotFound
	}

	if err != nil {
		return disk, err
	}

	if capacity < 0 {
		return disk, fmt.Errorf("invalid stored disk capacity")
	}

	disk.Capacity = uint64(capacity)
	return disk, nil
}

func diskSummary(ctx context.Context, tx *sql.Tx, disk base.Disk) (base.DiskSummary, error) {
	result := base.DiskSummary{Disk: disk}
	snapshot, err := scanSnapshot(tx.QueryRowContext(ctx, latestDiskSnapshotQuery, disk.Id))
	if errors.Is(err, ErrNotFound) {
		return result, nil
	}

	if err != nil {
		return result, err
	}

	result.LatestSnapshot = &snapshot
	return result, nil
}

func diskDetail(ctx context.Context, tx *sql.Tx, id base.DiskId) (base.DiskDetail, error) {
	var result base.DiskDetail
	disk, err := scanDisk(tx.QueryRowContext(ctx, "SELECT "+diskColumns+" FROM disk WHERE id=?", id))
	if err != nil {
		return result, err
	}

	result.DiskSummary, err = diskSummary(ctx, tx, disk)
	if err != nil || result.LatestSnapshot == nil {
		return result, err
	}

	snapshot := result.LatestSnapshot
	cataloged := base.DiskCataloged{
		FileCount: snapshot.FileCount, ContentCount: snapshot.ContentCount,
	}
	if err := tx.QueryRowContext(ctx, diskSizeQuery, snapshot.Id).
		Scan(&cataloged.KnownBytes, &cataloged.UnknownSizeFileCount); err != nil {
		return result, fmt.Errorf("aggregate cataloged sizes: %w", err)
	}

	cataloged.SizeComplete = cataloged.UnknownSizeFileCount == 0
	result.Cataloged = &cataloged
	return result, nil
}

func (db *DB) GetDisk(ctx context.Context, id base.DiskId) (base.DiskDetail, error) {
	var result base.DiskDetail
	if id <= 0 {
		return result, fmt.Errorf("%w: invalid disk id", ErrValidation)
	}

	release, err := db.readAccess()
	if err != nil {
		return result, err
	}
	defer release()
	err = db.TransactionContext(ctx, func(tx *Tx) error {
		var err error
		result, err = diskDetail(ctx, tx.Tx, id)
		return err
	})
	return result, err
}

func (db *DB) UpdateDisk(
	ctx context.Context, id base.DiskId, req base.UpdateDiskRequest,
) (base.DiskDetail, error) {
	var result base.DiskDetail
	if id <= 0 || !(req.Label.Present || req.Notes.Present || req.Serial.Present ||
		req.Capacity.Present) {
		return result, fmt.Errorf("%w: disk id and at least one field required", ErrValidation)
	}

	if req.Label.Present && (req.Label.Value == nil || *req.Label.Value == "") {
		return result, fmt.Errorf("%w: nonempty label required", ErrValidation)
	}

	if req.Capacity.Present && (req.Capacity.Value == nil || *req.Capacity.Value < 0) {
		return result, fmt.Errorf("%w: nonnegative capacity required", ErrValidation)
	}

	release, err := db.readAccess()
	if err != nil {
		return result, err
	}
	defer release()
	err = db.TransactionContext(ctx, func(tx *Tx) error {
		updated, err := tx.ExecContext(ctx, `UPDATE disk SET
			label=CASE WHEN ? THEN ? ELSE label END,
			notes=CASE WHEN ? THEN NULLIF(?,'') ELSE notes END,
			serial=CASE WHEN ? THEN NULLIF(?,'') ELSE serial END,
			capacity=CASE WHEN ? THEN ? ELSE capacity END WHERE id=?`,
			req.Label.Present, req.Label.Value, req.Notes.Present, req.Notes.Value,
			req.Serial.Present, req.Serial.Value, req.Capacity.Present, req.Capacity.Value, id,
		)
		if err != nil {
			return err
		}

		count, err := updated.RowsAffected()
		if err != nil {
			return err
		}

		if count == 0 {
			return ErrNotFound
		}

		result, err = diskDetail(ctx, tx.Tx, id)
		return err
	})
	return result, err
}

func (db *DB) ListDisks(
	ctx context.Context, limit int, afterID, maxID base.DiskId, expected *base.CatalogState,
) (base.DiskPage, error) {
	result := base.DiskPage{Items: []base.DiskSummary{}}
	if limit < 1 || limit > 200 || afterID < 0 || maxID < 0 ||
		(expected == nil && (afterID != 0 || maxID != 0)) ||
		(expected != nil && (afterID <= 0 || maxID <= afterID || expected.Revision < 0)) {
		return result, fmt.Errorf("%w: invalid disk list request", ErrValidation)
	}

	release, err := db.readAccess()
	if err != nil {
		return result, err
	}
	defer release()
	err = db.TransactionContext(ctx, func(tx *Tx) error {
		if err := tx.QueryRowContext(ctx,
			"SELECT COALESCE(MAX(id),0) FROM snapshot WHERE state='complete'",
		).Scan(&result.Catalog.Revision); err != nil {
			return err
		}

		if expected != nil && *expected != result.Catalog {
			return ErrStaleCursor
		}

		var currentMax base.DiskId
		if err := tx.QueryRowContext(ctx, "SELECT COALESCE(MAX(id),0) FROM disk").
			Scan(&currentMax); err != nil {
			return err
		}

		if expected == nil {
			maxID = currentMax
		} else {
			var exists bool
			if err := tx.QueryRowContext(ctx, "SELECT EXISTS(SELECT 1 FROM disk WHERE id=?)", afterID).
				Scan(&exists); err != nil {
				return err
			}

			if !exists || maxID > currentMax {
				return fmt.Errorf("%w: invalid cursor anchor", ErrValidation)
			}
		}
		result.MaxID = maxID
		rows, err := tx.QueryContext(ctx, diskPageQuery, afterID, maxID, limit+1)
		if err != nil {
			return err
		}
		defer rows.Close()
		var disks []base.Disk
		for rows.Next() {
			disk, err := scanDisk(rows)
			if err != nil {
				return err
			}

			disks = append(disks, disk)
		}
		if err := rows.Err(); err != nil {
			return err
		}

		if err := rows.Close(); err != nil {
			return err
		}

		for _, disk := range disks {
			summary, err := diskSummary(ctx, tx.Tx, disk)
			if err != nil {
				return err
			}

			result.Items = append(result.Items, summary)
		}
		return nil
	})
	return result, err
}
