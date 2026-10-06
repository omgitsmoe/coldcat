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

type CreateDiskRequest struct {
	Label    string
	Notes    string
	Serial   string
	Capacity int64
}

type UpdateDiskRequest = base.UpdateDiskRequest

func (a *App) CreateDiskWithRequest(ctx context.Context, req CreateDiskRequest) (base.DiskId, error) {
	id, err := a.db.CreateDiskContext(ctx, req.Label, req.Notes, req.Serial, req.Capacity)
	return base.DiskId(id), err
}

func (a *App) GetDisk(ctx context.Context, id base.DiskId) (base.DiskDetail, error) {
	return a.db.GetDisk(ctx, id)
}

func (a *App) UpdateDisk(
	ctx context.Context, id base.DiskId, req UpdateDiskRequest,
) (base.DiskDetail, error) {
	return a.db.UpdateDisk(ctx, id, req)
}

type ListDisksRequest struct {
	Limit  int
	Cursor string
}

type diskCursor struct {
	Version int               `json:"version"`
	Kind    string            `json:"kind"`
	Catalog base.CatalogState `json:"catalog"`
	Limit   int               `json:"limit"`
	AfterID base.DiskId       `json:"after_id"`
	MaxID   base.DiskId       `json:"max_id"`
}

func (a *App) ListDisks(ctx context.Context, req ListDisksRequest) (base.DiskPage, error) {
	if req.Limit == 0 {
		req.Limit = 50
	}

	var cursor diskCursor
	var expected *base.CatalogState
	if req.Cursor != "" {
		if len(req.Cursor) > 2048 {
			return base.DiskPage{}, fmt.Errorf("%w: cursor too large", database.ErrValidation)
		}

		data, err := base64.RawURLEncoding.Strict().DecodeString(req.Cursor)
		if err != nil {
			return base.DiskPage{}, fmt.Errorf("%w: malformed cursor", database.ErrValidation)
		}

		decoder := json.NewDecoder(bytes.NewReader(data))
		decoder.DisallowUnknownFields()
		if err := decoder.Decode(&cursor); err != nil {
			return base.DiskPage{}, fmt.Errorf("%w: malformed cursor", database.ErrValidation)
		}

		if err := decoder.Decode(new(any)); err != io.EOF {
			return base.DiskPage{}, fmt.Errorf("%w: malformed cursor", database.ErrValidation)
		}

		if cursor.Version != 1 || cursor.Kind != "disks" || cursor.Limit != req.Limit ||
			cursor.AfterID <= 0 || cursor.MaxID <= cursor.AfterID || cursor.Catalog.Revision < 0 {
			return base.DiskPage{}, fmt.Errorf(
				"%w: cursor does not match request", database.ErrValidation,
			)
		}

		expected = &cursor.Catalog
	}

	page, err := a.db.ListDisks(ctx, req.Limit, cursor.AfterID, cursor.MaxID, expected)
	if err != nil {
		return page, err
	}

	if len(page.Items) > req.Limit {
		page.Items = page.Items[:req.Limit]
		cursor = diskCursor{
			Version: 1, Kind: "disks", Catalog: page.Catalog, Limit: req.Limit,
			AfterID: page.Items[len(page.Items)-1].Disk.Id, MaxID: page.MaxID,
		}
		data, err := json.Marshal(cursor)
		if err != nil {
			return page, err
		}

		page.NextCursor = base64.RawURLEncoding.EncodeToString(data)
	}

	return page, nil
}
