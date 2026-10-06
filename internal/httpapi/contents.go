package httpapi

import (
	"fmt"
	"net/http"
	"net/url"
	"strconv"

	"github.com/omgitsmoe/coldcat/internal/app"
	"github.com/omgitsmoe/coldcat/internal/base"
	"github.com/omgitsmoe/coldcat/internal/database"
)

type contentFiltersDTO struct {
	DiskID           *string            `json:"disk_id"`
	Directory        string             `json:"directory"`
	ReplicaMetric    base.ReplicaMetric `json:"replica_metric"`
	OtherReplicas    *string            `json:"other_replicas"`
	MinOtherReplicas *string            `json:"min_other_replicas"`
	MaxOtherReplicas *string            `json:"max_other_replicas"`
}

type contentPageDTO struct {
	Scope      base.Scope        `json:"scope"`
	Revision   string            `json:"revision"`
	Filters    contentFiltersDTO `json:"filters"`
	Items      []contentDTO      `json:"items"`
	NextCursor *string           `json:"next_cursor"`
}

func contentPageResponse(page base.ContentPage) (contentPageDTO, error) {
	f := page.Filters
	result := contentPageDTO{
		Scope: f.Scope, Revision: decimal(page.Catalog.Revision),
		Filters: contentFiltersDTO{
			Directory: f.Directory, ReplicaMetric: f.ReplicaMetric,
			OtherReplicas:    nullableSize(f.OtherReplicas),
			MinOtherReplicas: nullableSize(f.MinOtherReplicas),
			MaxOtherReplicas: nullableSize(f.MaxOtherReplicas),
		},
		Items: make([]contentDTO, 0, len(page.Items)),
	}
	if f.DiskID != 0 {
		value := decimal(f.DiskID)
		result.Filters.DiskID = &value
	}
	if page.NextCursor != "" {
		result.NextCursor = &page.NextCursor
	}
	for _, item := range page.Items {
		dto, err := contentResponse(item)
		if err != nil {
			return result, err
		}
		result.Items = append(result.Items, dto)
	}
	return result, nil
}

func replicaBound(values url.Values, name string) (*int64, error) {
	if !values.Has(name) {
		return nil, nil
	}
	n, err := strconv.ParseInt(values.Get(name), 10, 64)
	if err != nil || n < 0 {
		return nil, fmt.Errorf("%w: %s must be a nonnegative 64-bit integer",
			database.ErrValidation, name)
	}
	return &n, nil
}

func listContents(a *app.App, w http.ResponseWriter, r *http.Request) error {
	values, err := queryAllowEmpty(r, "directory",
		"scope", "disk_id", "directory", "replica_metric", "other_replicas",
		"min_other_replicas", "max_other_replicas", "limit", "cursor")
	if err != nil {
		return err
	}
	f := base.ContentFilters{Directory: values.Get("directory"), ReplicaMetric: base.ReplicaDisks}
	f.Scope, err = scope(values)
	if err != nil {
		return err
	}
	if values.Has("disk_id") {
		n, err := strconv.ParseInt(values.Get("disk_id"), 10, 64)
		if err != nil || n <= 0 {
			return fmt.Errorf("%w: disk_id must be a positive 64-bit integer", database.ErrValidation)
		}
		f.DiskID = base.DiskId(n)
	}
	if values.Has("directory") && f.DiskID == 0 {
		return fmt.Errorf("%w: directory requires disk_id", database.ErrValidation)
	}
	if values.Has("replica_metric") {
		f.ReplicaMetric = base.ReplicaMetric(values.Get("replica_metric"))
	}
	f.OtherReplicas, err = replicaBound(values, "other_replicas")
	if err != nil {
		return err
	}
	f.MinOtherReplicas, err = replicaBound(values, "min_other_replicas")
	if err != nil {
		return err
	}
	f.MaxOtherReplicas, err = replicaBound(values, "max_other_replicas")
	if err != nil {
		return err
	}
	limit := 50
	if values.Has("limit") {
		limit, err = strconv.Atoi(values.Get("limit"))
		if err != nil || limit < 1 || limit > 200 {
			return fmt.Errorf("%w: limit must be between 1 and 200", database.ErrValidation)
		}
	}
	page, err := a.ListContents(r.Context(), app.ListContentsRequest{
		Filters: f, Limit: limit, Cursor: values.Get("cursor"),
	})
	if err != nil {
		return err
	}
	dto, err := contentPageResponse(page)
	if err != nil {
		return err
	}
	writeJSON(w, http.StatusOK, dto)
	return nil
}
