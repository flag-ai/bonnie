package handlers

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"

	"github.com/go-chi/chi/v5"

	"github.com/flag-ai/bonnie/internal/container"
)

// ContainerHandler serves container management endpoints.
type ContainerHandler struct {
	manager *container.Manager
	docker  container.DockerClient
	logger  *slog.Logger
}

// NewContainerHandler creates a ContainerHandler.
func NewContainerHandler(manager *container.Manager, docker container.DockerClient, logger *slog.Logger) *ContainerHandler {
	return &ContainerHandler{
		manager: manager,
		docker:  docker,
		logger:  logger,
	}
}

// List returns all containers.
func (h *ContainerHandler) List(w http.ResponseWriter, r *http.Request) {
	containers, err := h.manager.List(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(containers); err != nil {
		h.logger.Error("failed to encode containers", "error", err)
	}
}

// Create creates a new container.
func (h *ContainerHandler) Create(w http.ResponseWriter, r *http.Request) {
	var req container.CreateRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	if req.Image == "" {
		writeError(w, http.StatusBadRequest, "image is required")
		return
	}

	id, err := h.manager.Create(r.Context(), req)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	if err := json.NewEncoder(w).Encode(map[string]string{"id": id}); err != nil {
		h.logger.Error("failed to encode create response", "error", err)
	}
}

// Inspect returns detailed container information.
func (h *ContainerHandler) Inspect(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")

	info, err := h.manager.Inspect(r.Context(), id)
	if err != nil {
		writeError(w, http.StatusNotFound, err.Error())
		return
	}

	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(info); err != nil {
		h.logger.Error("failed to encode inspect response", "error", err)
	}
}

// Start starts a container.
func (h *ContainerHandler) Start(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")

	if err := h.manager.Start(r.Context(), id); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(map[string]string{"status": "started"}); err != nil {
		h.logger.Error("failed to encode response", "error", err)
	}
}

// Stop stops a container.
func (h *ContainerHandler) Stop(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")

	if err := h.manager.Stop(r.Context(), id); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(map[string]string{"status": "stopped"}); err != nil {
		h.logger.Error("failed to encode response", "error", err)
	}
}

// Restart restarts a container.
func (h *ContainerHandler) Restart(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")

	if err := h.manager.Restart(r.Context(), id); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(map[string]string{"status": "restarted"}); err != nil {
		h.logger.Error("failed to encode response", "error", err)
	}
}

// Remove removes a container.
func (h *ContainerHandler) Remove(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")

	if err := h.manager.Remove(r.Context(), id, true); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

// Logs streams container logs via SSE.
func (h *ContainerHandler) Logs(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")

	flusher, ok := w.(http.Flusher)
	if !ok {
		writeError(w, http.StatusInternalServerError, "streaming not supported")
		return
	}

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	flusher.Flush()

	sseWriter := &sseWriter{w: w, flusher: flusher}
	if err := container.LogStream(r.Context(), h.docker, id, true, sseWriter); err != nil {
		h.logger.Debug("log stream ended", "id", id, "error", err)
	}
}

// sseWriter wraps a ResponseWriter to format output as SSE data events.
type sseWriter struct {
	w       http.ResponseWriter
	flusher http.Flusher
}

func (s *sseWriter) Write(p []byte) (int, error) {
	if _, err := fmt.Fprintf(s.w, "data: %s\n\n", p); err != nil {
		return 0, err
	}
	s.flusher.Flush()
	return len(p), nil
}

func writeError(w http.ResponseWriter, code int, msg string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(map[string]string{"error": msg})
}
