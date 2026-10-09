package database

import (
	"bytes"
	"context"
	"crypto/sha256"
	"database/sql"
	"fmt"
	"hash"
	"slices"
	"strings"
	"unicode/utf8"

	"github.com/bmatcuk/doublestar/v4"
	"github.com/omgitsmoe/coldcat/internal/base"
)

const (
	maxDirectoryPatterns     = 100
	maxDirectoryPatternBytes = 1024
	maxDirectoryFilterBytes  = 16384
	comparisonBatchSize      = 1000
)

func NormalizeDirectoryComparisonFilters(
	f base.DirectoryComparisonFilters,
) (base.DirectoryComparisonFilters, error) {
	if f.SnapshotID <= 0 || (f.Path != "" && !validDirectory(f.Path)) ||
		!utf8.ValidString(f.Path) {
		return f, fmt.Errorf("%w: invalid directory comparison request", ErrValidation)
	}
	if len(f.Allow) > maxDirectoryPatterns || len(f.Block) > maxDirectoryPatterns {
		return f, fmt.Errorf("%w: too many directory patterns", ErrValidation)
	}

	total := 0
	normalize := func(patterns []string) ([]string, error) {
		result := slices.Clone(patterns)
		for _, pattern := range result {
			total += len(pattern)

			if pattern == "" || len(pattern) > maxDirectoryPatternBytes ||
				!utf8.ValidString(pattern) || strings.ContainsRune(pattern, 0) ||
				strings.HasPrefix(pattern, "/") || !doublestar.ValidatePattern(pattern) {
				return nil, fmt.Errorf("%w: invalid directory pattern %q", ErrValidation, pattern)
			}
		}

		slices.Sort(result)
		return slices.Compact(result), nil
	}

	var err error
	f.Allow, err = normalize(f.Allow)
	if err != nil {
		return f, err
	}

	f.Block, err = normalize(f.Block)
	if err != nil {
		return f, err
	}
	if total > maxDirectoryFilterBytes {
		return f, fmt.Errorf("%w: directory patterns are too large", ErrValidation)
	}

	return f, nil
}

func comparisonSelected(f base.DirectoryComparisonFilters, path string) bool {
	allowed := len(f.Allow) == 0
	for _, pattern := range f.Allow {
		allowed = allowed || doublestar.MatchUnvalidated(pattern, path)
	}

	if !allowed {
		return false
	}

	for _, pattern := range f.Block {
		if doublestar.MatchUnvalidated(pattern, path) {
			return false
		}
	}

	return true
}

func comparisonRelative(root, path string) string {
	if root == "" {
		return path
	}

	return strings.TrimPrefix(path, root+"/")
}

type comparisonRecord struct {
	path      string
	contentID base.ContentId
	algorithm string
	digest    []byte
	size      sql.NullInt64
}

func comparisonRecordQuery(root, after string) (string, string) {
	lower, operator := directoryPrefix(root), ">="
	if after >= lower {
		lower, operator = after, ">"
	}

	// Separate lower bounds let SQLite seek to the prefix and rescan earlier pages.
	query := `SELECT o.path,o.content_id,c.hash_type,c.hash,c.size
 FROM observation o JOIN content c ON c.id=o.content_id
 WHERE o.snapshot_id=? AND o.path` + operator + `? AND o.path<?
 ORDER BY o.path LIMIT ?`
	return query, lower
}

func comparisonRecords(
	ctx context.Context, tx *Tx, snapshotID base.SnapshotId, root, after string, limit int,
) ([]comparisonRecord, error) {
	query, lower := comparisonRecordQuery(root, after)
	rows, err := tx.QueryContext(ctx, query, snapshotID, lower, directoryUpper(root), limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	result := make([]comparisonRecord, 0, limit)
	for rows.Next() {
		var record comparisonRecord

		if err := rows.Scan(&record.path, &record.contentID, &record.algorithm,
			&record.digest, &record.size); err != nil {
			return nil, err
		}

		result = append(result, record)
	}

	return result, rows.Err()
}

func comparisonHashRecord(h hash.Hash, root string, record comparisonRecord) {
	fingerprintField(h, []byte(comparisonRelative(root, record.path)))
	fingerprintField(h, []byte(record.algorithm))
	fingerprintField(h, record.digest)
}

func buildSourceSelection(
	ctx context.Context, tx *Tx, f base.DirectoryComparisonFilters,
) ([32]byte, base.DirectorySelection, error) {
	var digest [32]byte
	var selection base.DirectorySelection
	if _, err := tx.ExecContext(ctx, `CREATE TEMP TABLE selected_directory_content (
 content_id INTEGER PRIMARY KEY, occurrences INTEGER NOT NULL,
 known_bytes INTEGER NOT NULL, unknown_size_files INTEGER NOT NULL
) WITHOUT ROWID`); err != nil {
		return digest, selection, err
	}

	h := sha256.New()
	fingerprintField(h, []byte("coldcat-directory-comparison-v1"))
	after := ""

	for {
		records, err := comparisonRecords(ctx, tx, f.SnapshotID, f.Path, after,
			comparisonBatchSize)
		if err != nil {
			return digest, selection, err
		}
		for _, record := range records {
			after = record.path

			if !comparisonSelected(f, comparisonRelative(f.Path, record.path)) {
				selection.ExcludedFileCount++
				continue
			}

			selection.RetainedFileCount++
			comparisonHashRecord(h, f.Path, record)

			known, unknown := int64(0), int64(1)
			if record.size.Valid {
				known, unknown = record.size.Int64, 0
				selection.KnownBytes += known
			} else {
				selection.UnknownSizeFileCount++
			}

			if _, err := tx.ExecContext(ctx, `INSERT INTO selected_directory_content
 (content_id,occurrences,known_bytes,unknown_size_files) VALUES(?,1,?,?)
 ON CONFLICT(content_id) DO UPDATE SET
 occurrences=occurrences+1,known_bytes=known_bytes+excluded.known_bytes,
 unknown_size_files=unknown_size_files+excluded.unknown_size_files`,
				record.contentID, known, unknown); err != nil {
				return digest, selection, err
			}
		}

		if len(records) < comparisonBatchSize {
			break
		}
	}
	if err := tx.QueryRowContext(ctx, "SELECT COUNT(*) FROM selected_directory_content").
		Scan(&selection.ContentCount); err != nil {
		return digest, selection, err
	}

	selection.EmptyComparison = selection.RetainedFileCount == 0
	copy(digest[:], h.Sum(nil))

	return digest, selection, nil
}

func candidateManifest(
	ctx context.Context, tx *Tx, snapshotID base.SnapshotId, root string,
	f base.DirectoryComparisonFilters,
) ([32]byte, int64, int64, error) {
	var digest [32]byte
	h := sha256.New()
	fingerprintField(h, []byte("coldcat-directory-comparison-v1"))

	var retained, excluded int64
	after := ""

	for {
		records, err := comparisonRecords(ctx, tx, snapshotID, root, after, comparisonBatchSize)
		if err != nil {
			return digest, retained, excluded, err
		}
		for _, record := range records {
			after = record.path

			if !comparisonSelected(f, comparisonRelative(root, record.path)) {
				excluded++
				continue
			}

			retained++
			comparisonHashRecord(h, root, record)
		}

		if len(records) < comparisonBatchSize {
			break
		}
	}

	copy(digest[:], h.Sum(nil))

	return digest, retained, excluded, nil
}

func manifestsEqual(
	ctx context.Context, tx *Tx, source base.DirectoryComparisonFilters,
	candidateSnapshot base.SnapshotId, candidateRoot string,
) (bool, error) {
	var sourceAfter, candidateAfter string
	var sourceQueue, candidateQueue []comparisonRecord

	next := func(snapshotID base.SnapshotId, root string, after *string,
		queue *[]comparisonRecord) (*comparisonRecord, error) {
		for {
			if len(*queue) == 0 {
				records, err := comparisonRecords(
					ctx,
					tx,
					snapshotID,
					root,
					*after,
					comparisonBatchSize,
				)
				if err != nil {
					return nil, err
				}

				*queue = records
				if len(records) == 0 {
					return nil, nil
				}
			}

			record := (*queue)[0]
			*queue = (*queue)[1:]
			*after = record.path

			if comparisonSelected(source, comparisonRelative(root, record.path)) {
				return &record, nil
			}
		}
	}

	for {
		left, err := next(source.SnapshotID, source.Path, &sourceAfter, &sourceQueue)
		if err != nil {
			return false, err
		}
		right, err := next(candidateSnapshot, candidateRoot, &candidateAfter, &candidateQueue)
		if err != nil {
			return false, err
		}

		if left == nil || right == nil {
			return left == nil && right == nil, nil
		}

		if comparisonRelative(source.Path, left.path) != comparisonRelative(candidateRoot, right.path) ||
			left.algorithm != right.algorithm || !bytes.Equal(left.digest, right.digest) {
			return false, nil
		}
	}
}

type replicaCandidate struct {
	directoryID int64
	snapshotID  base.SnapshotId
	diskID      base.DiskId
	path        string
	fingerprint []byte
}

func replicaCandidates(
	ctx context.Context, tx *Tx, source base.DirectoryComparisonFilters,
	sourceFingerprint []byte, after base.DirectoryReplicaAnchor, limit int,
) ([]replicaCandidate, error) {
	query := currentSnapshots + `SELECT d.id,d.snapshot_id,cs.disk_id,d.path,d.fingerprint
 FROM directory d JOIN current_snapshot cs ON cs.id=d.snapshot_id
 WHERE (cs.disk_id,d.path,d.id)>(?,?,?) AND NOT (d.snapshot_id=? AND d.path=?)`
	args := []any{after.DiskID, after.Path, after.DirectoryID, source.SnapshotID, source.Path}
	if len(source.Allow) == 0 && len(source.Block) == 0 {
		query += " AND d.fingerprint_version=1 AND d.fingerprint=?"
		args = append(args, sourceFingerprint)
	}

	query += " ORDER BY cs.disk_id,d.path,d.id LIMIT ?"
	args = append(args, limit)
	rows, err := tx.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	result := make([]replicaCandidate, 0, limit)
	for rows.Next() {
		var candidate replicaCandidate

		if err := rows.Scan(&candidate.directoryID, &candidate.snapshotID,
			&candidate.diskID, &candidate.path, &candidate.fingerprint); err != nil {
			return nil, err
		}

		result = append(result, candidate)
	}

	return result, rows.Err()
}

func validateComparisonPage(
	limit int, expected *base.CatalogState,
) error {
	if limit < 1 || limit > 200 || (expected != nil && expected.Revision < 0) {
		return fmt.Errorf("%w: invalid directory comparison page", ErrValidation)
	}

	return nil
}

func (db *DB) ListDirectoryReplicas(
	ctx context.Context, f base.DirectoryComparisonFilters, limit int,
	after base.DirectoryReplicaAnchor, expected *base.CatalogState,
) (base.DirectoryReplicaPage, error) {
	result := base.DirectoryReplicaPage{Items: []base.DirectoryReplica{}}
	var err error
	f, err = NormalizeDirectoryComparisonFilters(f)
	if err != nil {
		return result, err
	}

	result.Filters = f
	if err := validateComparisonPage(limit, expected); err != nil ||
		(expected == nil && after != (base.DirectoryReplicaAnchor{})) ||
		(expected != nil && (after.DiskID <= 0 || after.DirectoryID <= 0)) {
		if err != nil {
			return result, err
		}
		return result, fmt.Errorf("%w: invalid directory replica page", ErrValidation)
	}

	release, err := db.readAccess()
	if err != nil {
		return result, err
	}
	defer release()

	err = db.TransactionContext(ctx, func(tx *Tx) error {
		defer tx.ExecContext(context.Background(), "DROP TABLE IF EXISTS selected_directory_content")

		result.DirectoryContext, err = directoryContext(ctx, tx, f.SnapshotID)
		if err != nil {
			return err
		}
		if expected != nil && *expected != result.Catalog {
			return ErrStaleCursor
		}
		if expected != nil {
			var valid bool

			if err := tx.QueryRowContext(ctx, currentSnapshots+`SELECT EXISTS(
 SELECT 1 FROM directory d JOIN current_snapshot cs ON cs.id=d.snapshot_id
 WHERE d.id=? AND d.path=? AND cs.disk_id=?)`, after.DirectoryID,
				after.Path, after.DiskID).Scan(&valid); err != nil {
				return err
			}
			if !valid {
				return fmt.Errorf("%w: invalid directory replica cursor anchor", ErrValidation)
			}
		}

		var sourceFingerprint []byte
		if err := tx.QueryRowContext(ctx, `SELECT fingerprint FROM directory
 WHERE snapshot_id=? AND path=?`, f.SnapshotID, f.Path).Scan(&sourceFingerprint); err != nil {
			if err == sql.ErrNoRows {
				return ErrNotFound
			}

			return err
		}

		var sourceDigest [32]byte
		if len(f.Allow) == 0 && len(f.Block) == 0 {
			summary, err := scanDirectory(tx.QueryRowContext(ctx, "SELECT "+directoryColumns+
				" FROM directory WHERE snapshot_id=? AND path=?", f.SnapshotID, f.Path))
			if err != nil {
				return err
			}

			sourceDigest, result.Selection.RetainedFileCount,
				result.Selection.ExcludedFileCount, err = candidateManifest(
				ctx,
				tx,
				f.SnapshotID,
				f.Path,
				f,
			)
			if err != nil {
				return err
			}

			result.Selection.ContentCount = summary.ContentCount
			result.Selection.KnownBytes = summary.KnownBytes
			result.Selection.UnknownSizeFileCount = summary.UnknownSizeFileCount
			result.Selection.EmptyComparison = summary.FileCount == 0
		} else {
			sourceDigest, result.Selection, err = buildSourceSelection(ctx, tx, f)
			if err != nil {
				return err
			}
		}

		if result.Selection.EmptyComparison {
			return nil
		}

		anchor := after
		for len(result.Items) <= limit {
			candidates, err := replicaCandidates(ctx, tx, f, sourceFingerprint, anchor, 200)
			if err != nil {
				return err
			}
			if len(candidates) == 0 {
				break
			}

			for _, candidate := range candidates {
				anchor = base.DirectoryReplicaAnchor{DiskID: candidate.diskID,
					Path: candidate.path, DirectoryID: candidate.directoryID}

				digest, retained, excluded, err := candidateManifest(ctx, tx,
					candidate.snapshotID, candidate.path, f)
				if err != nil {
					return err
				}
				if digest != sourceDigest || retained != result.Selection.RetainedFileCount {
					continue
				}

				equal, err := manifestsEqual(ctx, tx, f, candidate.snapshotID, candidate.path)
				if err != nil {
					return err
				}
				if !equal {
					continue
				}

				disk, err := scanDisk(tx.QueryRowContext(ctx,
					"SELECT "+diskColumns+" FROM disk WHERE id=?", candidate.diskID))
				if err != nil {
					return err
				}

				snapshot, err := scanSnapshot(tx.QueryRowContext(ctx,
					"SELECT "+snapshotColumns+" FROM snapshot WHERE id=?", candidate.snapshotID))
				if err != nil {
					return err
				}

				result.Items = append(result.Items, base.DirectoryReplica{
					DirectoryID: candidate.directoryID, Disk: disk, Snapshot: snapshot,
					Path: candidate.path, SameDisk: candidate.diskID == result.Snapshot.DiskId,
					WholeTreeEqual:    bytes.Equal(sourceFingerprint, candidate.fingerprint),
					RetainedFileCount: retained, ExcludedFileCount: excluded,
				})

				if len(result.Items) > limit {
					break
				}
			}

			if len(result.Items) > limit || len(candidates) < 200 {
				break
			}
		}

		return nil
	})

	return result, err
}

func (db *DB) ListDirectoryCoverage(
	ctx context.Context, f base.DirectoryComparisonFilters, limit int,
	after base.DiskId, expected *base.CatalogState,
) (base.DirectoryCoveragePage, error) {
	result := base.DirectoryCoveragePage{Items: []base.DirectoryCoverage{}}
	var err error
	f, err = NormalizeDirectoryComparisonFilters(f)
	if err != nil {
		return result, err
	}

	result.Filters = f
	if err := validateComparisonPage(limit, expected); err != nil || after < 0 ||
		(expected == nil && after != 0) || (expected != nil && after <= 0) {
		if err != nil {
			return result, err
		}
		return result, fmt.Errorf("%w: invalid directory coverage page", ErrValidation)
	}

	release, err := db.readAccess()
	if err != nil {
		return result, err
	}
	defer release()

	err = db.TransactionContext(ctx, func(tx *Tx) error {
		defer tx.ExecContext(context.Background(), "DROP TABLE IF EXISTS selected_directory_content")

		result.DirectoryContext, err = directoryContext(ctx, tx, f.SnapshotID)
		if err != nil {
			return err
		}
		if expected != nil && *expected != result.Catalog {
			return ErrStaleCursor
		}
		if expected != nil {
			var valid bool

			if err := tx.QueryRowContext(ctx, currentSnapshots+`SELECT EXISTS(
 SELECT 1 FROM current_snapshot WHERE disk_id=? AND disk_id!=?)`,
				after, result.Snapshot.DiskId).Scan(&valid); err != nil {
				return err
			}
			if !valid {
				return fmt.Errorf("%w: invalid directory coverage cursor anchor", ErrValidation)
			}
		}

		if _, err := scanDirectory(tx.QueryRowContext(ctx, "SELECT "+directoryColumns+
			" FROM directory WHERE snapshot_id=? AND path=?", f.SnapshotID, f.Path)); err != nil {
			return err
		}

		_, result.Selection, err = buildSourceSelection(ctx, tx, f)
		if err != nil || result.Selection.EmptyComparison {
			return err
		}

		rows, err := tx.QueryContext(ctx, currentSnapshots+`SELECT
 d.id,d.label,COALESCE(d.notes,''),COALESCE(d.serial,''),d.capacity,
 s.id,s.disk_id,s.captured_at,s.imported_at,s.capture_provenance,
 COALESCE(s.input_path,''),COALESCE(s.input_format,''),s.file_count,s.content_count
 FROM disk d JOIN current_snapshot cs ON cs.disk_id=d.id
 JOIN snapshot s ON s.id=cs.id WHERE d.id>? AND d.id!=? ORDER BY d.id LIMIT ?`,
			after, result.Snapshot.DiskId, limit+1)
		if err != nil {
			return err
		}

		type destination struct {
			disk     base.Disk
			snapshot base.Snapshot
		}

		var destinations []destination
		for rows.Next() {
			var item destination
			var capacity int64
			var captured, imported string

			if err := rows.Scan(&item.disk.Id, &item.disk.Label, &item.disk.Notes,
				&item.disk.Serial, &capacity, &item.snapshot.Id, &item.snapshot.DiskId,
				&captured, &imported, &item.snapshot.CaptureProvenance,
				&item.snapshot.InputPath, &item.snapshot.InputFormat,
				&item.snapshot.FileCount, &item.snapshot.ContentCount); err != nil {
				rows.Close()
				return err
			}

			if capacity < 0 {
				rows.Close()
				return fmt.Errorf("invalid stored disk capacity")
			}

			item.disk.Capacity = uint64(capacity)
			item.snapshot.CapturedAt, err = ParseTime(captured)
			if err == nil {
				item.snapshot.ImportedAt, err = ParseTime(imported)
			}
			if err != nil {
				rows.Close()
				return err
			}

			destinations = append(destinations, item)
		}

		err = rows.Err()
		rows.Close()
		if err != nil {
			return err
		}

		for _, destination := range destinations {
			item := base.DirectoryCoverage{Disk: destination.disk, Snapshot: destination.snapshot}

			err := tx.QueryRowContext(ctx, `SELECT
 COALESCE(SUM(sc.occurrences),0),COUNT(*),COALESCE(SUM(sc.known_bytes),0),
 COALESCE(SUM(sc.unknown_size_files),0) FROM selected_directory_content sc
 WHERE EXISTS(SELECT 1 FROM observation o WHERE o.snapshot_id=?
 AND o.content_id=sc.content_id)`, destination.snapshot.Id).Scan(
				&item.CoveredFileCount, &item.CoveredContentCount,
				&item.CoveredKnownBytes, &item.CoveredUnknownSizeFiles)
			if err != nil {
				return err
			}

			item.MissingFileCount = result.Selection.RetainedFileCount - item.CoveredFileCount
			item.MissingContentCount = result.Selection.ContentCount - item.CoveredContentCount
			item.MissingKnownBytes = result.Selection.KnownBytes - item.CoveredKnownBytes
			item.MissingUnknownSizeFiles = result.Selection.UnknownSizeFileCount -
				item.CoveredUnknownSizeFiles
			item.Complete = item.MissingFileCount == 0

			result.Items = append(result.Items, item)
		}

		return nil
	})

	return result, err
}
