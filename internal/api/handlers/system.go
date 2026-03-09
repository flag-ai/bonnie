package handlers

import (
	"encoding/json"
	"log/slog"
	"net/http"

	"github.com/flag-ai/bonnie/internal/gpu"
	"github.com/flag-ai/bonnie/internal/system"
)

// SystemHandler serves system information endpoints.
type SystemHandler struct {
	runner gpu.CommandRunner
	logger *slog.Logger
}

// NewSystemHandler creates a SystemHandler.
func NewSystemHandler(runner gpu.CommandRunner, logger *slog.Logger) *SystemHandler {
	return &SystemHandler{runner: runner, logger: logger}
}

// Info returns system information.
func (h *SystemHandler) Info(w http.ResponseWriter, r *http.Request) {
	info, err := system.Collect(r.Context(), h.runner)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	disk, _ := system.CollectDisk(r.Context(), h.runner)

	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(map[string]any{
		"system": info,
		"disk":   disk,
	}); err != nil {
		h.logger.Error("failed to encode system info", "error", err)
	}
}
