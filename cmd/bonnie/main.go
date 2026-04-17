// Package main is the entrypoint for the BONNIE GPU host agent.
package main

import (
	"context"
	"errors"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	dockerclient "github.com/docker/docker/client"

	"github.com/flag-ai/commons/health"
	"github.com/flag-ai/commons/secrets"
	"github.com/flag-ai/commons/version"

	"github.com/flag-ai/bonnie/internal/api"
	"github.com/flag-ai/bonnie/internal/config"
	"github.com/flag-ai/bonnie/internal/container"
	"github.com/flag-ai/bonnie/internal/gpu"
	"github.com/flag-ai/bonnie/internal/storage"
)

func main() {
	if err := run(); err != nil {
		slog.Error("fatal", "error", err)
		os.Exit(1)
	}
}

func run() error {
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	// Bootstrap secrets provider: try OpenBao, fall back to env.
	provider, err := secrets.NewProvider(secrets.ProviderOpenBao, nil)
	if err != nil {
		provider, _ = secrets.NewProvider(secrets.ProviderEnv, nil)
	}

	cfg, err := config.Load(ctx, provider)
	if err != nil {
		return err
	}

	logger := cfg.Logger()
	logger.Info("starting bonnie", "version", version.Info(), "addr", cfg.ListenAddr)

	// GPU detection and polling
	runner := &gpu.ExecRunner{}
	detector := gpu.NewDetector(runner, logger)
	poller := gpu.NewPoller(detector, time.Duration(cfg.PollInterval)*time.Second, logger)
	poller.Start(ctx)

	snap := poller.Latest()
	logger.Info("gpu detection complete", "vendor", snap.Vendor, "count", len(snap.GPUs))

	// Docker client
	dockerClient, err := dockerclient.NewClientWithOpts(
		dockerclient.WithHost(cfg.DockerHost),
		dockerclient.WithAPIVersionNegotiation(),
	)
	if err != nil {
		return err
	}
	defer func() { _ = dockerClient.Close() }()

	// Container manager
	manager := container.NewManager(dockerClient, snap.Vendor, logger)

	// Model storage cache (fatal if the directory can't be created).
	if cfg.HFToken == "" {
		logger.Warn("HF token not configured; fetches of gated HuggingFace models will fail")
	}
	modelStore, err := storage.NewStore(cfg.ModelStorageDir, logger, cfg.HFToken)
	if err != nil {
		return err
	}

	// Health registry with Docker socket checker
	registry := health.NewRegistry()
	registry.Register(&dockerChecker{client: dockerClient})

	// Build router
	router := api.NewRouter(&api.RouterConfig{
		Logger:       logger,
		AuthToken:    cfg.AuthToken,
		Registry:     registry,
		Docker:       dockerClient,
		Manager:      manager,
		Poller:       poller,
		Runner:       runner,
		ModelStore:   modelStore,
		PairedRunner: manager,
	})

	srv := &http.Server{
		Handler:           router,
		ReadHeaderTimeout: 10 * time.Second,
	}

	// Start server
	errCh := make(chan error, 1)
	go func() {
		ln, listenErr := net.Listen("tcp", cfg.ListenAddr)
		if listenErr != nil {
			errCh <- listenErr
			return
		}
		logger.Info("server listening", "addr", ln.Addr().String())
		if serveErr := srv.Serve(ln); serveErr != nil && !errors.Is(serveErr, http.ErrServerClosed) {
			errCh <- serveErr
		}
		close(errCh)
	}()

	// Wait for shutdown signal or server error
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
		return err
	}

	logger.Info("bonnie stopped")
	return nil
}

// dockerChecker implements health.Checker for Docker socket connectivity.
type dockerChecker struct {
	client *dockerclient.Client
}

func (d *dockerChecker) Name() string { return "docker" }

func (d *dockerChecker) Check(ctx context.Context) error {
	_, err := d.client.Ping(ctx)
	return err
}
