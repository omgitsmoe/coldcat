package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"

	"github.com/omgitsmoe/coldcat/internal/database"
	"github.com/urfave/cli/v3"
)

func catalogCommand() *cli.Command {
	return &cli.Command{
		Name: "catalog", Usage: "inspect, validate, or compact an offline catalog",
		Commands: []*cli.Command{
			{
				Name: "stats", Usage: "scan allocated bytes by table and index (may take time)",
				Flags: []cli.Flag{&cli.BoolFlag{Name: "json"}},
				Action: func(ctx context.Context, cmd *cli.Command) error {
					return withMaintenanceCatalog(ctx, cmd, func(db *database.DB) error {
						report, err := db.Storage(ctx)
						if err != nil {
							return err
						}
						if cmd.Bool("json") {
							return json.NewEncoder(cmd.Writer).Encode(report)
						}
						if _, err := fmt.Fprintf(cmd.Writer,
							"allocated: %d bytes; free: %d bytes; snapshots: %d; historical files: %d\n",
							report.PageSize*report.PageCount, report.PageSize*report.FreePages,
							report.Snapshots, report.HistoricalFiles); err != nil {
							return err
						}
						for _, object := range report.Objects {
							if _, err := fmt.Fprintf(cmd.Writer, "%12d  %s\n", object.Bytes,
								object.Name); err != nil {
								return err
							}
						}
						return nil
					})
				},
			},
			{
				Name: "check", Usage: "run full SQLite integrity and foreign-key checks",
				Action: func(ctx context.Context, cmd *cli.Command) error {
					return withMaintenanceCatalog(ctx, cmd, func(db *database.DB) error {
						if err := db.Check(ctx); err != nil {
							return err
						}
						_, err := fmt.Fprintln(cmd.Writer, "catalog checks passed")
						return err
					})
				},
			},
			{
				Name: "compact", Usage: "write a validated, compacted copy; needs extra disk space",
				Flags: []cli.Flag{&cli.StringFlag{Name: "output", Required: true}},
				Action: func(ctx context.Context, cmd *cli.Command) error {
					return withMaintenanceCatalog(ctx, cmd, func(db *database.DB) error {
						if err := db.CompactInto(ctx, cmd.String("output")); err != nil {
							return err
						}
						_, err := fmt.Fprintf(cmd.Writer, "compacted catalog: %s\n", cmd.String("output"))
						return err
					})
				},
			},
		},
	}
}

func withMaintenanceCatalog(
	ctx context.Context, cmd *cli.Command, fn func(*database.DB) error,
) (result error) {
	path := cmd.String("db")
	if _, err := os.Stat(path); err != nil {
		return fmt.Errorf("existing catalog required: %w", err)
	}
	db, err := database.OpenContext(ctx, path)
	if err != nil {
		return err
	}
	defer func() { result = errors.Join(result, db.Close()) }()
	return fn(db)
}
