package database

import (
	"context"

	"github.com/omgitsmoe/coldcat/internal/base"
)

const catalogSummaryQuery = currentSnapshots + `SELECT
	(SELECT COALESCE(MAX(id),0) FROM snapshot WHERE state='complete'),
	(SELECT COUNT(*) FROM disk),
	COUNT(*), COUNT(DISTINCT o.content_id)
	FROM observation o JOIN current_snapshot cs ON cs.id=o.snapshot_id`

func (db *DB) GetCatalogSummary(ctx context.Context) (base.CatalogSummary, error) {
	release, err := db.readAccess()
	if err != nil {
		return base.CatalogSummary{}, err
	}
	defer release()

	var result base.CatalogSummary
	err = db.db.QueryRowContext(ctx, catalogSummaryQuery).Scan(
		&result.Catalog.Revision, &result.DiskCount, &result.FileCount, &result.ContentCount,
	)
	if err != nil {
		return base.CatalogSummary{}, err
	}

	return result, nil
}
