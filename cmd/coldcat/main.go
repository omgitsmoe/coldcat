package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"github.com/omgitsmoe/coldcat/internal/app"
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
					&cli.StringFlag{
						Name:  "assets",
						Usage: "built frontend directory (optional; served at origin root)",
					},
				},
				Action: serveCommand,
			},
			diskCreateCommand("create"),
			diskCommands(),
			snapshotCommands(),
			importCommand(),
		},
	}
}

func main() {
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()
	if code := runCommand(ctx, newCommand(), os.Args); code != 0 {
		os.Exit(code)
	}
}

func runCommand(ctx context.Context, cmd *cli.Command, args []string) int {
	if err := cmd.Run(ctx, args); err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	return 0
}
