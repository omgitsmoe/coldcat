package app

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"

	"github.com/omgitsmoe/coldcat/internal/base"
	"github.com/omgitsmoe/coldcat/internal/database"
)

type LookupContentRequest struct {
	HashType string
	Hash     string
	Scope    base.Scope
}

func (a *App) LookupContent(
	ctx context.Context,
	req LookupContentRequest,
) (base.ContentSummary, error) {
	hash, err := hex.DecodeString(req.Hash)
	if err != nil || len(hash) == 0 {
		return base.ContentSummary{}, fmt.Errorf(
			"%w: hash must be nonempty hexadecimal",
			database.ErrValidation,
		)
	}

	if req.Scope == "" {
		req.Scope = base.ScopeCurrent
	}

	return a.db.LookupContent(ctx, req.HashType, hash, req.Scope)
}

func (a *App) GetCatalogState(ctx context.Context) (base.CatalogState, error) {
	return a.db.GetCatalogState(ctx)
}

func (a *App) CheckReadiness(ctx context.Context) error {
	_, err := a.GetCatalogState(ctx)
	return err
}

type ListContentObservationsRequest struct {
	ContentID base.ContentId
	Scope     base.Scope
	Limit     int
	Cursor    string
}

type observationCursor struct {
	Version   int                    `json:"version"`
	Catalog   base.CatalogState      `json:"catalog"`
	ContentID base.ContentId         `json:"content_id"`
	Scope     base.Scope             `json:"scope"`
	Limit     int                    `json:"limit"`
	After     base.FileObservationId `json:"after"`
}

func (a *App) ListContentObservations(
	ctx context.Context,
	req ListContentObservationsRequest,
) (base.ContentObservationPage, error) {
	if req.Scope == "" {
		req.Scope = base.ScopeCurrent
	}

	if req.Limit == 0 {
		req.Limit = 50
	}

	var cursor observationCursor
	var expected *base.CatalogState
	if req.Cursor != "" {
		if len(req.Cursor) > 2048 {
			return base.ContentObservationPage{}, fmt.Errorf(
				"%w: cursor too large",
				database.ErrValidation,
			)
		}

		data, err := base64.RawURLEncoding.Strict().DecodeString(req.Cursor)
		if err != nil {
			return base.ContentObservationPage{}, fmt.Errorf(
				"%w: malformed cursor",
				database.ErrValidation,
			)
		}

		decoder := json.NewDecoder(bytes.NewReader(data))
		decoder.DisallowUnknownFields()
		if err := decoder.Decode(&cursor); err != nil {
			return base.ContentObservationPage{}, fmt.Errorf(
				"%w: malformed cursor",
				database.ErrValidation,
			)
		}

		if err := decoder.Decode(new(any)); err != io.EOF {
			return base.ContentObservationPage{}, fmt.Errorf(
				"%w: malformed cursor",
				database.ErrValidation,
			)
		}

		if cursor.Version != 1 || cursor.ContentID != req.ContentID || cursor.Scope != req.Scope ||
			cursor.Limit != req.Limit ||
			cursor.After <= 0 ||
			cursor.Catalog.Revision < 0 {
			return base.ContentObservationPage{}, fmt.Errorf(
				"%w: cursor does not match request",
				database.ErrValidation,
			)
		}

		expected = &cursor.Catalog
	}

	page, err := a.db.ListContentObservations(
		ctx,
		req.ContentID,
		req.Scope,
		req.Limit,
		cursor.After,
		expected,
	)
	if err != nil {
		return page, err
	}

	if len(page.Items) > req.Limit {
		page.Items = page.Items[:req.Limit]
		cursor = observationCursor{
			Version:   1,
			Catalog:   page.Catalog,
			ContentID: req.ContentID,
			Scope:     req.Scope,
			Limit:     req.Limit,
			After:     page.Items[len(page.Items)-1].Observation.Id,
		}
		data, err := json.Marshal(cursor)
		if err != nil {
			return page, err
		}

		page.NextCursor = base64.RawURLEncoding.EncodeToString(data)
	}

	return page, nil
}
