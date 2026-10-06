package app

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"time"

	"github.com/omgitsmoe/coldcat/internal/base"
	"github.com/omgitsmoe/coldcat/internal/database"
)

type ListDiskSnapshotsRequest struct {
	DiskID base.DiskId
	Limit  int
	Cursor string
}

type snapshotCursor struct {
	Version         int               `json:"version"`
	Kind            string            `json:"kind"`
	Catalog         base.CatalogState `json:"catalog"`
	DiskID          base.DiskId       `json:"disk_id"`
	Limit           int               `json:"limit"`
	AfterID         base.SnapshotId   `json:"after_id"`
	AfterCapturedAt time.Time         `json:"after_captured_at"`
}

func (a *App) ListDiskSnapshots(
	ctx context.Context,
	req ListDiskSnapshotsRequest,
) (base.SnapshotPage, error) {
	if req.Limit == 0 {
		req.Limit = 50
	}

	var cursor snapshotCursor
	var expected *base.CatalogState
	if req.Cursor != "" {
		if len(req.Cursor) > 2048 {
			return base.SnapshotPage{}, fmt.Errorf("%w: cursor too large", database.ErrValidation)
		}

		data, err := base64.RawURLEncoding.Strict().DecodeString(req.Cursor)
		if err != nil {
			return base.SnapshotPage{}, fmt.Errorf("%w: malformed cursor", database.ErrValidation)
		}

		decoder := json.NewDecoder(bytes.NewReader(data))
		decoder.DisallowUnknownFields()
		if err := decoder.Decode(&cursor); err != nil {
			return base.SnapshotPage{}, fmt.Errorf("%w: malformed cursor", database.ErrValidation)
		}

		if err := decoder.Decode(new(any)); err != io.EOF {
			return base.SnapshotPage{}, fmt.Errorf("%w: malformed cursor", database.ErrValidation)
		}

		if cursor.Version != 1 || cursor.Kind != "disk_snapshots" || cursor.DiskID != req.DiskID ||
			cursor.Limit != req.Limit || cursor.AfterID <= 0 || cursor.AfterCapturedAt.IsZero() ||
			cursor.Catalog.Revision < 0 {
			return base.SnapshotPage{}, fmt.Errorf(
				"%w: cursor does not match request", database.ErrValidation,
			)
		}

		expected = &cursor.Catalog
	}

	page, err := a.db.ListDiskSnapshots(
		ctx, req.DiskID, req.Limit, cursor.AfterID, cursor.AfterCapturedAt, expected,
	)
	if err != nil {
		return page, err
	}

	if len(page.Items) > req.Limit {
		page.Items = page.Items[:req.Limit]
		last := page.Items[len(page.Items)-1]
		cursor = snapshotCursor{
			Version: 1, Kind: "disk_snapshots", Catalog: page.Catalog,
			DiskID: req.DiskID, Limit: req.Limit,
			AfterID: last.Id, AfterCapturedAt: last.CapturedAt,
		}
		data, err := json.Marshal(cursor)
		if err != nil {
			return page, err
		}

		page.NextCursor = base64.RawURLEncoding.EncodeToString(data)
	}

	return page, nil
}
