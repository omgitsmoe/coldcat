package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"strconv"
	"time"

	"github.com/omgitsmoe/coldcat/internal/app"
	"github.com/omgitsmoe/coldcat/internal/base"
	"github.com/omgitsmoe/coldcat/internal/importer"
	"github.com/urfave/cli/v3"
)

type importResult struct {
	Snapshot  snapshotListItem `json:"snapshot"`
	ElapsedMS string           `json:"elapsed_ms"`
}

type importReporter struct {
	writer  io.Writer
	now     func() time.Time
	started time.Time
	last    time.Time
}

func (r *importReporter) report(p importer.Progress) error {
	now := r.now()
	if p.Phase == importer.ProgressImporting && !p.StreamingComplete &&
		!r.last.IsZero() && now.Sub(r.last) < 250*time.Millisecond {
		return nil
	}

	_, err := fmt.Fprintf(r.writer, "%s: %d files committed, elapsed %.3fs\n",
		p.Phase, p.CommittedFiles, now.Sub(r.started).Seconds())
	if err == nil {
		r.last = now
	}
	return err
}

func importCommand() *cli.Command {
	return &cli.Command{
		Name: "import", Usage: "import a complete disk inventory", ArgsUsage: "<file.cshd>",
		Flags: []cli.Flag{
			&cli.BoolFlag{Name: "allow-repeat", Usage: "explicitly repeat an imported inventory"},
			&cli.BoolFlag{Name: "json", Usage: "output a JSON final result"},
		},
		MutuallyExclusiveFlags: []cli.MutuallyExclusiveFlags{
			{Flags: [][]cli.Flag{
				{&cli.Int64Flag{Name: "disk-id", Usage: "disk ID"}},
				{&cli.StringFlag{Name: "label", Usage: "disk label"}},
			}, Required: true},
			{Flags: [][]cli.Flag{
				{&cli.StringFlag{Name: "captured-at", Usage: "inventory time (RFC3339)"}},
				{&cli.BoolFlag{Name: "use-source-mtime", Usage: "use checksum file mtime"}},
			}, Required: true},
		},
		Arguments: []cli.Argument{&cli.StringArg{Name: "checksum-file", Required: true}},
		OnUsageError: func(_ context.Context, _ *cli.Command, err error, _ bool) error {
			return err
		},
		Action: runImport,
	}
}

func runImport(ctx context.Context, cmd *cli.Command) error {
	req := app.ImportRequest{
		DiskID: base.DiskId(cmd.Int64("disk-id")), Path: cmd.StringArg("checksum-file"),
		UseSourceMTime: cmd.Bool("use-source-mtime"), AllowRepeat: cmd.Bool("allow-repeat"),
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

	reporter := importReporter{writer: cmd.ErrWriter, now: time.Now, started: time.Now()}
	req.Progress = reporter.report
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

		elapsed := reporter.now().Sub(reporter.started)
		if cmd.Bool("json") {
			return json.NewEncoder(cmd.Writer).Encode(importResult{
				Snapshot:  snapshotListValue(result),
				ElapsedMS: strconv.FormatInt(elapsed.Milliseconds(), 10),
			})
		}
		_, err = fmt.Fprintf(cmd.Writer,
			"imported snapshot %d: complete, %d files, %d contents, elapsed %.3fs\n",
			result.Id, result.FileCount, result.ContentCount, elapsed.Seconds())
		return err
	})
}
