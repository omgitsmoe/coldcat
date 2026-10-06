package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/omgitsmoe/coldcat/internal/app"
	"github.com/omgitsmoe/coldcat/internal/base"
	"github.com/omgitsmoe/coldcat/internal/database"
	"github.com/urfave/cli/v3"
)

func withCatalog(ctx context.Context, cmd *cli.Command, fn func(*app.App) error) error {
	db, err := database.OpenContext(ctx, cmd.String("db"))
	if err != nil {
		return fmt.Errorf("open catalog: %w", err)
	}
	defer db.Close()
	return fn(app.New(db))
}

func newCommand() *cli.Command {
	diskIDFlag := &cli.Int64Flag{Name: "disk-id", Usage: "disk ID"}
	labelFlag := &cli.StringFlag{Name: "label", Usage: "disk label"}
	capturedFlag := &cli.StringFlag{Name: "captured-at", Usage: "inventory time (RFC3339)"}
	sourceMTimeFlag := &cli.BoolFlag{
		Name:  "use-source-mtime",
		Usage: "explicitly use the checksum file mtime as inventory time",
	}
	allowRepeatFlag := &cli.BoolFlag{
		Name:  "allow-repeat",
		Usage: "explicitly record another snapshot of an already imported inventory",
	}
	return &cli.Command{
		Name: "coldcat", Usage: "manage checksum data for disks",
		Flags: []cli.Flag{
			&cli.StringFlag{Name: "db", Usage: "catalog database path", Value: "coldcat.sqlite"},
		},
		Commands: []*cli.Command{
			{
				Name:  "serve",
				Usage: "serve the catalog HTTP API; stop before importing",
				Flags: []cli.Flag{
					&cli.StringFlag{
						Name:  "listen",
						Usage: "HTTP listen address",
						Value: "127.0.0.1:8080",
					},
				},
				Action: serveCommand,
			},
			diskCreateCommand("create"),
			diskCommands(),
			snapshotCommands(),
			{Name: "import", Usage: "import a complete disk inventory", ArgsUsage: "<file.cshd>",
				Flags: []cli.Flag{allowRepeatFlag},
				MutuallyExclusiveFlags: []cli.MutuallyExclusiveFlags{
					{Flags: [][]cli.Flag{{diskIDFlag}, {labelFlag}}, Required: true},
					{Flags: [][]cli.Flag{{capturedFlag}, {sourceMTimeFlag}}, Required: true},
				}, Arguments: []cli.Argument{&cli.StringArg{Name: "checksum-file", Required: true}},
				Action: func(ctx context.Context, cmd *cli.Command) error {
					req := app.ImportRequest{
						DiskID:         base.DiskId(cmd.Int64("disk-id")),
						Path:           cmd.StringArg("checksum-file"),
						UseSourceMTime: cmd.Bool("use-source-mtime"),
						AllowRepeat:    cmd.Bool("allow-repeat"),
					}
					if cmd.IsSet("captured-at") {
						captured, err := time.Parse(time.RFC3339Nano, cmd.String("captured-at"))
						if err != nil {
							return fmt.Errorf("invalid captured-at: %w", err)
						}

						req.CapturedAt = captured
					} else if !req.UseSourceMTime {
						return fmt.Errorf("use-source-mtime must be true")
					}

					return withCatalog(ctx, cmd, func(a *app.App) error {
						var result base.Snapshot
						var err error
						if cmd.IsSet("label") {
							result, err = a.ImportByLabel(ctx, cmd.String("label"), req)
						} else {
							result, err = a.Import(ctx, req)
						}

						if err != nil {
							return err
						}

						_, err = fmt.Fprintf(
							cmd.Writer,
							"imported snapshot %d: complete, %d files, %d contents\n",
							result.Id,
							result.FileCount,
							result.ContentCount,
						)
						return err
					})
				},
			},
		},
	}
}

func main() {
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()
	if err := newCommand().Run(ctx, os.Args); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
