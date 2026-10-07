package database

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"unicode/utf8"

	"github.com/omgitsmoe/coldcat/internal/base"
)

func ValidateSearchFilters(f base.SearchFilters) error {
	content := f.ContentFilters
	content.Directory = ""
	if err := ValidateContentFilters(content); err != nil {
		return err
	}
	if f.Query == "" || len(f.Query) > 1024 || !utf8.ValidString(f.Query) ||
		strings.ContainsRune(f.Query, 0) || (f.Field != "name" && f.Field != "path") ||
		(f.Match != "exact" && f.Match != "substring" && f.Match != "fuzzy") || f.SnapshotID < 0 {
		return fmt.Errorf("%w: invalid search query, field, match, or snapshot", ErrValidation)
	}
	if f.Directory != "" {
		if (f.DiskID == 0 && f.SnapshotID == 0) || !validDirectory(f.Directory) ||
			!utf8.ValidString(f.Directory) {
			return fmt.Errorf("%w: invalid search directory", ErrValidation)
		}
	}
	return nil
}

func searchCandidates(f base.SearchFilters) (string, []any) {
	return searchWindow(f, 0, base.SearchAnchor{})
}

func searchWindow(f base.SearchFilters, limit int, after base.SearchAnchor) (string, []any) {
	column := "p." + f.Field
	var source string
	var args []any
	if f.Match == "exact" {
		source = "SELECT p.id,p.path,p.name FROM search_path p WHERE " + column + "=?"
		args = []any{f.Query}
	} else if utf8.RuneCountInString(f.Query) < 3 {
		source = `SELECT p.id,p.path,p.name FROM search_short g
 CROSS JOIN search_path p ON p.id=g.path_id WHERE g.field=? AND g.gram=?`
		args = []any{f.Field, f.Query}
	} else {
		source = `SELECT p.id,p.path,p.name FROM search_trigram t
 CROSS JOIN search_path p ON p.id=t.rowid WHERE search_trigram MATCH ?`
		args = []any{f.Field + `:"` + strings.ReplaceAll(f.Query, `"`, `""`) + `"`}
	}
	var ranked string
	if f.Match == "fuzzy" {
		source, args = fuzzyPaths(f)
		ranked = `ranked_paths AS MATERIALIZED (` + source + `)`
	} else {
		ranked = `retrieved_paths AS MATERIALIZED (` + source + `),
 ranked_paths AS MATERIALIZED (SELECT p.*,
 CASE WHEN ` + column + `=? THEN 0 WHEN substr(` + column + `,1,length(?))=?
 THEN 1 ELSE 2 END AS rank FROM retrieved_paths p WHERE instr(` + column + `,?)>0)`
		args = append(args, f.Query, f.Query, f.Query, f.Query)
	}
	query := currentSnapshots + `, ` + ranked + `,
 paths AS MATERIALIZED (SELECT p.* FROM ranked_paths p`
	if limit > 0 {
		// Each selected path has a qualifying observation after the anchor. Keeping
		// limit+1 paths therefore preserves the entire next page without expanding
		// every matching path into its historical observations before sorting.
		query += ` WHERE (p.rank,p.path)>=(?,?) AND EXISTS (
 SELECT 1 FROM observation o JOIN snapshot s ON s.id=o.snapshot_id
 LEFT JOIN current_snapshot cs ON cs.id=s.id
 WHERE o.path=p.path AND s.state='complete' AND (?='history' OR cs.id IS NOT NULL)
 AND (?=0 OR s.disk_id=?) AND (?=0 OR s.id=?)
 AND (?='' OR substr(o.path,1,length(?)+1)=?||'/') AND (p.rank,o.path,o.id)>(?,?,?)`
		args = append(args, searchRank(after.Relevance), after.Path, f.Scope,
			f.DiskID, f.DiskID, f.SnapshotID, f.SnapshotID,
			f.Directory, f.Directory, f.Directory,
			searchRank(after.Relevance), after.Path, after.ID)
		metric := `(SELECT `
		if f.ReplicaMetric == base.ReplicaDisks {
			metric += `COUNT(DISTINCT cc.disk_id)`
		} else {
			metric += `COUNT(*)`
		}
		metric += ` FROM observation co JOIN current_snapshot cc ON cc.id=co.snapshot_id
 WHERE co.content_id=o.content_id)-1`
		for _, bound := range []struct {
			value *int64
			op    string
		}{
			{f.OtherReplicas, "="}, {f.MinOtherReplicas, ">="}, {f.MaxOtherReplicas, "<="},
		} {
			if bound.value != nil {
				query += " AND " + metric + bound.op + "?"
				args = append(args, *bound.value)
			}
		}
		query += `) ORDER BY p.rank,p.path LIMIT ?`
		args = append(args, limit+1)
	}
	query += `), matches AS (
 SELECT o.id,o.content_id,o.snapshot_id,o.path,p.name,cs.id IS NOT NULL AS is_current,p.rank
 FROM paths p CROSS JOIN observation o ON o.path=p.path
 JOIN snapshot s ON s.id=o.snapshot_id LEFT JOIN current_snapshot cs ON cs.id=s.id
 WHERE s.state='complete' AND (?='history' OR cs.id IS NOT NULL)
 AND (?=0 OR s.disk_id=?) AND (?=0 OR s.id=?)
 AND (?='' OR substr(o.path,1,length(?)+1)=?||'/')
 ) `
	args = append(args, f.Scope,
		f.DiskID, f.DiskID, f.SnapshotID, f.SnapshotID,
		f.Directory, f.Directory, f.Directory)
	if f.OtherReplicas == nil && f.MinOtherReplicas == nil && f.MaxOtherReplicas == nil {
		return query + `, eligible AS (SELECT * FROM matches) `, args
	}
	query += `, candidate_contents AS MATERIALIZED (SELECT DISTINCT content_id FROM matches),
 candidate_counts AS MATERIALIZED (SELECT c.content_id,` + searchReplicaCounts + `
 FROM candidate_contents c), eligible AS (
 SELECT m.* FROM matches m JOIN candidate_counts cc ON cc.content_id=m.content_id WHERE
 (? IS NULL OR (CASE WHEN ?='disks' THEN disks ELSE locations END)-1=?)
 AND (? IS NULL OR (CASE WHEN ?='disks' THEN disks ELSE locations END)-1>=?)
 AND (? IS NULL OR (CASE WHEN ?='disks' THEN disks ELSE locations END)-1<=?)
 ) `
	args = append(args,
		f.OtherReplicas, f.ReplicaMetric, f.OtherReplicas,
		f.MinOtherReplicas, f.ReplicaMetric, f.MinOtherReplicas,
		f.MaxOtherReplicas, f.ReplicaMetric, f.MaxOtherReplicas)
	return query, args
}

const searchReplicaCounts = `
 (SELECT COUNT(*) FROM observation co JOIN current_snapshot cs ON cs.id=co.snapshot_id
 WHERE co.content_id=c.content_id) AS locations,
 (SELECT COUNT(DISTINCT cs.disk_id) FROM observation co
 JOIN current_snapshot cs ON cs.id=co.snapshot_id WHERE co.content_id=c.content_id) AS disks`

func searchRank(value string) int {
	switch value {
	case "exact":
		return 0
	case "prefix":
		return 1
	case "substring":
		return 2
	case "typo":
		return 3
	default:
		return -1
	}
}

func (db *DB) Search(
	ctx context.Context, f base.SearchFilters, limit int, after base.SearchAnchor,
	expected *base.CatalogState,
) (base.SearchPage, error) {
	result := base.SearchPage{Filters: f, Items: []base.SearchItem{}}
	if err := ValidateSearchFilters(f); err != nil {
		return result, err
	}
	if limit < 1 || limit > 200 || after.ID < 0 ||
		(after.ID == 0 && (after.Relevance != "" || after.Path != "")) ||
		(after.ID > 0 && (searchRank(after.Relevance) < 0 ||
			(after.Relevance == "typo" && f.Match != "fuzzy"))) {
		return result, fmt.Errorf("%w: invalid search page", ErrValidation)
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
	if after.ID > 0 && after.Path == "" {
		// Imported paths can exceed the cursor size limit. Immutable observations
		// let the cursor carry only the ID and reconstruct the path within its revision.
		err := db.db.QueryRowContext(ctx, `SELECT o.path FROM observation o
 JOIN snapshot s ON s.id=o.snapshot_id WHERE o.id=? AND s.state='complete'`,
			after.ID).Scan(&after.Path)
		if err == sql.ErrNoRows {
			return result, fmt.Errorf("%w: invalid search cursor anchor", ErrValidation)
		}
		if err != nil {
			return result, err
		}
	}
	if f.DiskID > 0 {
		var exists bool
		if err := db.db.QueryRowContext(ctx, "SELECT EXISTS(SELECT 1 FROM disk WHERE id=?)",
			f.DiskID).Scan(&exists); err != nil {
			return result, err
		}
		if !exists {
			return result, ErrNotFound
		}
	}
	if f.SnapshotID > 0 {
		var disk base.DiskId
		err := db.db.QueryRowContext(ctx,
			"SELECT disk_id FROM snapshot WHERE id=? AND state='complete'", f.SnapshotID).Scan(&disk)
		if err == sql.ErrNoRows {
			return result, ErrNotFound
		}
		if err != nil {
			return result, err
		}
		if f.DiskID != 0 && f.DiskID != disk {
			return result, fmt.Errorf("%w: snapshot does not belong to disk", ErrValidation)
		}
	}

	query, args := searchCandidates(f)
	rank := searchRank(after.Relevance)
	if after.ID > 0 {
		var valid bool
		anchorArgs := append(append([]any{}, args...), after.ID, rank, after.Path)
		if err := db.db.QueryRowContext(ctx, query+
			`SELECT EXISTS(SELECT 1 FROM eligible WHERE id=? AND rank=? AND path=?)`,
			anchorArgs...).Scan(&valid); err != nil {
			return result, err
		}
		if !valid {
			return result, fmt.Errorf("%w: invalid search cursor anchor", ErrValidation)
		}
	}
	query, args = searchWindow(f, limit, after)
	query += `, page AS MATERIALIZED (
 SELECT * FROM eligible WHERE (rank,path,id)>(?,?,?) ORDER BY rank,path,id LIMIT ?
 ), page_contents AS MATERIALIZED (SELECT DISTINCT content_id FROM page),
 page_counts AS MATERIALIZED (SELECT c.content_id,` + searchReplicaCounts + `
 FROM page_contents c)
 SELECT p.id,p.content_id,p.snapshot_id,p.path,p.name,p.is_current,p.rank,
 o.mtime,c.size,c.hash_type,c.hash,
 s.disk_id,d.label,COALESCE(d.notes,''),COALESCE(d.serial,''),d.capacity,
 s.captured_at,s.imported_at,s.capture_provenance,COALESCE(s.input_path,''),
 COALESCE(s.input_format,''),s.file_count,s.content_count,pc.locations,pc.disks,
 (SELECT COUNT(*) FROM observation co JOIN snapshot ss ON ss.id=co.snapshot_id
 WHERE co.content_id=p.content_id AND ss.state='complete'),
 CASE WHEN ?='current' THEN pc.locations ELSE (SELECT COUNT(*) FROM (
 SELECT DISTINCT ss.disk_id,co.path FROM observation co
 JOIN snapshot ss ON ss.id=co.snapshot_id
 WHERE co.content_id=p.content_id AND ss.state='complete')) END,
 CASE WHEN ?='current' THEN pc.disks ELSE (SELECT COUNT(DISTINCT ss.disk_id)
 FROM observation co JOIN snapshot ss ON ss.id=co.snapshot_id
 WHERE co.content_id=p.content_id AND ss.state='complete') END
 FROM page p JOIN observation o ON o.id=p.id JOIN content c ON c.id=p.content_id
 JOIN page_counts pc ON pc.content_id=p.content_id
 JOIN snapshot s ON s.id=p.snapshot_id JOIN disk d ON d.id=s.disk_id
 ORDER BY p.rank,p.path,p.id`
	args = append(args, rank, after.Path, after.ID, limit+1, f.Scope, f.Scope)
	rows, err := db.db.QueryContext(ctx, query, args...)
	if err != nil {
		return result, err
	}
	defer rows.Close()
	for rows.Next() {
		var item base.SearchItem
		var mtime sql.NullString
		var size sql.NullInt64
		var algorithm, captured, imported string
		var capacity int64
		var relevance int
		o, s, d, c := &item.Observation, &item.Snapshot, &item.Disk, &item.Content
		if err := rows.Scan(&o.Id, &o.ContentId, &o.SnapshotId, &o.Path, &item.Basename,
			&item.IsCurrent, &relevance, &mtime, &size, &algorithm, &c.Content.Hash,
			&s.DiskId, &d.Label, &d.Notes, &d.Serial, &capacity,
			&captured, &imported, &s.CaptureProvenance, &s.InputPath, &s.InputFormat,
			&s.FileCount, &s.ContentCount, &c.CurrentLocationCount, &c.CurrentDiskCount,
			&c.ObservationCount, &c.LocationCount, &c.DiskCount); err != nil {
			return result, err
		}
		s.Id, d.Id, c.Content.Id, c.Scope = o.SnapshotId, s.DiskId, o.ContentId, f.Scope
		d.Capacity = uint64(capacity)
		item.Relevance = []string{"exact", "prefix", "substring", "typo"}[relevance]
		c.Content.HashType, err = base.FromIdentifier(algorithm)
		if err != nil {
			return result, err
		}
		if size.Valid {
			c.Content.Size = &size.Int64
		}
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
			o.MTime = &t
		}
		result.Items = append(result.Items, item)
	}
	return result, rows.Err()
}
