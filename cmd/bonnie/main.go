package main

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/flag-ai/commons/config"
	"github.com/flag-ai/commons/health"
	"github.com/flag-ai/commons/secrets"
	"github.com/flag-ai/commons/version"

	"github.com/flag-ai/bonnie/internal/server"
)

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	// Bootstrap secrets provider: try OpenBao, fall back to env.
	provider, err := secrets.NewProvider(secrets.ProviderOpenBao, nil)
	if err != nil {
		provider, _ = secrets.NewProvider(secrets.ProviderEnv, nil)
	}

	cfg, err := config.LoadBase(ctx, "bonnie", provider)
	if err != nil {
		slog.Error("failed to load config", "error", err)
		os.Exit(1)
	}

	logger := cfg.Logger()
	logger.Info("starting bonnie", "version", version.Info(), "addr", cfg.ListenAddr)

	registry := health.NewRegistry()

	srv := server.New(cfg.ListenAddr, logger, registry)

	// Start server in background.
	errCh := make(chan error, 1)
	go func() {
		if err := srv.Start(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			errCh <- err
		}
		close(errCh)
	}()

	// Wait for shutdown signal or server error.
	select {
	case <-ctx.Done():
		logger.Info("shutdown signal received")
	case err := <-errCh:
		if err != nil {
			logger.Error("server error", "error", err)
		}
	}

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	if err := srv.Shutdown(shutdownCtx); err != nil {
		logger.Error("shutdown error", "error", err)
		os.Exit(1)
	}

	logger.Info("bonnie stopped")
}
