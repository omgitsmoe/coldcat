package app

import (
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

func (a *App) Import(disk base.DiskId, path string) error {
	if err := importer.Import(a.db, disk, path); err != nil {
		return fmt.Errorf("failed to import %q for disk %v: %w", path, disk, err)
	}
	return nil
}

func (a *App) ImportByLabel(label, path string) error {
	diskID, err := a.db.DiskIDByLabel(label)
	if err != nil {
		return err
	}
	return a.Import(base.DiskId(diskID), path)
}

func (a *App) CreateDisk(label, notes, serial string, capacity int64) (base.DiskId, error) {
	id, err := a.db.CreateDisk(label, notes, serial, capacity)
	if err != nil {
		return 0, err
	}
	return base.DiskId(id), nil
}
