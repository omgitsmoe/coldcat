package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"time"

	"github.com/omgitsmoe/coldcat/internal/app"
	"github.com/omgitsmoe/coldcat/internal/database"
	"github.com/omgitsmoe/coldcat/internal/httpapi"
	"github.com/urfave/cli/v3"
)

func serveCommand(ctx context.Context, cmd *cli.Command) error {
	if cmd.IsSet("assets") && cmd.String("assets") == "" {
		return fmt.Errorf("assets directory must not be empty")
	}

	if cmd.Bool("api-only") && cmd.IsSet("assets") {
		return fmt.Errorf("--api-only and --assets cannot be combined")
	}

	return serveCatalogUI(ctx, cmd.String("db"), cmd.String("listen"),
		cmd.String("assets"), cmd.Bool("api-only"), cmd.ErrWriter)
}

func serveCatalog(ctx context.Context, path, address string, output io.Writer) error {
	return serveCatalogUI(ctx, path, address, "", false, output)
}

func serveCatalogUI(
	ctx context.Context, path, address, assets string, apiOnly bool, output io.Writer,
) (result error) {
	if _, _, err := net.SplitHostPort(address); err != nil {
		return fmt.Errorf("invalid listen address: %w", err)
	}

	db, err := database.OpenContext(ctx, path)
	if err != nil {
		return fmt.Errorf("open catalog: %w", err)
	}
	defer func() { result = errors.Join(result, db.Close()) }()

	handler := httpapi.New(app.New(db))
	if assets != "" {
		var closeAssets func() error
		handler, closeAssets, err = httpapi.WithAssets(handler, assets)
		if err != nil {
			return err
		}
		defer func() { result = errors.Join(result, closeAssets()) }()
	} else if !apiOnly {
		handler, err = defaultAssetHandler(handler)
		if err != nil {
			return err
		}
	}

	listener, err := (&net.ListenConfig{}).Listen(ctx, "tcp", address)
	if err != nil {
		return fmt.Errorf("listen: %w", err)
	}
	defer listener.Close()
	server := &http.Server{
		Handler:           handler,
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       15 * time.Second,
		WriteTimeout:      30 * time.Second,
		IdleTimeout:       60 * time.Second,
	}
	if _, err := fmt.Fprintf(output, "serving http://%s\n", listener.Addr()); err != nil {
		return err
	}

	done := make(chan error, 1)
	go func() { done <- server.Serve(listener) }()
	select {
	case err := <-done:
		return errors.Join(err, server.Close())
	case <-ctx.Done():
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		shutdownErr := server.Shutdown(shutdownCtx)
		if shutdownErr != nil {
			shutdownErr = errors.Join(shutdownErr, server.Close())
		}

		serveErr := <-done
		if errors.Is(serveErr, http.ErrServerClosed) {
			serveErr = nil
		}

		return errors.Join(shutdownErr, serveErr)
	}
}
