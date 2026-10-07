package httpapi

import (
	"net/http"

	"github.com/omgitsmoe/coldcat/internal/app"
	"github.com/omgitsmoe/coldcat/internal/base"
)

type catalogDTO struct {
	Revision     string     `json:"revision"`
	Scope        base.Scope `json:"scope"`
	DiskCount    string     `json:"disk_count"`
	FileCount    string     `json:"file_count"`
	ContentCount string     `json:"content_count"`
}

func catalogResponse(value base.CatalogSummary) catalogDTO {
	return catalogDTO{
		Revision: decimal(value.Catalog.Revision), Scope: base.ScopeCurrent,
		DiskCount: decimal(value.DiskCount), FileCount: decimal(value.FileCount),
		ContentCount: decimal(value.ContentCount),
	}
}

func getCatalog(a *app.App, w http.ResponseWriter, r *http.Request) error {
	if _, err := query(r); err != nil {
		return err
	}

	result, err := a.GetCatalogSummary(r.Context())
	if err != nil {
		return err
	}

	writeJSON(w, http.StatusOK, catalogResponse(result))
	return nil
}
