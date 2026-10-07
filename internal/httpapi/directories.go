package httpapi

import (
	"encoding/hex"
	"fmt"
	"net/http"
	"net/url"
	"strconv"

	"github.com/omgitsmoe/coldcat/internal/app"
	"github.com/omgitsmoe/coldcat/internal/base"
	"github.com/omgitsmoe/coldcat/internal/database"
)

type directorySummaryDTO struct {
	Path                      string  `json:"path"`
	FileCount                 string  `json:"file_count"`
	ContentCount              string  `json:"content_count"`
	KnownBytes                string  `json:"known_bytes"`
	UnknownSizeFileCount      string  `json:"unknown_size_file_count"`
	SizeComplete              bool    `json:"size_complete"`
	UniqueContentKnownBytes   string  `json:"unique_content_known_bytes"`
	UnknownSizeContentCount   string  `json:"unknown_size_content_count"`
	UniqueContentSizeComplete bool    `json:"unique_content_size_complete"`
	MaxKnownMTime             *string `json:"max_known_mtime"`
}

func directorySummaryResponse(value base.DirectorySummary) directorySummaryDTO {
	return directorySummaryDTO{
		Path: value.Path, FileCount: decimal(value.FileCount), ContentCount: decimal(value.ContentCount),
		KnownBytes: decimal(value.KnownBytes), UnknownSizeFileCount: decimal(value.UnknownSizeFileCount),
		SizeComplete:              value.UnknownSizeFileCount == 0,
		UniqueContentKnownBytes:   decimal(value.UniqueContentKnownBytes),
		UnknownSizeContentCount:   decimal(value.UnknownSizeContentCount),
		UniqueContentSizeComplete: value.UnknownSizeContentCount == 0,
		MaxKnownMTime:             nullableTime(value.MaxKnownMTime),
	}
}

type directoryContextDTO struct {
	Snapshot     snapshotDTO `json:"snapshot"`
	Revision     string      `json:"revision"`
	IsCurrent    bool        `json:"is_current"`
	ReplicaScope base.Scope  `json:"replica_scope"`
}

func directoryContextResponse(value base.DirectoryContext) directoryContextDTO {
	return directoryContextDTO{
		Snapshot: snapshotResponse(value.Snapshot), Revision: decimal(value.Catalog.Revision),
		IsCurrent: value.IsCurrent, ReplicaScope: base.ScopeCurrent,
	}
}

type redundancyBucketDTO struct {
	OtherDiskCount string `json:"other_disk_count"`
	FileCount      string `json:"file_count"`
}

type directoryDetailDTO struct {
	directoryContextDTO
	Directory           directorySummaryDTO   `json:"directory"`
	RedundancyHistogram []redundancyBucketDTO `json:"redundancy_histogram"`
}

type directoryFileDTO struct {
	Observation        observationDTO `json:"observation"`
	Hash               hashDTO        `json:"hash"`
	Size               *string        `json:"size"`
	OtherLocationCount string         `json:"other_location_count"`
	OtherDiskCount     string         `json:"other_disk_count"`
}

type directoryEntryDTO struct {
	Path      string               `json:"path"`
	Kind      string               `json:"kind"`
	Directory *directorySummaryDTO `json:"directory"`
	File      *directoryFileDTO    `json:"file"`
}

type directoryFiltersDTO struct {
	Path             string             `json:"path"`
	DirectoriesOnly  bool               `json:"directories_only"`
	Recursive        bool               `json:"recursive"`
	ReplicaMetric    base.ReplicaMetric `json:"replica_metric"`
	OtherReplicas    *string            `json:"other_replicas"`
	MinOtherReplicas *string            `json:"min_other_replicas"`
	MaxOtherReplicas *string            `json:"max_other_replicas"`
}

type directoryPageDTO struct {
	directoryContextDTO
	Filters    directoryFiltersDTO `json:"filters"`
	Items      []directoryEntryDTO `json:"items"`
	NextCursor *string             `json:"next_cursor"`
}

func directoryPageResponse(page base.DirectoryPage) (directoryPageDTO, error) {
	f := page.Filters
	result := directoryPageDTO{
		directoryContextDTO: directoryContextResponse(page.DirectoryContext),
		Filters: directoryFiltersDTO{
			Path: f.Path, DirectoriesOnly: f.DirectoriesOnly, Recursive: f.Recursive,
			ReplicaMetric: f.ReplicaMetric, OtherReplicas: nullableSize(f.OtherReplicas),
			MinOtherReplicas: nullableSize(f.MinOtherReplicas),
			MaxOtherReplicas: nullableSize(f.MaxOtherReplicas),
		},
		Items: make([]directoryEntryDTO, 0, len(page.Items)),
	}
	if page.NextCursor != "" {
		result.NextCursor = &page.NextCursor
	}
	for _, item := range page.Items {
		entry := directoryEntryDTO{Path: item.Path, Kind: item.Kind}
		if item.Directory != nil {
			summary := directorySummaryResponse(*item.Directory)
			entry.Directory = &summary
		} else {
			algorithm, err := item.Content.HashType.ToIdentifier()
			if err != nil {
				return result, err
			}
			entry.File = &directoryFileDTO{
				Observation:        observationResponse(*item.Observation),
				Hash:               hashDTO{Algorithm: algorithm, Hex: hex.EncodeToString(item.Content.Hash)},
				Size:               nullableSize(item.Content.Size),
				OtherLocationCount: decimal(item.OtherLocationCount),
				OtherDiskCount:     decimal(item.OtherDiskCount),
			}
		}
		result.Items = append(result.Items, entry)
	}
	return result, nil
}

func getDirectory(a *app.App, w http.ResponseWriter, r *http.Request) error {
	id, err := resourceID(r)
	if err != nil {
		return err
	}
	values, err := queryAllowEmpty(r, "path", "path")
	if err != nil {
		return err
	}
	detail, err := a.GetDirectory(r.Context(), app.GetDirectoryRequest{
		SnapshotID: base.SnapshotId(id), Path: values.Get("path"),
	})
	if err != nil {
		return err
	}
	result := directoryDetailDTO{
		directoryContextDTO: directoryContextResponse(detail.DirectoryContext),
		Directory:           directorySummaryResponse(detail.DirectorySummary),
		RedundancyHistogram: make([]redundancyBucketDTO, 0, len(detail.RedundancyHistogram)),
	}
	for _, bucket := range detail.RedundancyHistogram {
		result.RedundancyHistogram = append(result.RedundancyHistogram, redundancyBucketDTO{
			OtherDiskCount: decimal(bucket.OtherDiskCount), FileCount: decimal(bucket.FileCount),
		})
	}
	writeJSON(w, http.StatusOK, result)
	return nil
}

func listDirectoryEntries(
	a *app.App, w http.ResponseWriter, r *http.Request, directoriesOnly bool,
) error {
	id, err := resourceID(r)
	if err != nil {
		return err
	}
	pathParameter := "path"
	allowed := []string{"path", "recursive", "replica_metric", "other_replicas",
		"min_other_replicas", "max_other_replicas", "limit", "cursor"}
	if directoriesOnly {
		pathParameter = "parent"
		allowed = []string{"parent", "limit", "cursor"}
	}
	values, err := queryAllowEmpty(r, pathParameter, allowed...)
	if err != nil {
		return err
	}
	f := base.DirectoryFilters{
		SnapshotID: base.SnapshotId(id), Path: values.Get(pathParameter),
		DirectoriesOnly: directoriesOnly, ReplicaMetric: base.ReplicaDisks,
	}
	if values.Has("recursive") {
		if values.Get("recursive") != "true" && values.Get("recursive") != "false" {
			return fmt.Errorf("%w: recursive must be true or false", database.ErrValidation)
		}
		f.Recursive = values.Get("recursive") == "true"
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
	page, err := a.ListDirectoryEntries(r.Context(), app.ListDirectoryEntriesRequest{
		Filters: f, Limit: limit, Cursor: values.Get("cursor"),
	})
	if err != nil {
		return err
	}
	dto, err := directoryPageResponse(page)
	if err != nil {
		return err
	}
	writeJSON(w, http.StatusOK, dto)
	return nil
}

type directoryComparisonFiltersDTO struct {
	Path  string   `json:"path"`
	Allow []string `json:"allow"`
	Block []string `json:"block"`
}

type directorySelectionDTO struct {
	RetainedFileCount    string `json:"retained_file_count"`
	ExcludedFileCount    string `json:"excluded_file_count"`
	ContentCount         string `json:"content_count"`
	KnownBytes           string `json:"known_bytes"`
	UnknownSizeFileCount string `json:"unknown_size_file_count"`
	SizeComplete         bool   `json:"size_complete"`
	EmptyComparison      bool   `json:"empty_comparison"`
}

func directoryComparisonFiltersResponse(
	filters base.DirectoryComparisonFilters,
) directoryComparisonFiltersDTO {
	return directoryComparisonFiltersDTO{
		Path:  filters.Path,
		Allow: append([]string{}, filters.Allow...),
		Block: append([]string{}, filters.Block...),
	}
}

func directorySelectionResponse(selection base.DirectorySelection) directorySelectionDTO {
	return directorySelectionDTO{
		RetainedFileCount:    decimal(selection.RetainedFileCount),
		ExcludedFileCount:    decimal(selection.ExcludedFileCount),
		ContentCount:         decimal(selection.ContentCount),
		KnownBytes:           decimal(selection.KnownBytes),
		UnknownSizeFileCount: decimal(selection.UnknownSizeFileCount),
		SizeComplete:         selection.UnknownSizeFileCount == 0,
		EmptyComparison:      selection.EmptyComparison,
	}
}

type directoryReplicaDTO struct {
	Disk              diskDTO     `json:"disk"`
	Snapshot          snapshotDTO `json:"snapshot"`
	Path              string      `json:"path"`
	SameDisk          bool        `json:"same_disk"`
	WholeTreeEqual    bool        `json:"whole_tree_equal"`
	RetainedFileCount string      `json:"retained_file_count"`
	ExcludedFileCount string      `json:"excluded_file_count"`
}

type directoryReplicaPageDTO struct {
	directoryContextDTO
	Filters    directoryComparisonFiltersDTO `json:"filters"`
	Selection  directorySelectionDTO         `json:"selection"`
	Items      []directoryReplicaDTO         `json:"items"`
	NextCursor *string                       `json:"next_cursor"`
}

func directoryReplicaPageResponse(page base.DirectoryReplicaPage) directoryReplicaPageDTO {
	result := directoryReplicaPageDTO{
		directoryContextDTO: directoryContextResponse(page.DirectoryContext),
		Filters:             directoryComparisonFiltersResponse(page.Filters),
		Selection:           directorySelectionResponse(page.Selection),
		Items:               make([]directoryReplicaDTO, 0, len(page.Items)),
	}

	if page.NextCursor != "" {
		result.NextCursor = &page.NextCursor
	}

	for _, item := range page.Items {
		result.Items = append(result.Items, directoryReplicaDTO{
			Disk:              diskResponse(item.Disk),
			Snapshot:          snapshotResponse(item.Snapshot),
			Path:              item.Path,
			SameDisk:          item.SameDisk,
			WholeTreeEqual:    item.WholeTreeEqual,
			RetainedFileCount: decimal(item.RetainedFileCount),
			ExcludedFileCount: decimal(item.ExcludedFileCount),
		})
	}

	return result
}

type directoryCoverageDTO struct {
	Disk                    diskDTO     `json:"disk"`
	Snapshot                snapshotDTO `json:"snapshot"`
	CoveredFileCount        string      `json:"covered_file_count"`
	MissingFileCount        string      `json:"missing_file_count"`
	CoveredContentCount     string      `json:"covered_content_count"`
	MissingContentCount     string      `json:"missing_content_count"`
	CoveredKnownBytes       string      `json:"covered_known_bytes"`
	MissingKnownBytes       string      `json:"missing_known_bytes"`
	CoveredUnknownSizeFiles string      `json:"covered_unknown_size_file_count"`
	MissingUnknownSizeFiles string      `json:"missing_unknown_size_file_count"`
	Complete                bool        `json:"complete"`
}

type directoryCoveragePageDTO struct {
	directoryContextDTO
	Filters    directoryComparisonFiltersDTO `json:"filters"`
	Selection  directorySelectionDTO         `json:"selection"`
	Items      []directoryCoverageDTO        `json:"items"`
	NextCursor *string                       `json:"next_cursor"`
}

func directoryCoveragePageResponse(page base.DirectoryCoveragePage) directoryCoveragePageDTO {
	result := directoryCoveragePageDTO{
		directoryContextDTO: directoryContextResponse(page.DirectoryContext),
		Filters:             directoryComparisonFiltersResponse(page.Filters),
		Selection:           directorySelectionResponse(page.Selection),
		Items:               make([]directoryCoverageDTO, 0, len(page.Items)),
	}

	if page.NextCursor != "" {
		result.NextCursor = &page.NextCursor
	}

	for _, item := range page.Items {
		result.Items = append(result.Items, directoryCoverageDTO{
			Disk:                    diskResponse(item.Disk),
			Snapshot:                snapshotResponse(item.Snapshot),
			CoveredFileCount:        decimal(item.CoveredFileCount),
			MissingFileCount:        decimal(item.MissingFileCount),
			CoveredContentCount:     decimal(item.CoveredContentCount),
			MissingContentCount:     decimal(item.MissingContentCount),
			CoveredKnownBytes:       decimal(item.CoveredKnownBytes),
			MissingKnownBytes:       decimal(item.MissingKnownBytes),
			CoveredUnknownSizeFiles: decimal(item.CoveredUnknownSizeFiles),
			MissingUnknownSizeFiles: decimal(item.MissingUnknownSizeFiles),
			Complete:                item.Complete,
		})
	}

	return result
}

func directoryComparisonQuery(r *http.Request) (url.Values, error) {
	values, err := url.ParseQuery(r.URL.RawQuery)
	if err != nil {
		return nil, fmt.Errorf("%w: malformed query", database.ErrValidation)
	}

	for key, items := range values {
		switch key {
		case "allow", "block":
			for _, item := range items {
				if item == "" {
					return nil, fmt.Errorf("%w: empty parameter %q", database.ErrValidation, key)
				}
			}
		case "path":
			if len(items) != 1 {
				return nil, fmt.Errorf("%w: repeated parameter %q", database.ErrValidation, key)
			}
		case "limit", "cursor":
			if len(items) != 1 || items[0] == "" {
				return nil, fmt.Errorf(
					"%w: repeated or empty parameter %q",
					database.ErrValidation,
					key,
				)
			}
		default:
			return nil, fmt.Errorf("%w: unknown parameter %q", database.ErrValidation, key)
		}
	}

	return values, nil
}

func directoryComparisonRequest(
	r *http.Request,
) (app.ListDirectoryComparisonsRequest, error) {
	var result app.ListDirectoryComparisonsRequest

	id, err := resourceID(r)
	if err != nil {
		return result, err
	}

	values, err := directoryComparisonQuery(r)
	if err != nil {
		return result, err
	}

	limit := 50
	if values.Has("limit") {
		limit, err = strconv.Atoi(values.Get("limit"))
		if err != nil || limit < 1 || limit > 200 {
			return result, fmt.Errorf(
				"%w: limit must be between 1 and 200",
				database.ErrValidation,
			)
		}
	}

	result = app.ListDirectoryComparisonsRequest{
		Filters: base.DirectoryComparisonFilters{
			SnapshotID: base.SnapshotId(id),
			Path:       values.Get("path"),
			Allow:      values["allow"],
			Block:      values["block"],
		},
		Limit:  limit,
		Cursor: values.Get("cursor"),
	}

	return result, nil
}

func listDirectoryReplicas(a *app.App, w http.ResponseWriter, r *http.Request) error {
	req, err := directoryComparisonRequest(r)
	if err != nil {
		return err
	}

	page, err := a.ListDirectoryReplicas(r.Context(), req)
	if err != nil {
		return err
	}

	writeJSON(w, http.StatusOK, directoryReplicaPageResponse(page))
	return nil
}

func listDirectoryCoverage(a *app.App, w http.ResponseWriter, r *http.Request) error {
	req, err := directoryComparisonRequest(r)
	if err != nil {
		return err
	}

	page, err := a.ListDirectoryCoverage(r.Context(), req)
	if err != nil {
		return err
	}

	writeJSON(w, http.StatusOK, directoryCoveragePageResponse(page))
	return nil
}
