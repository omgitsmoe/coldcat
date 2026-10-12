package httpapi

import (
	"fmt"
	"net/http"
	"strconv"

	"github.com/omgitsmoe/coldcat/internal/app"
	"github.com/omgitsmoe/coldcat/internal/base"
	"github.com/omgitsmoe/coldcat/internal/database"
)

type directorySearchFiltersDTO struct {
	Query      string     `json:"q"`
	Field      string     `json:"field"`
	Match      string     `json:"match"`
	Scope      base.Scope `json:"scope"`
	DiskID     *string    `json:"disk_id"`
	SnapshotID *string    `json:"snapshot_id"`
}

type directorySearchItemDTO struct {
	ID        string              `json:"id"`
	Directory directorySummaryDTO `json:"directory"`
	Snapshot  snapshotDTO         `json:"snapshot"`
	Disk      diskDTO             `json:"disk"`
	Basename  string              `json:"basename"`
	IsCurrent bool                `json:"is_current"`
}

type directorySearchPageDTO struct {
	Scope      base.Scope                `json:"scope"`
	Revision   string                    `json:"revision"`
	Filters    directorySearchFiltersDTO `json:"filters"`
	Items      []directorySearchItemDTO  `json:"items"`
	NextCursor *string                   `json:"next_cursor"`
}

func searchDirectories(a *app.App, w http.ResponseWriter, r *http.Request) error {
	values, err := query(r, "q", "field", "match", "scope", "disk_id", "snapshot_id",
		"limit", "cursor")
	if err != nil {
		return err
	}
	f := base.DirectorySearchFilters{
		Query: values.Get("q"), Field: values.Get("field"), Match: values.Get("match"),
	}
	f.Scope, err = scope(values)
	if err != nil {
		return err
	}
	for _, name := range []string{"disk_id", "snapshot_id"} {
		if !values.Has(name) {
			continue
		}
		n, err := strconv.ParseInt(values.Get(name), 10, 64)
		if err != nil || n <= 0 {
			return fmt.Errorf("%w: %s must be a positive 64-bit integer", database.ErrValidation, name)
		}
		if name == "disk_id" {
			f.DiskID = base.DiskId(n)
		} else {
			f.SnapshotID = base.SnapshotId(n)
		}
	}
	limit := 50
	if values.Has("limit") {
		limit, err = strconv.Atoi(values.Get("limit"))
		if err != nil || limit < 1 || limit > 200 {
			return fmt.Errorf("%w: limit must be between 1 and 200", database.ErrValidation)
		}
	}
	page, err := a.SearchDirectories(r.Context(), app.DirectorySearchRequest{
		Filters: f, Limit: limit, Cursor: values.Get("cursor"),
	})
	if err != nil {
		return err
	}
	f = page.Filters
	result := directorySearchPageDTO{
		Scope: f.Scope, Revision: decimal(page.Catalog.Revision),
		Filters: directorySearchFiltersDTO{
			Query: f.Query, Field: f.Field, Match: f.Match, Scope: f.Scope,
		},
		Items: make([]directorySearchItemDTO, 0, len(page.Items)),
	}
	if f.DiskID != 0 {
		value := decimal(f.DiskID)
		result.Filters.DiskID = &value
	}
	if f.SnapshotID != 0 {
		value := decimal(f.SnapshotID)
		result.Filters.SnapshotID = &value
	}
	if page.NextCursor != "" {
		result.NextCursor = &page.NextCursor
	}
	for _, item := range page.Items {
		result.Items = append(result.Items, directorySearchItemDTO{
			ID: decimal(item.ID), Directory: directorySummaryResponse(item.Directory),
			Snapshot: snapshotResponse(item.Snapshot), Disk: diskResponse(item.Disk),
			Basename: item.Basename, IsCurrent: item.IsCurrent,
		})
	}
	writeJSON(w, http.StatusOK, result)
	return nil
}
