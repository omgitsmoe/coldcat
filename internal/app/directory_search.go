package app

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"

	"github.com/omgitsmoe/coldcat/internal/base"
	"github.com/omgitsmoe/coldcat/internal/database"
)

type DirectorySearchRequest struct {
	Filters base.DirectorySearchFilters
	Limit   int
	Cursor  string
}

type directorySearchCursor struct {
	Version int                         `json:"version"`
	Kind    string                      `json:"kind"`
	Catalog base.CatalogState           `json:"catalog"`
	Filters base.DirectorySearchFilters `json:"filters"`
	Limit   int                         `json:"limit"`
	AfterID int64                       `json:"after_id"`
}

func (a *App) SearchDirectories(
	ctx context.Context, req DirectorySearchRequest,
) (base.DirectorySearchPage, error) {
	f := &req.Filters
	if f.Field == "" {
		f.Field = "name"
	}
	if f.Match == "" {
		f.Match = "syntax"
	}
	if f.Scope == "" {
		f.Scope = base.ScopeCurrent
	}
	if req.Limit == 0 {
		req.Limit = 50
	}
	if err := database.ValidateDirectorySearchFilters(*f); err != nil {
		return base.DirectorySearchPage{}, err
	}
	const kind = "directory-search:syntax-v3:fold-v1:path:id"
	var cursor directorySearchCursor
	var expected *base.CatalogState
	if req.Cursor != "" {
		invalid := func() (base.DirectorySearchPage, error) {
			return base.DirectorySearchPage{}, fmt.Errorf(
				"%w: invalid directory search cursor", database.ErrValidation)
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
		if cursor.Version != 1 || cursor.Kind != kind || cursor.AfterID <= 0 ||
			cursor.Catalog.Revision < 0 || cursor.Limit != req.Limit || cursor.Filters != *f {
			return invalid()
		}
		expected = &cursor.Catalog
	}
	page, err := a.db.SearchDirectories(ctx, *f, req.Limit, cursor.AfterID, expected)
	if err != nil {
		return page, err
	}
	if len(page.Items) > req.Limit {
		page.Items = page.Items[:req.Limit]
		data, err := json.Marshal(directorySearchCursor{
			Version: 1, Kind: kind, Catalog: page.Catalog, Filters: *f, Limit: req.Limit,
			AfterID: page.Items[len(page.Items)-1].ID,
		})
		if err != nil {
			return page, err
		}
		page.NextCursor = base64.RawURLEncoding.EncodeToString(data)
	}
	return page, nil
}
