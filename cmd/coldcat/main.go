package main

import (
	"context"
	"fmt"
	"os"

	"github.com/omgitsmoe/coldcat/internal/app"
	"github.com/omgitsmoe/coldcat/internal/base"
	"github.com/omgitsmoe/coldcat/internal/database"
	"github.com/urfave/cli/v3"
)

func main() {
	diskIDFlag := &cli.Int64Flag{Name: "disk-id", Usage: "disk ID"}
	labelFlag := &cli.StringFlag{Name: "label", Usage: "disk label"}
	command := &cli.Command{
		Name:  "coldcat",
		Usage: "manage checksum data for disks",
		Commands: []*cli.Command{
			{
				Name:  "create",
				Usage: "create a disk",
				Flags: []cli.Flag{
					&cli.StringFlag{Name: "label", Usage: "disk label", Required: true},
					&cli.StringFlag{Name: "capacity", Usage: "disk capacity (for example, 2TB)", Required: true},
					&cli.StringFlag{Name: "serial", Usage: "disk serial number"},
					&cli.StringFlag{Name: "notes", Usage: "optional notes about the disk"},
				},
				Action: func(ctx context.Context, cmd *cli.Command) error {
					capacity, err := parseCapacity(cmd.String("capacity"))
					if err != nil {
						return fmt.Errorf("invalid capacity %q: %w", cmd.String("capacity"), err)
					}

					db, err := database.Open("coldcat.sqlite")
					if err != nil {
						return fmt.Errorf("failed to open database: %w", err)
					}
					defer db.Close()

					id, err := app.New(db).CreateDisk(
						cmd.String("label"),
						cmd.String("notes"),
						cmd.String("serial"),
						capacity,
					)
					if err != nil {
						return err
					}
					fmt.Printf("created disk %d\n", id)
					return nil
				},
			},
			{
				Name:      "import",
				Usage:     "import checksums for a disk",
				ArgsUsage: "<path-to-checksum-file>",
				MutuallyExclusiveFlags: []cli.MutuallyExclusiveFlags{{
					Flags: [][]cli.Flag{
						{diskIDFlag},
						{labelFlag},
					},
					Required: true,
				}},
				Arguments: []cli.Argument{
					&cli.StringArg{Name: "checksum-file", Required: true},
				},
				Action: func(ctx context.Context, cmd *cli.Command) error {
					db, err := database.Open("coldcat.sqlite")
					if err != nil {
						return fmt.Errorf("failed to open database: %w", err)
					}
					defer db.Close()

					application := app.New(db)
					if cmd.IsSet("label") {
						return application.ImportByLabel(cmd.String("label"), cmd.StringArg("checksum-file"))
					}
					return application.Import(
						base.DiskId(cmd.Int64("disk-id")),
						cmd.StringArg("checksum-file"),
					)
				},
			},
		},
	}

	if err := command.Run(context.Background(), os.Args); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
