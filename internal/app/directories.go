package app

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"

	"github.com/omgitsmoe/coldcat/internal/base"
	"github.com/omgitsmoe/coldcat/internal/database"
)

type GetDirectoryRequest struct {
	SnapshotID base.SnapshotId
	Path       string
}

type ListDirectoryEntriesRequest struct {
	Filters base.DirectoryFilters
	Limit   int
	Cursor  string
}

type ListDirectoryComparisonsRequest struct {
	Filters base.DirectoryComparisonFilters
	Limit   int
	Cursor  string
}

type directoryCursor struct {
	Version int                  `json:"version"`
	Kind    string               `json:"kind"`
	Catalog base.CatalogState    `json:"catalog"`
	Filters string               `json:"filters"`
	Limit   int                  `json:"limit"`
	After   base.DirectoryAnchor `json:"after"`
}

func (a *App) GetDirectory(
	ctx context.Context, req GetDirectoryRequest,
) (base.DirectoryDetail, error) {
	return a.db.GetDirectory(ctx, req.SnapshotID, req.Path)
}

func (a *App) ListDirectories(
	ctx context.Context, req ListDirectoryEntriesRequest,
) (base.DirectoryPage, error) {
	req.Filters.DirectoriesOnly = true
	return a.ListDirectoryEntries(ctx, req)
}

func (a *App) ListDirectoryEntries(
	ctx context.Context, req ListDirectoryEntriesRequest,
) (base.DirectoryPage, error) {
	if req.Filters.ReplicaMetric == "" {
		req.Filters.ReplicaMetric = base.ReplicaDisks
	}
	if req.Limit == 0 {
		req.Limit = 50
	}
	if err := database.ValidateDirectoryFilters(req.Filters); err != nil {
		return base.DirectoryPage{}, err
	}
	filterData, err := json.Marshal(req.Filters)
	if err != nil {
		return base.DirectoryPage{}, err
	}
	digest := sha256.Sum256(filterData)
	filterDigest := hex.EncodeToString(digest[:])
	var cursor directoryCursor
	var expected *base.CatalogState
	if req.Cursor != "" {
		invalid := func() (base.DirectoryPage, error) {
			return base.DirectoryPage{}, fmt.Errorf("%w: invalid directory cursor", database.ErrValidation)
		}
		if len(req.Cursor) > 16384 {
			return invalid()
		}
		data, err := base64.RawURLEncoding.Strict().DecodeString(req.Cursor)
		if err != nil {
			return invalid()
		}
		decoder := json.NewDecoder(bytes.NewReader(data))
		decoder.DisallowUnknownFields()
		if err := decoder.Decode(&cursor); err != nil {
			return invalid()
		}
		if err := decoder.Decode(new(any)); err != io.EOF {
			return invalid()
		}
		if cursor.Version != 1 || cursor.Kind != "directory:path:kind:asc" ||
			cursor.Catalog.Revision < 0 || cursor.Limit != req.Limit ||
			cursor.Filters != filterDigest {
			return invalid()
		}
		expected = &cursor.Catalog
	}
	page, err := a.db.ListDirectoryEntries(ctx, req.Filters, req.Limit, cursor.After, expected)
	if err != nil {
		return page, err
	}
	if len(page.Items) > req.Limit {
		page.Items = page.Items[:req.Limit]
		last := page.Items[len(page.Items)-1]
		data, err := json.Marshal(directoryCursor{
			Version: 1, Kind: "directory:path:kind:asc", Catalog: page.Catalog,
			Filters: filterDigest, Limit: req.Limit,
			After: base.DirectoryAnchor{ID: last.ID, Kind: last.Kind},
		})
		if err != nil {
			return page, err
		}
		page.NextCursor = base64.RawURLEncoding.EncodeToString(data)
	}
	return page, nil
}

type directoryComparisonCursor struct {
	Version       int                         `json:"version"`
	Kind          string                      `json:"kind"`
	Catalog       base.CatalogState           `json:"catalog"`
	Filters       string                      `json:"filters"`
	Limit         int                         `json:"limit"`
	ReplicaAfter  base.DirectoryReplicaAnchor `json:"replica_after,omitempty"`
	CoverageAfter base.DiskId                 `json:"coverage_after,omitempty"`
}

func normalizeComparisonRequest(
	req ListDirectoryComparisonsRequest,
) (ListDirectoryComparisonsRequest, string, error) {
	filters, err := database.NormalizeDirectoryComparisonFilters(req.Filters)
	if err != nil {
		return req, "", err
	}
	req.Filters = filters

	if req.Limit == 0 {
		req.Limit = 50
	}

	data, err := json.Marshal(filters)
	if err != nil {
		return req, "", err
	}

	digest := sha256.Sum256(data)
	return req, hex.EncodeToString(digest[:]), nil
}

func decodeDirectoryComparisonCursor(
	encoded, kind, filterDigest string, limit int,
) (directoryComparisonCursor, error) {
	invalid := func() (directoryComparisonCursor, error) {
		return directoryComparisonCursor{}, fmt.Errorf(
			"%w: invalid directory comparison cursor",
			database.ErrValidation,
		)
	}

	if encoded == "" {
		return directoryComparisonCursor{}, nil
	}
	if len(encoded) > 16384 {
		return invalid()
	}

	data, err := base64.RawURLEncoding.Strict().DecodeString(encoded)
	if err != nil {
		return invalid()
	}

	var cursor directoryComparisonCursor
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&cursor); err != nil {
		return invalid()
	}
	if err := decoder.Decode(new(any)); err != io.EOF {
		return invalid()
	}

	if cursor.Version != 1 || cursor.Kind != kind || cursor.Catalog.Revision < 0 ||
		cursor.Limit != limit || cursor.Filters != filterDigest {
		return invalid()
	}

	return cursor, nil
}

func encodeDirectoryComparisonCursor(cursor directoryComparisonCursor) (string, error) {
	data, err := json.Marshal(cursor)
	if err != nil {
		return "", err
	}

	return base64.RawURLEncoding.EncodeToString(data), nil
}

func (a *App) ListDirectoryReplicas(
	ctx context.Context, req ListDirectoryComparisonsRequest,
) (base.DirectoryReplicaPage, error) {
	req, filterDigest, err := normalizeComparisonRequest(req)
	if err != nil {
		return base.DirectoryReplicaPage{}, err
	}

	const kind = "directory-replicas:disk:path:id:asc"
	cursor, err := decodeDirectoryComparisonCursor(req.Cursor, kind, filterDigest, req.Limit)
	if err != nil {
		return base.DirectoryReplicaPage{}, err
	}

	var expected *base.CatalogState
	if req.Cursor != "" {
		if cursor.ReplicaAfter.DiskID <= 0 || cursor.ReplicaAfter.DirectoryID <= 0 ||
			cursor.CoverageAfter != 0 {
			return base.DirectoryReplicaPage{}, fmt.Errorf(
				"%w: invalid directory comparison cursor",
				database.ErrValidation,
			)
		}

		expected = &cursor.Catalog
	}

	page, err := a.db.ListDirectoryReplicas(
		ctx,
		req.Filters,
		req.Limit,
		cursor.ReplicaAfter,
		expected,
	)
	if err != nil {
		return page, err
	}

	if len(page.Items) > req.Limit {
		page.Items = page.Items[:req.Limit]
		last := page.Items[len(page.Items)-1]

		page.NextCursor, err = encodeDirectoryComparisonCursor(directoryComparisonCursor{
			Version: 1,
			Kind:    kind,
			Catalog: page.Catalog,
			Filters: filterDigest,
			Limit:   req.Limit,
			ReplicaAfter: base.DirectoryReplicaAnchor{
				DiskID:      last.Disk.Id,
				Path:        last.Path,
				DirectoryID: last.DirectoryID,
			},
		})
		if err != nil {
			return page, err
		}
	}

	return page, nil
}

func (a *App) ListDirectoryCoverage(
	ctx context.Context, req ListDirectoryComparisonsRequest,
) (base.DirectoryCoveragePage, error) {
	req, filterDigest, err := normalizeComparisonRequest(req)
	if err != nil {
		return base.DirectoryCoveragePage{}, err
	}

	const kind = "directory-coverage:disk:asc"
	cursor, err := decodeDirectoryComparisonCursor(req.Cursor, kind, filterDigest, req.Limit)
	if err != nil {
		return base.DirectoryCoveragePage{}, err
	}

	var expected *base.CatalogState
	if req.Cursor != "" {
		if cursor.CoverageAfter <= 0 || cursor.ReplicaAfter != (base.DirectoryReplicaAnchor{}) {
			return base.DirectoryCoveragePage{}, fmt.Errorf(
				"%w: invalid directory comparison cursor",
				database.ErrValidation,
			)
		}

		expected = &cursor.Catalog
	}

	page, err := a.db.ListDirectoryCoverage(
		ctx,
		req.Filters,
		req.Limit,
		cursor.CoverageAfter,
		expected,
	)
	if err != nil {
		return page, err
	}

	if len(page.Items) > req.Limit {
		page.Items = page.Items[:req.Limit]
		last := page.Items[len(page.Items)-1]

		page.NextCursor, err = encodeDirectoryComparisonCursor(directoryComparisonCursor{
			Version:       1,
			Kind:          kind,
			Catalog:       page.Catalog,
			Filters:       filterDigest,
			Limit:         req.Limit,
			CoverageAfter: last.Disk.Id,
		})
		if err != nil {
			return page, err
		}
	}

	return page, nil
}
