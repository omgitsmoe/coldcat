package app

import (
	"context"

	"github.com/omgitsmoe/coldcat/internal/base"
)

func (a *App) GetCatalogSummary(ctx context.Context) (base.CatalogSummary, error) {
	return a.db.GetCatalogSummary(ctx)
}
