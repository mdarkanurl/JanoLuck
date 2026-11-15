package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/mdarkanurl/JanoLuck/internal/server"
)

const shutdownTimeout = 15 * time.Second

func main() {
	if err := run(); err != nil {
		slog.Error("http server stopped with error", "error", err)
		os.Exit(1)
	}
}

func run() (err error) {
	srv := server.New()
	defer func() {
		if closeErr := srv.Close(); closeErr != nil {
			if err == nil {
				err = fmt.Errorf("close server resources: %w", closeErr)
				return
			}
			slog.Error("failed to close server resources", "error", closeErr)
		}
	}()

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	serveErr := make(chan error, 1)
	go func() {
		if startErr := srv.Start(); startErr != nil && !errors.Is(startErr, http.ErrServerClosed) {
			serveErr <- startErr
			return
		}
		serveErr <- nil
	}()

	select {
	case err := <-serveErr:
		return err
	case <-ctx.Done():
		stop()
	}

	slog.Info("shutting down http server", "timeout", shutdownTimeout)

	shutdownCtx, cancel := context.WithTimeout(context.Background(), shutdownTimeout)
	defer cancel()

	if shutdownErr := srv.Shutdown(shutdownCtx); shutdownErr != nil {
		slog.Warn("graceful shutdown did not complete in time", "error", shutdownErr)
	}

	if err := <-serveErr; err != nil {
		return err
	}

	slog.Info("http server stopped")
	return nil
}
