package database

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"fmt"
	"strings"

	"github.com/omgitsmoe/coldcat/internal/base"
	"modernc.org/sqlite"
)

func init() {
	sqlite.MustRegisterDeterministicScalarFunction("coldcat_search_field_v1", 2,
		func(_ *sqlite.FunctionContext, args []driver.Value) (driver.Value, error) {
			text, textOK := args[0].(string)
			field, fieldOK := args[1].(string)
			if !textOK || !fieldOK || (field != "name" && field != "path") {
				return nil, fmt.Errorf("search field requires text and name/path")
			}
			if field == "name" {
				text = text[strings.LastIndexByte(text, '/')+1:]
			}
			return searchFold(text), nil
		})
}

func ValidateDirectorySearchFilters(f base.DirectorySearchFilters) error {
	if f.Match != "syntax" && f.Match != "whole" &&
		f.Match != "substring" && f.Match != "exact" {
		return fmt.Errorf("%w: invalid directory search match", ErrValidation)
	}
	return ValidateSearchFilters(base.SearchFilters{
		Query: f.Query, Field: f.Field, Match: f.Match, SnapshotID: f.SnapshotID,
		ContentFilters: base.ContentFilters{
			Scope: f.Scope, DiskID: f.DiskID, ReplicaMetric: base.ReplicaDisks,
		},
	})
}

func directorySearchPredicate(f base.DirectorySearchFilters) (string, []any, error) {
	column := "coldcat_search_field_v1(p.path,'" + f.Field + "')"
	if f.Match == "exact" {
		return column + "=?", []any{searchFold(f.Query)}, nil
	}
	if f.Match == "substring" {
		return "instr(" + column + ",?)>0", []any{searchFold(f.Query)}, nil
	}
	if strings.TrimSpace(f.Query) == "*" {
		return "1", nil, nil
	}
	e, err := parseSearchSyntax(f.Query, f.Match == "whole")
	if err != nil {
		return "", nil, err
	}
	_, predicate, args := e.compile(f.Field)
	return strings.ReplaceAll(predicate, "p."+f.Field+"_fold", column), args, nil
}

func (db *DB) SearchDirectories(
	ctx context.Context, f base.DirectorySearchFilters, limit int, afterID int64,
	expected *base.CatalogState,
) (base.DirectorySearchPage, error) {
	result := base.DirectorySearchPage{Filters: f, Items: []base.DirectorySearchItem{}}
	if err := ValidateDirectorySearchFilters(f); err != nil {
		return result, err
	}
	if limit < 1 || limit > 200 || afterID < 0 || (expected != nil) != (afterID > 0) {
		return result, fmt.Errorf("%w: invalid directory search page", ErrValidation)
	}
	predicate, predicateArgs, err := directorySearchPredicate(f)
	if err != nil {
		return result, err
	}
	release, err := db.readAccess()
	if err != nil {
		return result, err
	}
	defer release()

	err = db.TransactionContext(ctx, func(tx *Tx) error {
		if err := tx.QueryRowContext(ctx, `SELECT COALESCE(MAX(id),0) FROM snapshot
 WHERE state='complete'`).Scan(&result.Catalog.Revision); err != nil {
			return err
		}
		if expected != nil && *expected != result.Catalog {
			return ErrStaleCursor
		}
		if f.DiskID > 0 {
			if _, err := scanDisk(tx.QueryRowContext(ctx,
				"SELECT "+diskColumns+" FROM disk WHERE id=?", f.DiskID)); err != nil {
				return err
			}
		}
		if f.SnapshotID > 0 {
			s, err := scanSnapshot(tx.QueryRowContext(ctx,
				"SELECT "+snapshotColumns+" FROM snapshot WHERE id=? AND state='complete'",
				f.SnapshotID))
			if err != nil {
				return err
			}
			if f.DiskID != 0 && s.DiskId != f.DiskID {
				return fmt.Errorf("%w: snapshot does not belong to disk", ErrValidation)
			}
		}
		query := currentSnapshots + `, eligible AS (
 SELECT p.id,p.path,p.snapshot_id,cs.id IS NOT NULL AS is_current
 FROM directory p JOIN snapshot s ON s.id=p.snapshot_id
 LEFT JOIN current_snapshot cs ON cs.id=s.id
 WHERE s.state='complete' AND (?='history' OR cs.id IS NOT NULL)
 AND (?=0 OR s.disk_id=?) AND (?=0 OR s.id=?) AND ` + predicate + `) `
		args := []any{f.Scope, f.DiskID, f.DiskID, f.SnapshotID, f.SnapshotID}
		args = append(args, predicateArgs...)
		var afterPath string
		if afterID > 0 {
			err := tx.QueryRowContext(ctx, query+"SELECT path FROM eligible WHERE id=?",
				append(append([]any{}, args...), afterID)...).Scan(&afterPath)
			if err == sql.ErrNoRows {
				return fmt.Errorf("%w: invalid directory search anchor", ErrValidation)
			}
			if err != nil {
				return err
			}
		}
		rows, err := tx.QueryContext(ctx, query+`SELECT id,path,snapshot_id,is_current
 FROM eligible WHERE (path,id)>(?,?) ORDER BY path,id LIMIT ?`,
			append(args, afterPath, afterID, limit+1)...)
		if err != nil {
			return err
		}
		for rows.Next() {
			var item base.DirectorySearchItem
			if err := rows.Scan(&item.ID, &item.Directory.Path, &item.Snapshot.Id,
				&item.IsCurrent); err != nil {
				rows.Close()
				return err
			}
			result.Items = append(result.Items, item)
		}
		err = rows.Err()
		if closeErr := rows.Close(); err == nil {
			err = closeErr
		}
		if err != nil {
			return err
		}
		snapshots := map[base.SnapshotId]base.Snapshot{}
		disks := map[base.DiskId]base.Disk{}
		for i := range result.Items {
			item := &result.Items[i]
			item.Basename = item.Directory.Path[strings.LastIndexByte(item.Directory.Path, '/')+1:]
			item.Directory, err = scanDirectory(tx.QueryRowContext(ctx,
				"SELECT "+directoryColumns+" FROM directory WHERE id=?", item.ID))
			if err != nil {
				return err
			}
			s, ok := snapshots[item.Snapshot.Id]
			if !ok {
				s, err = scanSnapshot(tx.QueryRowContext(ctx,
					"SELECT "+snapshotColumns+" FROM snapshot WHERE id=?", item.Snapshot.Id))
				if err != nil {
					return err
				}
				snapshots[s.Id] = s
			}
			item.Snapshot = s
			d, ok := disks[s.DiskId]
			if !ok {
				d, err = scanDisk(tx.QueryRowContext(ctx,
					"SELECT "+diskColumns+" FROM disk WHERE id=?", s.DiskId))
				if err != nil {
					return err
				}
				disks[d.Id] = d
			}
			item.Disk = d
		}
		return nil
	})
	return result, err
}
