package main

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"time"

	"github.com/omgitsmoe/coldcat/internal/app"
	"github.com/omgitsmoe/coldcat/internal/base"
	"github.com/urfave/cli/v3"
)

func diskCreateCommand(name string) *cli.Command {
	return &cli.Command{
		Name: name, Usage: "create a disk",
		Flags: []cli.Flag{
			&cli.StringFlag{Name: "label", Required: true},
			&cli.StringFlag{Name: "capacity", Required: true},
			&cli.StringFlag{Name: "serial"}, &cli.StringFlag{Name: "notes"},
		},
		Action: func(ctx context.Context, cmd *cli.Command) error {
			capacity, err := parseCapacity(cmd.String("capacity"))
			if err != nil {
				return fmt.Errorf("invalid capacity: %w", err)
			}

			return withCatalog(ctx, cmd, func(a *app.App) error {
				id, err := a.CreateDiskWithRequest(ctx, app.CreateDiskRequest{
					Label: cmd.String("label"), Notes: cmd.String("notes"),
					Serial: cmd.String("serial"), Capacity: capacity,
				})
				if err != nil {
					return err
				}

				_, err = fmt.Fprintf(cmd.Writer, "created disk %d\n", id)
				return err
			})
		},
	}
}

type snapshotListItem struct {
	ID                string `json:"id"`
	DiskID            string `json:"disk_id"`
	State             string `json:"state"`
	CapturedAt        string `json:"captured_at"`
	ImportedAt        string `json:"imported_at"`
	CaptureProvenance string `json:"capture_provenance"`
	InputPath         string `json:"input_path"`
	InputFormat       string `json:"input_format"`
	FileCount         string `json:"file_count"`
	ContentCount      string `json:"content_count"`
}

func snapshotListValue(s base.Snapshot) snapshotListItem {
	return snapshotListItem{
		ID: strconv.FormatInt(int64(s.Id), 10), DiskID: strconv.FormatInt(int64(s.DiskId), 10),
		State: "complete", CapturedAt: s.CapturedAt.UTC().Format(time.RFC3339Nano),
		ImportedAt:        s.ImportedAt.UTC().Format(time.RFC3339Nano),
		CaptureProvenance: s.CaptureProvenance, InputPath: s.InputPath, InputFormat: s.InputFormat,
		FileCount: strconv.FormatInt(s.FileCount, 10), ContentCount: strconv.FormatInt(s.ContentCount, 10),
	}
}

type diskListItem struct {
	ID             string            `json:"id"`
	Label          string            `json:"label"`
	Notes          *string           `json:"notes"`
	Serial         *string           `json:"serial"`
	Capacity       string            `json:"capacity"`
	LatestSnapshot *snapshotListItem `json:"latest_snapshot"`
}

func diskListValue(s base.DiskSummary) diskListItem {
	result := diskListItem{
		ID: strconv.FormatInt(int64(s.Disk.Id), 10), Label: s.Disk.Label,
		Capacity: strconv.FormatUint(s.Disk.Capacity, 10),
	}
	if s.Disk.Notes != "" {
		result.Notes = &s.Disk.Notes
	}
	if s.Disk.Serial != "" {
		result.Serial = &s.Disk.Serial
	}
	if s.LatestSnapshot != nil {
		snapshot := snapshotListValue(*s.LatestSnapshot)
		result.LatestSnapshot = &snapshot
	}
	return result
}

func diskCommands() *cli.Command {
	return &cli.Command{Name: "disk", Usage: "manage disks", Commands: []*cli.Command{
		diskCreateCommand("create"),
		{
			Name: "list", Usage: "list disks and their latest complete inventories",
			Flags: []cli.Flag{&cli.BoolFlag{Name: "json", Usage: "output a JSON array"}},
			Action: func(ctx context.Context, cmd *cli.Command) error {
				return withCatalog(ctx, cmd, func(a *app.App) error {
					items := []diskListItem{}
					req := app.ListDisksRequest{Limit: 200}
					for {
						page, err := a.ListDisks(ctx, req)
						if err != nil {
							return err
						}

						for _, item := range page.Items {
							value := diskListValue(item)
							if cmd.Bool("json") {
								items = append(items, value)
								continue
							}

							latest := "no inventory"
							if value.LatestSnapshot != nil {
								latest = "snapshot " + value.LatestSnapshot.ID +
									" captured " + value.LatestSnapshot.CapturedAt
							}
							if _, err := fmt.Fprintf(cmd.Writer, "%s\t%q\t%s bytes\t%s\n",
								value.ID, value.Label, value.Capacity, latest); err != nil {
								return err
							}
						}
						if page.NextCursor == "" {
							break
						}
						req.Cursor = page.NextCursor
					}
					if cmd.Bool("json") {
						return json.NewEncoder(cmd.Writer).Encode(items)
					}
					return nil
				})
			},
		},
	}}
}

func snapshotCommands() *cli.Command {
	return &cli.Command{Name: "snapshot", Usage: "inspect complete inventories", Commands: []*cli.Command{
		{
			Name: "list", Usage: "list a disk's complete inventories",
			Flags: []cli.Flag{
				&cli.Int64Flag{Name: "disk-id", Required: true},
				&cli.BoolFlag{Name: "json", Usage: "output a JSON array"},
			},
			Action: func(ctx context.Context, cmd *cli.Command) error {
				return withCatalog(ctx, cmd, func(a *app.App) error {
					items := []snapshotListItem{}
					req := app.ListDiskSnapshotsRequest{DiskID: base.DiskId(cmd.Int64("disk-id")), Limit: 200}
					for {
						page, err := a.ListDiskSnapshots(ctx, req)
						if err != nil {
							return err
						}

						for _, item := range page.Items {
							value := snapshotListValue(item)
							if cmd.Bool("json") {
								items = append(items, value)
								continue
							}

							if _, err := fmt.Fprintf(cmd.Writer, "%s\t%s\t%s files\t%s contents\n",
								value.ID, value.CapturedAt, value.FileCount, value.ContentCount); err != nil {
								return err
							}
						}
						if page.NextCursor == "" {
							break
						}
						req.Cursor = page.NextCursor
					}
					if cmd.Bool("json") {
						return json.NewEncoder(cmd.Writer).Encode(items)
					}
					return nil
				})
			},
		},
	}}
}
