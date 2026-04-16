// Package api provides the HTTP API for BONNIE.
package api

import (
	"log/slog"

	"github.com/go-chi/chi/v5"

	"github.com/flag-ai/bonnie/internal/api/handlers"
	"github.com/flag-ai/bonnie/internal/api/middleware"
	"github.com/flag-ai/bonnie/internal/container"
	"github.com/flag-ai/bonnie/internal/gpu"
	"github.com/flag-ai/commons/health"
)

// RouterConfig holds dependencies for building the API router.
type RouterConfig struct {
	Logger       *slog.Logger
	AuthToken    string
	Registry     *health.Registry
	Docker       container.DockerClient
	Manager      *container.Manager
	Poller       *gpu.Poller
	Runner       gpu.CommandRunner
	ModelStore   handlers.ModelStore
	PairedRunner handlers.PairedRunner
}

// NewRouter creates a Chi router with all BONNIE API routes and middleware.
func NewRouter(cfg *RouterConfig) *chi.Mux {
	r := chi.NewRouter()

	// Global middleware
	r.Use(middleware.Recovery(cfg.Logger))
	r.Use(middleware.Logging(cfg.Logger))

	// Health endpoints (no auth)
	healthH := handlers.NewHealthHandler(cfg.Registry, cfg.Docker, cfg.Logger)
	r.Get("/health", healthH.Health)
	r.Get("/ready", healthH.Ready)

	// GPU metrics endpoint (no auth, for Prometheus scraping)
	gpuH := handlers.NewGPUHandler(cfg.Poller, cfg.Logger)
	r.Get("/metrics", gpuH.Metrics)

	// Authenticated API routes
	r.Group(func(r chi.Router) {
		r.Use(middleware.Auth(cfg.AuthToken, "/health", "/ready", "/metrics"))

		r.Route("/api/v1", func(r chi.Router) {
			// System
			systemH := handlers.NewSystemHandler(cfg.Runner, cfg.Logger)
			r.Get("/system/info", systemH.Info)

			// GPU
			r.Get("/gpu/status", gpuH.Status)
			r.Get("/gpu/metrics", gpuH.Metrics)

			// Containers
			containerH := handlers.NewContainerHandler(cfg.Manager, cfg.Docker, cfg.Logger)
			r.Get("/containers", containerH.List)
			r.Post("/containers", containerH.Create)
			r.Get("/containers/{id}", containerH.Inspect)
			r.Post("/containers/{id}/start", containerH.Start)
			r.Post("/containers/{id}/stop", containerH.Stop)
			r.Post("/containers/{id}/restart", containerH.Restart)
			r.Delete("/containers/{id}", containerH.Remove)
			r.Get("/containers/{id}/logs", containerH.Logs)

			// Exec
			execH := handlers.NewExecHandler(cfg.Runner, cfg.Logger)
			r.Post("/exec", execH.Exec)

			// Models (model-storage endpoints)
			if cfg.ModelStore != nil {
				modelsH := handlers.NewModelsHandler(cfg.ModelStore, cfg.Logger)
				r.Route("/models", func(r chi.Router) {
					r.Get("/", modelsH.List)
					r.Post("/fetch", modelsH.Fetch)
					r.Delete("/{id}", modelsH.Delete)
				})
			}

			// Benchmark (paired engine+benchmark run)
			if cfg.PairedRunner != nil {
				benchmarkH := handlers.NewBenchmarkHandler(cfg.PairedRunner, cfg.Logger)
				r.Post("/benchmark", benchmarkH.Run)
			}
		})
	})

	return r
}
