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
	return serveCatalog(ctx, cmd.String("db"), cmd.String("listen"), cmd.ErrWriter)
}

func serveCatalog(ctx context.Context, path, address string, output io.Writer) (result error) {
	if _, _, err := net.SplitHostPort(address); err != nil {
		return fmt.Errorf("invalid listen address: %w", err)
	}
	db, err := database.OpenContext(ctx, path)
	if err != nil {
		return fmt.Errorf("open catalog: %w", err)
	}
	defer func() { result = errors.Join(result, db.Close()) }()
	listener, err := (&net.ListenConfig{}).Listen(ctx, "tcp", address)
	if err != nil {
		return fmt.Errorf("listen: %w", err)
	}
	defer listener.Close()
	server := &http.Server{
		Handler:           httpapi.New(app.New(db)),
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
