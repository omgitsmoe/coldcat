package database

import (
	"context"
	"database/sql"
	"fmt"
	"strings"

	"github.com/omgitsmoe/coldcat/internal/base"
)

func ValidateContentFilters(f base.ContentFilters) error {
	invalid := f.Scope != base.ScopeCurrent && f.Scope != base.ScopeHistory
	invalid = invalid || (f.ReplicaMetric != base.ReplicaDisks &&
		f.ReplicaMetric != base.ReplicaLocations) || f.DiskID < 0
	if f.Directory != "" {
		invalid = invalid || f.DiskID == 0 || strings.HasPrefix(f.Directory, "/")
		if len(f.Directory) > 1 && f.Directory[1] == ':' {
			first := f.Directory[0]
			invalid = invalid || (first >= 'A' && first <= 'Z') || (first >= 'a' && first <= 'z')
		}
		for _, part := range strings.Split(f.Directory, "/") {
			invalid = invalid || part == "" || part == "." || part == ".."
		}
	}
	invalid = invalid || strings.ContainsRune(f.Directory, 0) || len(f.Directory) > 1024
	filtered := false
	for _, n := range []*int64{f.OtherReplicas, f.MinOtherReplicas, f.MaxOtherReplicas} {
		if n != nil {
			filtered = true
			invalid = invalid || *n < 0
		}
	}
	invalid = invalid || (filtered && f.Scope != base.ScopeCurrent)
	invalid = invalid || (f.OtherReplicas != nil &&
		(f.MinOtherReplicas != nil || f.MaxOtherReplicas != nil))
	invalid = invalid || (f.MinOtherReplicas != nil && f.MaxOtherReplicas != nil &&
		*f.MinOtherReplicas > *f.MaxOtherReplicas)
	if invalid {
		return fmt.Errorf("%w: invalid content filters", ErrValidation)
	}
	return nil
}

const contentCandidates = currentSnapshots + `, candidates AS (
 SELECT c.id,c.size,c.hash_type,c.hash,
 (SELECT COUNT(*) FROM observation o JOIN current_snapshot cs ON cs.id=o.snapshot_id
 WHERE o.content_id=c.id) AS locations,
 (SELECT COUNT(DISTINCT cs.disk_id) FROM observation o
 JOIN current_snapshot cs ON cs.id=o.snapshot_id WHERE o.content_id=c.id) AS disks
 FROM content c WHERE c.id>=? AND EXISTS (
 SELECT 1 FROM observation o JOIN snapshot s ON s.id=o.snapshot_id
 LEFT JOIN current_snapshot cs ON cs.id=s.id
 WHERE o.content_id=c.id AND s.state='complete' AND (?='history' OR cs.id IS NOT NULL)
 AND (?=0 OR s.disk_id=?)
 AND (?='' OR substr(o.path,1,length(?)+1)=?||'/'))
), eligible AS (
 SELECT * FROM candidates WHERE
 (? IS NULL OR (CASE WHEN ?='disks' THEN disks ELSE locations END)-1=?)
 AND (? IS NULL OR (CASE WHEN ?='disks' THEN disks ELSE locations END)-1>=?)
 AND (? IS NULL OR (CASE WHEN ?='disks' THEN disks ELSE locations END)-1<=?)
)
`

func contentArguments(f base.ContentFilters, start base.ContentId) []any {
	return []any{start, f.Scope, f.DiskID, f.DiskID,
		f.Directory, f.Directory, f.Directory,
		f.OtherReplicas, f.ReplicaMetric, f.OtherReplicas,
		f.MinOtherReplicas, f.ReplicaMetric, f.MinOtherReplicas,
		f.MaxOtherReplicas, f.ReplicaMetric, f.MaxOtherReplicas}
}

const contentListQuery = contentCandidates + `, page AS MATERIALIZED (
 SELECT * FROM eligible WHERE id>? ORDER BY id LIMIT ?
)
SELECT p.id,p.size,p.hash_type,p.hash,p.locations,p.disks,
 (SELECT COUNT(*) FROM observation o JOIN snapshot s ON s.id=o.snapshot_id
 WHERE o.content_id=p.id AND s.state='complete'),
 CASE WHEN ?='history' THEN (SELECT COUNT(*) FROM (
 SELECT DISTINCT s.disk_id,o.path FROM observation o
 JOIN snapshot s ON s.id=o.snapshot_id WHERE o.content_id=p.id AND s.state='complete'
 )) ELSE p.locations END,
 CASE WHEN ?='history' THEN (SELECT COUNT(DISTINCT s.disk_id) FROM observation o
 JOIN snapshot s ON s.id=o.snapshot_id WHERE o.content_id=p.id AND s.state='complete'
 ) ELSE p.disks END
FROM page p ORDER BY p.id`

func (db *DB) ListContents(
	ctx context.Context, f base.ContentFilters, limit int, after base.ContentId,
	expected *base.CatalogState,
) (base.ContentPage, error) {
	result := base.ContentPage{Filters: f, Items: []base.ContentSummary{}}
	if err := ValidateContentFilters(f); err != nil {
		return result, err
	}

	if limit < 1 || limit > 200 || after < 0 {
		return result, fmt.Errorf("%w: invalid content page", ErrValidation)
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

	if f.DiskID != 0 {
		var exists bool
		if err := db.db.QueryRowContext(ctx, "SELECT EXISTS(SELECT 1 FROM disk WHERE id=?)",
			f.DiskID).Scan(&exists); err != nil {
			return result, err
		}
		if !exists {
			return result, ErrNotFound
		}
	}

	if after > 0 {
		args := append(contentArguments(f, after), after)
		var valid bool
		if err := db.db.QueryRowContext(ctx, contentCandidates+
			`SELECT EXISTS(SELECT 1 FROM eligible WHERE id=?)`, args...).Scan(&valid); err != nil {
			return result, err
		}
		if !valid {
			return result, fmt.Errorf("%w: invalid cursor anchor", ErrValidation)
		}
	}

	args := append(contentArguments(f, after), after, limit+1, f.Scope, f.Scope)
	rows, err := db.db.QueryContext(ctx, contentListQuery, args...)
	if err != nil {
		return result, err
	}
	defer rows.Close()

	for rows.Next() {
		item := base.ContentSummary{Scope: f.Scope}
		var size sql.NullInt64
		var algorithm string
		if err := rows.Scan(&item.Content.Id, &size, &algorithm, &item.Content.Hash,
			&item.CurrentLocationCount, &item.CurrentDiskCount, &item.ObservationCount,
			&item.LocationCount, &item.DiskCount); err != nil {
			return result, err
		}

		item.Content.HashType, err = base.FromIdentifier(algorithm)
		if err != nil {
			return result, err
		}
		if size.Valid {
			item.Content.Size = &size.Int64
		}
		result.Items = append(result.Items, item)
	}

	return result, rows.Err()
}
