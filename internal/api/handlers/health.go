// Package handlers provides HTTP handlers for the BONNIE API.
package handlers

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"

	"github.com/flag-ai/commons/health"

	"github.com/flag-ai/bonnie/internal/container"
)

// HealthHandler serves health and readiness endpoints.
type HealthHandler struct {
	registry *health.Registry
	docker   container.DockerClient
	logger   *slog.Logger
}

// NewHealthHandler creates a HealthHandler.
func NewHealthHandler(registry *health.Registry, docker container.DockerClient, logger *slog.Logger) *HealthHandler {
	return &HealthHandler{
		registry: registry,
		docker:   docker,
		logger:   logger,
	}
}

// Health returns the health check report from the registry.
func (h *HealthHandler) Health(w http.ResponseWriter, r *http.Request) {
	report := h.registry.RunAll(r.Context())

	w.Header().Set("Content-Type", "application/json")
	if !report.Healthy {
		w.WriteHeader(http.StatusServiceUnavailable)
	}
	if err := json.NewEncoder(w).Encode(report); err != nil {
		h.logger.Error("failed to encode health report", "error", err)
	}
}

// Ready checks that the Docker socket is reachable.
func (h *HealthHandler) Ready(w http.ResponseWriter, _ *http.Request) {
	_, err := h.docker.Ping(context.Background())

	w.Header().Set("Content-Type", "application/json")
	if err != nil {
		h.logger.Error("docker not ready", "error", err)
		w.WriteHeader(http.StatusServiceUnavailable)
		if encErr := json.NewEncoder(w).Encode(map[string]any{
			"ready": false,
		}); encErr != nil {
			h.logger.Error("failed to encode ready response", "error", encErr)
		}
		return
	}

	if err := json.NewEncoder(w).Encode(map[string]any{
		"ready": true,
	}); err != nil {
		h.logger.Error("failed to encode ready response", "error", err)
	}
}
