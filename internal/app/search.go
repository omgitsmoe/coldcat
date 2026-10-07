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

type SearchRequest struct {
	Filters base.SearchFilters
	Limit   int
	Cursor  string
}

type searchCursor struct {
	Version int                `json:"version"`
	Kind    string             `json:"kind"`
	Catalog base.CatalogState  `json:"catalog"`
	Filters base.SearchFilters `json:"filters"`
	Limit   int                `json:"limit"`
	After   base.SearchAnchor  `json:"after"`
}

func (a *App) Search(ctx context.Context, req SearchRequest) (base.SearchPage, error) {
	f := &req.Filters
	if f.Field == "" {
		f.Field = "name"
	}
	if f.Match == "" {
		f.Match = "substring"
	}
	if f.Scope == "" {
		f.Scope = base.ScopeCurrent
	}
	if f.ReplicaMetric == "" {
		f.ReplicaMetric = base.ReplicaDisks
	}
	if req.Limit == 0 {
		req.Limit = 50
	}
	version, kind := 3, "search:fold-v1:rank:path:id"
	if err := database.ValidateSearchFilters(*f); err != nil {
		return base.SearchPage{}, err
	}

	var cursor searchCursor
	var expected *base.CatalogState
	if req.Cursor != "" {
		invalid := func() (base.SearchPage, error) {
			return base.SearchPage{}, fmt.Errorf("%w: invalid search cursor", database.ErrValidation)
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
		if cursor.Version != version || cursor.Kind != kind || cursor.After.ID <= 0 ||
			cursor.Catalog.Revision < 0 || cursor.Limit != req.Limit ||
			!reflect.DeepEqual(cursor.Filters, req.Filters) {
			return invalid()
		}
		expected = &cursor.Catalog
	}
	page, err := a.db.Search(ctx, *f, req.Limit, cursor.After, expected)
	if err != nil {
		return page, err
	}
	if len(page.Items) > req.Limit {
		page.Items = page.Items[:req.Limit]
		last := page.Items[len(page.Items)-1]
		data, err := json.Marshal(searchCursor{
			Version: version, Kind: kind, Catalog: page.Catalog,
			Filters: *f, Limit: req.Limit,
			After: base.SearchAnchor{Relevance: last.Relevance,
				ID: last.Observation.Id},
		})
		if err != nil {
			return page, err
		}
		page.NextCursor = base64.RawURLEncoding.EncodeToString(data)
	}
	return page, nil
}
