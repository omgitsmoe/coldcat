package app

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"reflect"

	"github.com/omgitsmoe/coldcat/internal/base"
	"github.com/omgitsmoe/coldcat/internal/database"
)

type ListContentsRequest struct {
	Filters base.ContentFilters
	Limit   int
	Cursor  string
}

type contentCursor struct {
	Version int                 `json:"version"`
	Kind    string              `json:"kind"`
	Catalog base.CatalogState   `json:"catalog"`
	Filters base.ContentFilters `json:"filters"`
	Limit   int                 `json:"limit"`
	After   base.ContentId      `json:"after"`
}

func (a *App) ListContents(ctx context.Context, req ListContentsRequest) (base.ContentPage, error) {
	if req.Filters.Scope == "" {
		req.Filters.Scope = base.ScopeCurrent
	}

	if req.Filters.ReplicaMetric == "" {
		req.Filters.ReplicaMetric = base.ReplicaDisks
	}
	if req.Limit == 0 {
		req.Limit = 50
	}
	if err := database.ValidateContentFilters(req.Filters); err != nil {
		return base.ContentPage{}, err
	}

	var cursor contentCursor
	var expected *base.CatalogState
	if req.Cursor != "" {
		invalid := func() (base.ContentPage, error) {
			return base.ContentPage{}, fmt.Errorf("%w: invalid content cursor", database.ErrValidation)
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
		if cursor.Version != 1 || cursor.Kind != "contents:id:asc" || cursor.After <= 0 ||
			cursor.Catalog.Revision < 0 || cursor.Limit != req.Limit ||
			!reflect.DeepEqual(cursor.Filters, req.Filters) {
			return invalid()
		}
		expected = &cursor.Catalog
	}

	page, err := a.db.ListContents(ctx, req.Filters, req.Limit, cursor.After, expected)
	if err != nil {
		return page, err
	}

	if len(page.Items) > req.Limit {
		page.Items = page.Items[:req.Limit]
		data, err := json.Marshal(contentCursor{
			Version: 1, Kind: "contents:id:asc", Catalog: page.Catalog,
			Filters: req.Filters, Limit: req.Limit, After: page.Items[len(page.Items)-1].Content.Id,
		})
		if err != nil {
			return page, err
		}

		page.NextCursor = base64.RawURLEncoding.EncodeToString(data)
	}

	return page, nil
}
