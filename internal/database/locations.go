package database

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"github.com/omgitsmoe/coldcat/internal/base"
)

var ErrStaleCursor = errors.New("cursor belongs to a different inventory revision")

func (db *DB) catalogState(ctx context.Context) (base.CatalogState, error) {
	var state base.CatalogState
	err := db.db.QueryRowContext(ctx, `SELECT COALESCE(MAX(id),0) FROM snapshot WHERE state='complete'`).
		Scan(&state.Revision)
	return state, err
}

func (db *DB) GetCatalogState(ctx context.Context) (base.CatalogState, error) {
	release, err := db.readAccess()
	if err != nil {
		return base.CatalogState{}, err
	}
	defer release()
	return db.catalogState(ctx)
}

func (db *DB) LookupContent(
	ctx context.Context,
	algorithm string,
	hash []byte,
	scope base.Scope,
) (base.ContentSummary, error) {
	release, err := db.readAccess()
	if err != nil {
		return base.ContentSummary{}, err
	}
	defer release()
	if _, err := base.FromIdentifier(algorithm); err != nil || len(hash) == 0 {
		return base.ContentSummary{}, fmt.Errorf("%w: invalid hash identity", ErrValidation)
	}

	if scope != base.ScopeCurrent && scope != base.ScopeHistory {
		return base.ContentSummary{}, fmt.Errorf("%w: invalid scope %q", ErrValidation, scope)
	}

	var id base.ContentId
	err = db.db.QueryRowContext(ctx, `SELECT id FROM content WHERE hash_type=? AND hash=?`, algorithm, hash).
		Scan(&id)
	if errors.Is(err, sql.ErrNoRows) {
		return base.ContentSummary{}, ErrNotFound
	}

	if err != nil {
		return base.ContentSummary{}, err
	}

	return db.contentSummary(ctx, id, scope)
}

const contentObservationQuery = currentSnapshots + `SELECT
 o.id,o.snapshot_id,o.content_id,o.path,o.mtime,
 s.disk_id,s.captured_at,s.imported_at,s.capture_provenance,COALESCE(s.input_path,''),COALESCE(s.input_format,''),s.file_count,s.content_count,
 d.label,COALESCE(d.serial,''),d.capacity,c.size,cs.id IS NOT NULL
 FROM observation o JOIN snapshot s ON s.id=o.snapshot_id
 JOIN disk d ON d.id=s.disk_id JOIN content c ON c.id=o.content_id
 LEFT JOIN current_snapshot cs ON cs.id=s.id
 WHERE o.content_id=? AND o.id>? AND s.state='complete' AND (?='history' OR cs.id IS NOT NULL)
 ORDER BY o.id LIMIT ?`

func (db *DB) ListContentObservations(
	ctx context.Context,
	id base.ContentId,
	scope base.Scope,
	limit int,
	after base.FileObservationId,
	expected *base.CatalogState,
) (base.ContentObservationPage, error) {
	result := base.ContentObservationPage{Scope: scope, Items: []base.ContentObservation{}}
	if id <= 0 || limit < 1 || limit > 200 || after < 0 ||
		(scope != base.ScopeCurrent && scope != base.ScopeHistory) {
		return result, fmt.Errorf("%w: invalid observation list request", ErrValidation)
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
	if err := db.db.QueryRowContext(ctx, `SELECT EXISTS (
			SELECT 1
			FROM observation AS o
			JOIN snapshot AS s ON s.id = o.snapshot_id
			WHERE o.content_id = ?
			  AND s.state = 'complete'
		)`, id).Scan(&exists); err != nil {
		return result, err
	}

	if !exists {
		return result, ErrNotFound
	}

	if after > 0 {
		var valid bool
		if err := db.db.QueryRowContext(
			ctx,
			currentSnapshots+`SELECT EXISTS (
				SELECT 1
				FROM observation AS o
				JOIN snapshot AS s ON s.id = o.snapshot_id
				LEFT JOIN current_snapshot AS cs ON cs.id = s.id
				WHERE o.id = ?
				  AND o.content_id = ?
				  AND s.state = 'complete'
				  AND (? = 'history' OR cs.id IS NOT NULL)
			)`,
			after,
			id,
			scope,
		).Scan(&valid); err != nil {
			return result, err
		}

		if !valid {
			return result, fmt.Errorf("%w: invalid cursor anchor", ErrValidation)
		}
	}

	rows, err := db.db.QueryContext(ctx, contentObservationQuery, id, after, scope, limit+1)
	if err != nil {
		return result, err
	}
	defer rows.Close()
	for rows.Next() {
		var item base.ContentObservation
		var mtime sql.NullString
		var size sql.NullInt64
		var captured, imported string
		var capacity int64
		s := &item.Snapshot
		err := rows.Scan(
			&item.Observation.Id,
			&item.Observation.SnapshotId,
			&item.Observation.ContentId,
			&item.Observation.Path,
			&mtime,
			&s.DiskId,
			&captured,
			&imported,
			&s.CaptureProvenance,
			&s.InputPath,
			&s.InputFormat,
			&s.FileCount,
			&s.ContentCount,
			&item.Disk.Label,
			&item.Disk.Serial,
			&capacity,
			&size,
			&item.IsCurrent,
		)
		if err != nil {
			return result, err
		}

		s.Id = item.Observation.SnapshotId
		item.Disk.Id = s.DiskId
		if capacity < 0 {
			return result, fmt.Errorf("invalid stored disk capacity")
		}

		item.Disk.Capacity = uint64(capacity)
		if s.CapturedAt, err = ParseTime(captured); err != nil {
			return result, err
		}

		if s.ImportedAt, err = ParseTime(imported); err != nil {
			return result, err
		}

		if mtime.Valid {
			t, err := ParseTime(mtime.String)
			if err != nil {
				return result, err
			}

			item.Observation.MTime = &t
		}

		if size.Valid {
			item.Size = &size.Int64
		}

		result.Items = append(result.Items, item)
	}

	return result, rows.Err()
}
