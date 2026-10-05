package app

import (
	"context"
	"fmt"

	"github.com/omgitsmoe/coldcat/internal/base"
	"github.com/omgitsmoe/coldcat/internal/database"
	"github.com/omgitsmoe/coldcat/internal/importer"
)

type App struct {
	db *database.DB
}

func New(db *database.DB) *App {
	return &App{db}
}

type ImportRequest = importer.Request

func (a *App) Import(ctx context.Context, req ImportRequest) (base.Snapshot, error) {
	result, err := importer.Import(ctx, a.db, req)
	if err != nil {
		return result, fmt.Errorf("failed to import %q for disk %v: %w", req.Path, req.DiskID, err)
	}
	return result, nil
}

func (a *App) ImportByLabel(ctx context.Context, label string, req ImportRequest) (base.Snapshot, error) {
	diskID, err := a.db.DiskIDByLabelContext(ctx, label)
	if err != nil {
		return base.Snapshot{}, err
	}
	req.DiskID = base.DiskId(diskID)
	return a.Import(ctx, req)
}

func (a *App) CreateDisk(ctx context.Context, label, notes, serial string, capacity int64) (base.DiskId, error) {
	id, err := a.db.CreateDiskContext(ctx, label, notes, serial, capacity)
	if err != nil {
		return 0, err
	}
	return base.DiskId(id), nil
}

func (a *App) LatestCompleteSnapshot(ctx context.Context, id base.DiskId) (base.Snapshot, error) {
	return a.db.LatestCompleteSnapshot(ctx, id)
}
func (a *App) GetCompleteSnapshot(ctx context.Context, id base.SnapshotId) (base.Snapshot, error) {
	return a.db.GetCompleteSnapshot(ctx, id)
}
func (a *App) GetContentSummary(ctx context.Context, id base.ContentId, scope base.Scope) (base.ContentSummary, error) {
	return a.db.GetContentSummary(ctx, id, scope)
}
func (a *App) GetObservationSummary(ctx context.Context, id base.FileObservationId) (base.ObservationSummary, error) {
	return a.db.GetObservationSummary(ctx, id)
}
