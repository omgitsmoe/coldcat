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
