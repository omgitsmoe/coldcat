package httpapi

import (
	"fmt"
	"net/http"
	"strconv"

	"github.com/omgitsmoe/coldcat/internal/app"
	"github.com/omgitsmoe/coldcat/internal/base"
	"github.com/omgitsmoe/coldcat/internal/database"
)

type searchItemDTO struct {
	Observation observationDTO `json:"observation"`
	Content     contentDTO     `json:"content"`
	Disk        diskDTO        `json:"disk"`
	Snapshot    snapshotDTO    `json:"snapshot"`
	Basename    string         `json:"basename"`
	IsCurrent   bool           `json:"is_current"`
	Relevance   string         `json:"relevance"`
}

type searchFiltersDTO struct {
	Query      string  `json:"q"`
	Field      string  `json:"field"`
	Match      string  `json:"match"`
	SnapshotID *string `json:"snapshot_id"`
	contentFiltersDTO
}

type searchPageDTO struct {
	Scope      base.Scope       `json:"scope"`
	Revision   string           `json:"revision"`
	Filters    searchFiltersDTO `json:"filters"`
	Items      []searchItemDTO  `json:"items"`
	NextCursor *string          `json:"next_cursor"`
}

func searchPageResponse(page base.SearchPage) (searchPageDTO, error) {
	f := page.Filters
	result := searchPageDTO{
		Scope: f.Scope, Revision: decimal(page.Catalog.Revision),
		Filters: searchFiltersDTO{
			Query: f.Query, Field: f.Field, Match: f.Match,
			contentFiltersDTO: contentFiltersDTO{
				Directory: f.Directory, ReplicaMetric: f.ReplicaMetric,
				OtherReplicas:    nullableSize(f.OtherReplicas),
				MinOtherReplicas: nullableSize(f.MinOtherReplicas),
				MaxOtherReplicas: nullableSize(f.MaxOtherReplicas),
			},
		},
		Items: make([]searchItemDTO, 0, len(page.Items)),
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
		content, err := contentResponse(item.Content)
		if err != nil {
			return result, err
		}
		result.Items = append(result.Items, searchItemDTO{
			Observation: observationResponse(item.Observation), Content: content,
			Disk: diskResponse(item.Disk), Snapshot: snapshotResponse(item.Snapshot),
			Basename: item.Basename, IsCurrent: item.IsCurrent, Relevance: item.Relevance,
		})
	}
	return result, nil
}

func search(a *app.App, w http.ResponseWriter, r *http.Request) error {
	values, err := queryAllowEmpty(r, "directory", "q", "field", "match", "scope", "disk_id",
		"snapshot_id", "directory", "replica_metric", "other_replicas",
		"min_other_replicas", "max_other_replicas", "limit", "cursor")
	if err != nil {
		return err
	}
	f := base.SearchFilters{Query: values.Get("q"), Field: values.Get("field"),
		Match: values.Get("match")}
	f.Directory = values.Get("directory")
	f.ReplicaMetric = base.ReplicaMetric(values.Get("replica_metric"))
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
	if values.Has("directory") && f.DiskID == 0 && f.SnapshotID == 0 {
		return fmt.Errorf("%w: directory requires disk_id or snapshot_id", database.ErrValidation)
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
	page, err := a.Search(r.Context(), app.SearchRequest{
		Filters: f, Limit: limit, Cursor: values.Get("cursor"),
	})
	if err != nil {
		return err
	}
	dto, err := searchPageResponse(page)
	if err != nil {
		return err
	}
	writeJSON(w, http.StatusOK, dto)
	return nil
}
