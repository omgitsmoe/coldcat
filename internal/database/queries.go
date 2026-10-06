package database

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/omgitsmoe/coldcat/internal/base"
)

type Scope = base.Scope

const ScopeCurrent = base.ScopeCurrent
const ScopeHistory = base.ScopeHistory

const currentSnapshots = `WITH current_snapshot AS (
 SELECT s.id,s.disk_id FROM snapshot s WHERE s.state='complete' AND NOT EXISTS (
 SELECT 1 FROM snapshot newer WHERE newer.disk_id=s.disk_id AND newer.state='complete'
 AND (newer.captured_at,newer.id)>(s.captured_at,s.id))) `

const snapshotColumns = `id,disk_id,captured_at,imported_at,capture_provenance,COALESCE(input_path,''),COALESCE(input_format,''),file_count,content_count`

type scanner interface{ Scan(...any) error }

func scanSnapshot(row scanner) (base.Snapshot, error) {
	var s base.Snapshot
	var captured, imported string
	err := row.Scan(
		&s.Id,
		&s.DiskId,
		&captured,
		&imported,
		&s.CaptureProvenance,
		&s.InputPath,
		&s.InputFormat,
		&s.FileCount,
		&s.ContentCount,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return s, ErrNotFound
	}

	if err != nil {
		return s, err
	}

	s.CapturedAt, err = ParseTime(captured)
	if err != nil {
		return s, err
	}

	s.ImportedAt, err = ParseTime(imported)
	return s, err
}

func (db *DB) GetCompleteSnapshot(ctx context.Context, id base.SnapshotId) (base.Snapshot, error) {
	release, err := db.readAccess()
	if err != nil {
		return base.Snapshot{}, err
	}
	defer release()
	return scanSnapshot(
		db.db.QueryRowContext(
			ctx,
			"SELECT "+snapshotColumns+" FROM snapshot WHERE id=? AND state='complete'",
			id,
		),
	)
}

func (db *DB) LatestCompleteSnapshot(ctx context.Context, disk base.DiskId) (base.Snapshot, error) {
	release, err := db.readAccess()
	if err != nil {
		return base.Snapshot{}, err
	}
	defer release()
	return scanSnapshot(
		db.db.QueryRowContext(
			ctx,
			"SELECT "+snapshotColumns+" FROM snapshot WHERE disk_id=? AND state='complete' ORDER BY captured_at DESC,id DESC LIMIT 1",
			disk,
		),
	)
}

func (db *DB) GetContentSummary(
	ctx context.Context,
	id base.ContentId,
	scope Scope,
) (base.ContentSummary, error) {
	release, err := db.readAccess()
	if err != nil {
		return base.ContentSummary{}, err
	}
	defer release()
	return db.contentSummary(ctx, id, scope)
}

func (db *DB) contentSummary(
	ctx context.Context,
	id base.ContentId,
	scope Scope,
) (base.ContentSummary, error) {
	result := base.ContentSummary{Scope: scope}
	if scope != ScopeCurrent && scope != ScopeHistory {
		return result, fmt.Errorf("%w: invalid scope %q", ErrValidation, scope)
	}

	var size sql.NullInt64
	var algorithm string
	err := db.db.QueryRowContext(ctx, `SELECT id, size, hash_type, hash
		FROM content AS c
		WHERE id = ?
		  AND EXISTS (
			SELECT 1
			FROM observation AS o
			JOIN snapshot AS s ON s.id = o.snapshot_id
			WHERE o.content_id = c.id
			  AND s.state = 'complete'
		)`, id).
		Scan(&result.Content.Id, &size, &algorithm, &result.Content.Hash)
	if errors.Is(err, sql.ErrNoRows) {
		return result, ErrNotFound
	}

	if err != nil {
		return result, err
	}

	if size.Valid {
		result.Content.Size = &size.Int64
	}

	result.Content.HashType, err = base.FromIdentifier(algorithm)
	if err != nil {
		return result, err
	}

	err = db.db.QueryRowContext(ctx, currentSnapshots+`SELECT COUNT(*), COUNT(DISTINCT cs.disk_id)
		FROM observation AS o
		JOIN current_snapshot AS cs ON cs.id = o.snapshot_id
		WHERE o.content_id = ?`, id).Scan(&result.CurrentLocationCount, &result.CurrentDiskCount)
	if err != nil {
		return result, err
	}

	if err := db.db.QueryRowContext(ctx, `SELECT COUNT(*)
		FROM observation AS o
		JOIN snapshot AS s ON s.id = o.snapshot_id
		WHERE o.content_id = ?
		  AND s.state = 'complete'`, id).Scan(&result.ObservationCount); err != nil {
		return result, err
	}

	result.LocationCount = result.CurrentLocationCount
	result.DiskCount = result.CurrentDiskCount
	if scope == ScopeHistory {
		err = db.db.QueryRowContext(ctx, `SELECT COUNT(*), COUNT(DISTINCT disk_id)
			FROM (
				SELECT DISTINCT s.disk_id, o.path
				FROM observation AS o
				JOIN snapshot AS s ON s.id = o.snapshot_id
				WHERE o.content_id = ?
				  AND s.state = 'complete'
			)`, id).Scan(&result.LocationCount, &result.DiskCount)
	}

	return result, err
}

func (db *DB) GetObservationSummary(
	ctx context.Context,
	id base.FileObservationId,
) (base.ObservationSummary, error) {
	release, err := db.readAccess()
	if err != nil {
		return base.ObservationSummary{}, err
	}
	defer release()

	var result base.ObservationSummary
	var mtime sql.NullString
	var capacity int64
	err = db.db.QueryRowContext(ctx, `SELECT o.id, o.snapshot_id, o.content_id, o.path, o.mtime,
		d.id, d.label, COALESCE(d.serial, ''), d.capacity
		FROM observation AS o
		JOIN snapshot AS s ON s.id = o.snapshot_id
		JOIN disk AS d ON d.id = s.disk_id
		WHERE o.id = ?
		  AND s.state = 'complete'`, id).
		Scan(
			&result.Observation.Id, &result.Observation.SnapshotId, &result.Observation.ContentId, &result.Observation.Path, &mtime, &result.Disk.Id, &result.Disk.Label, &result.Disk.Serial, &capacity)
	if errors.Is(err, sql.ErrNoRows) {
		return result, ErrNotFound
	}

	if err != nil {
		return result, err
	}

	if capacity < 0 {
		return result, fmt.Errorf("invalid stored disk capacity")
	}

	result.Disk.Capacity = uint64(capacity)
	if mtime.Valid {
		var t time.Time
		t, err = ParseTime(mtime.String)
		if err != nil {
			return result, err
		}

		result.Observation.MTime = &t
	}

	result.Snapshot, err = scanSnapshot(
		db.db.QueryRowContext(
			ctx,
			"SELECT "+snapshotColumns+" FROM snapshot WHERE id=? AND state='complete'",
			result.Observation.SnapshotId,
		),
	)
	if err != nil {
		return result, err
	}

	result.Content, err = db.contentSummary(ctx, result.Observation.ContentId, ScopeCurrent)
	if err != nil {
		return result, err
	}

	err = db.db.QueryRowContext(ctx, currentSnapshots+`SELECT
		COUNT(*) FILTER (WHERE NOT (cs.disk_id = ? AND o.path = ?)),
		COUNT(DISTINCT cs.disk_id) FILTER (WHERE cs.disk_id != ?)
		FROM observation AS o
		JOIN current_snapshot AS cs ON cs.id = o.snapshot_id
		WHERE o.content_id = ?`,
		result.Disk.Id,
		result.Observation.Path,
		result.Disk.Id,
		result.Observation.ContentId,
	).Scan(&result.OtherLocationCount, &result.OtherDiskCount)
	return result, err
}
