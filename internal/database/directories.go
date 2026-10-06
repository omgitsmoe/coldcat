package database

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"unicode/utf8"

	"github.com/omgitsmoe/coldcat/internal/base"
)

const directoryColumns = `path,file_count,content_count,known_bytes,unknown_size_file_count,
 unique_content_known_bytes,unknown_size_content_count,max_known_mtime`

func ValidateDirectoryFilters(f base.DirectoryFilters) error {
	if f.SnapshotID <= 0 || (f.Path != "" && !validDirectoryPath(f.Path)) ||
		!utf8.ValidString(f.Path) || (f.DirectoriesOnly && f.Recursive) {
		return fmt.Errorf("%w: invalid directory request", ErrValidation)
	}
	if err := ValidateContentFilters(base.ContentFilters{
		Scope: base.ScopeCurrent, ReplicaMetric: f.ReplicaMetric,
		OtherReplicas: f.OtherReplicas, MinOtherReplicas: f.MinOtherReplicas,
		MaxOtherReplicas: f.MaxOtherReplicas,
	}); err != nil {
		return err
	}
	if f.DirectoriesOnly && (f.OtherReplicas != nil || f.MinOtherReplicas != nil ||
		f.MaxOtherReplicas != nil) {
		return fmt.Errorf("%w: replica filters apply to files", ErrValidation)
	}
	return nil
}

func scanDirectory(row scanner) (base.DirectorySummary, error) {
	var result base.DirectorySummary
	var mtime sql.NullString
	err := row.Scan(&result.Path, &result.FileCount, &result.ContentCount,
		&result.KnownBytes, &result.UnknownSizeFileCount, &result.UniqueContentKnownBytes,
		&result.UnknownSizeContentCount, &mtime)
	if errors.Is(err, sql.ErrNoRows) {
		return result, ErrNotFound
	}
	if err != nil {
		return result, err
	}
	if mtime.Valid {
		value, err := ParseTime(mtime.String)
		if err != nil {
			return result, err
		}
		result.MaxKnownMTime = &value
	}
	return result, nil
}

func directoryContext(
	ctx context.Context, tx *Tx, id base.SnapshotId,
) (base.DirectoryContext, error) {
	var result base.DirectoryContext
	var err error
	result.Snapshot, err = scanSnapshot(tx.QueryRowContext(ctx,
		"SELECT "+snapshotColumns+" FROM snapshot WHERE id=? AND state='complete'", id))
	if err != nil {
		return result, err
	}
	err = tx.QueryRowContext(ctx, `SELECT COALESCE(MAX(id),0) FROM snapshot
 WHERE state='complete'`).Scan(&result.Catalog.Revision)
	if err != nil {
		return result, err
	}
	err = tx.QueryRowContext(ctx, `SELECT id=? FROM snapshot
 WHERE disk_id=? AND state='complete' ORDER BY captured_at DESC,id DESC LIMIT 1`,
		id, result.Snapshot.DiskId).Scan(&result.IsCurrent)
	return result, err
}

func (db *DB) GetDirectory(
	ctx context.Context, id base.SnapshotId, path string,
) (base.DirectoryDetail, error) {
	result := base.DirectoryDetail{RedundancyHistogram: []base.RedundancyBucket{}}
	if err := ValidateDirectoryFilters(base.DirectoryFilters{
		SnapshotID: id, Path: path, ReplicaMetric: base.ReplicaDisks,
	}); err != nil {
		return result, err
	}
	release, err := db.readAccess()
	if err != nil {
		return result, err
	}
	defer release()
	err = db.TransactionContext(ctx, func(tx *Tx) error {
		var err error
		result.DirectoryContext, err = directoryContext(ctx, tx, id)
		if err != nil {
			return err
		}
		result.DirectorySummary, err = scanDirectory(tx.QueryRowContext(ctx,
			"SELECT "+directoryColumns+" FROM directory WHERE snapshot_id=? AND path=?", id, path))
		if err != nil {
			return err
		}
		rows, err := tx.QueryContext(ctx, currentSnapshots+`SELECT other_disks,SUM(occurrences)
 FROM (SELECT dc.occurrences,(SELECT COUNT(DISTINCT cs.disk_id) FROM observation o
 JOIN current_snapshot cs ON cs.id=o.snapshot_id
 WHERE o.content_id=dc.content_id AND cs.disk_id!=?) AS other_disks
 FROM directory_content dc WHERE dc.snapshot_id=? AND dc.directory_path=?)
 GROUP BY other_disks ORDER BY other_disks`, result.Snapshot.DiskId, id, path)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var bucket base.RedundancyBucket
			if err := rows.Scan(&bucket.OtherDiskCount, &bucket.FileCount); err != nil {
				return err
			}
			result.RedundancyHistogram = append(result.RedundancyHistogram, bucket)
		}
		return rows.Err()
	})
	return result, err
}

func directoryEntriesQuery(
	f base.DirectoryFilters, diskID base.DiskId, afterPath string, limit int,
) (string, []any) {
	// Keep the cursor anchor for validation, plus one lookahead row.
	branchLimit := limit + 2
	if f.DirectoriesOnly {
		return currentSnapshots + `, entries AS (
 SELECT path,'directory' AS kind,id,0 AS locations,0 AS disks
 FROM directory WHERE snapshot_id=? AND parent_path=? AND path>=?
 ORDER BY path LIMIT ?) `, []any{f.SnapshotID, f.Path, afterPath, branchLimit}
	}
	files := `SELECT o.id,o.content_id,o.path FROM directory_file df
 JOIN observation o ON o.id=df.observation_id
 WHERE df.snapshot_id=? AND df.directory_path=? AND df.path>=?`
	args := []any{f.SnapshotID, f.Path, afterPath}
	if f.Recursive {
		files = `SELECT id,content_id,path FROM observation
 WHERE snapshot_id=? AND path>=? AND path<?`
		args = []any{f.SnapshotID, max(directoryPrefix(f.Path), afterPath), directoryUpper(f.Path)}
	}
	if f.OtherReplicas == nil && f.MinOtherReplicas == nil && f.MaxOtherReplicas == nil {
		if f.Recursive {
			files += " ORDER BY path LIMIT ?"
		} else {
			files += " ORDER BY df.path LIMIT ?"
		}
		args = append(args, branchLimit)
	}
	query := currentSnapshots + `, files AS MATERIALIZED (` + files + `),
 content_stats AS MATERIALIZED (
 SELECT c.content_id,
 (SELECT COUNT(*) FROM observation o JOIN current_snapshot cs ON cs.id=o.snapshot_id
 WHERE o.content_id=c.content_id) AS locations,
 (SELECT COUNT(DISTINCT cs.disk_id) FROM observation o
 JOIN current_snapshot cs ON cs.id=o.snapshot_id
 WHERE o.content_id=c.content_id AND cs.disk_id!=?) AS disks
 FROM (SELECT DISTINCT content_id FROM files) c), candidates AS (
 SELECT f.*,st.locations-EXISTS(SELECT 1 FROM observation own
 WHERE own.snapshot_id=(SELECT id FROM current_snapshot WHERE disk_id=?)
 AND own.path=f.path AND own.content_id=f.content_id) AS locations,st.disks
 FROM files f JOIN content_stats st ON st.content_id=f.content_id
 ), entries AS (
 SELECT path,'file' AS kind,id,locations,disks FROM candidates
 WHERE (? IS NULL OR CASE WHEN ?='disks' THEN disks ELSE locations END=?)
 AND (? IS NULL OR CASE WHEN ?='disks' THEN disks ELSE locations END>=?)
 AND (? IS NULL OR CASE WHEN ?='disks' THEN disks ELSE locations END<=?)
 UNION ALL SELECT path,'directory',id,0,0 FROM (
 SELECT path,id FROM directory WHERE snapshot_id=? AND parent_path=? AND path>=? AND NOT ?
 ORDER BY path LIMIT ?)) `
	args = append(args, diskID, diskID,
		f.OtherReplicas, f.ReplicaMetric, f.OtherReplicas,
		f.MinOtherReplicas, f.ReplicaMetric, f.MinOtherReplicas,
		f.MaxOtherReplicas, f.ReplicaMetric, f.MaxOtherReplicas,
		f.SnapshotID, f.Path, afterPath, f.Recursive, branchLimit)
	return query, args
}

func (db *DB) ListDirectoryEntries(
	ctx context.Context, f base.DirectoryFilters, limit int, after base.DirectoryAnchor,
	expected *base.CatalogState,
) (base.DirectoryPage, error) {
	result := base.DirectoryPage{Filters: f, Items: []base.DirectoryEntry{}}
	if err := ValidateDirectoryFilters(f); err != nil {
		return result, err
	}
	if limit < 1 || limit > 200 ||
		(expected == nil && after != (base.DirectoryAnchor{})) ||
		(expected != nil && (after.ID <= 0 ||
			(after.Kind != "file" && after.Kind != "directory"))) {
		return result, fmt.Errorf("%w: invalid directory page", ErrValidation)
	}
	release, err := db.readAccess()
	if err != nil {
		return result, err
	}
	defer release()
	err = db.TransactionContext(ctx, func(tx *Tx) error {
		var err error
		result.DirectoryContext, err = directoryContext(ctx, tx, f.SnapshotID)
		if err != nil {
			return err
		}
		if expected != nil && *expected != result.Catalog {
			return ErrStaleCursor
		}
		if _, err := scanDirectory(tx.QueryRowContext(ctx,
			"SELECT "+directoryColumns+" FROM directory WHERE snapshot_id=? AND path=?",
			f.SnapshotID, f.Path)); err != nil {
			return err
		}
		var afterPath string
		if expected != nil {
			table := "observation"
			if after.Kind == "directory" {
				table = "directory"
			}
			err := tx.QueryRowContext(ctx,
				"SELECT path FROM "+table+" WHERE id=? AND snapshot_id=?",
				after.ID, f.SnapshotID).Scan(&afterPath)
			if errors.Is(err, sql.ErrNoRows) {
				return fmt.Errorf("%w: invalid directory cursor anchor", ErrValidation)
			}
			if err != nil {
				return err
			}
		}
		query, args := directoryEntriesQuery(f, result.Snapshot.DiskId, afterPath, limit)
		if expected != nil {
			var valid bool
			anchorArgs := append(append([]any{}, args...), afterPath, after.Kind)
			if err := tx.QueryRowContext(ctx, query+`SELECT EXISTS(
 SELECT 1 FROM entries WHERE path=? AND kind=?)`, anchorArgs...).Scan(&valid); err != nil {
				return err
			}
			if !valid {
				return fmt.Errorf("%w: invalid directory cursor anchor", ErrValidation)
			}
		}
		args = append(args, afterPath, after.Kind, limit+1)
		rows, err := tx.QueryContext(ctx, query+`SELECT path,kind,id,locations,disks
 FROM entries WHERE (path,kind)>(?,?) ORDER BY path,kind LIMIT ?`, args...)
		if err != nil {
			return err
		}
		for rows.Next() {
			var entry base.DirectoryEntry
			if err := rows.Scan(&entry.Path, &entry.Kind, &entry.ID,
				&entry.OtherLocationCount, &entry.OtherDiskCount); err != nil {
				rows.Close()
				return err
			}
			result.Items = append(result.Items, entry)
		}
		err = rows.Err()
		rows.Close()
		if err != nil {
			return err
		}
		for i := range result.Items {
			entry := &result.Items[i]
			if entry.Kind == "directory" {
				summary, err := scanDirectory(tx.QueryRowContext(ctx,
					"SELECT "+directoryColumns+" FROM directory WHERE snapshot_id=? AND path=?",
					f.SnapshotID, entry.Path))
				if err != nil {
					return err
				}
				entry.Directory = &summary
				continue
			}
			observation := base.FileObservation{
				Id: base.FileObservationId(entry.ID), SnapshotId: f.SnapshotID, Path: entry.Path,
			}
			var content base.Content
			var mtime sql.NullString
			var size sql.NullInt64
			var algorithm string
			if err := tx.QueryRowContext(ctx, `SELECT c.id,c.size,c.hash_type,c.hash,o.mtime
 FROM observation o JOIN content c ON c.id=o.content_id WHERE o.id=?`, entry.ID).
				Scan(&content.Id, &size, &algorithm, &content.Hash, &mtime); err != nil {
				return err
			}
			content.HashType, err = base.FromIdentifier(algorithm)
			if err != nil {
				return err
			}
			if size.Valid {
				content.Size = &size.Int64
			}
			if mtime.Valid {
				value, err := ParseTime(mtime.String)
				if err != nil {
					return err
				}
				observation.MTime = &value
			}
			observation.ContentId = content.Id
			entry.Observation, entry.Content = &observation, &content
		}
		return nil
	})
	return result, err
}
