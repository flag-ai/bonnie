package handlers

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"

	"github.com/flag-ai/bonnie/internal/gpu"
)

// ExecRequest describes a command to execute on the host.
type ExecRequest struct {
	Command string   `json:"command"`
	Args    []string `json:"args,omitempty"`
}

// ExecHandler runs commands on the host and streams output via SSE.
type ExecHandler struct {
	runner gpu.CommandRunner
	logger *slog.Logger
}

// NewExecHandler creates an ExecHandler.
func NewExecHandler(runner gpu.CommandRunner, logger *slog.Logger) *ExecHandler {
	return &ExecHandler{
		runner: runner,
		logger: logger,
	}
}

// Exec runs a command and streams output via SSE.
func (h *ExecHandler) Exec(w http.ResponseWriter, r *http.Request) {
	var req ExecRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	if req.Command == "" {
		writeError(w, http.StatusBadRequest, "command is required")
		return
	}

	flusher, ok := w.(http.Flusher)
	if !ok {
		writeError(w, http.StatusInternalServerError, "streaming not supported")
		return
	}

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	flusher.Flush()

	h.logger.Info("exec command", "command", req.Command, "args", req.Args)

	out, err := h.runner.Run(r.Context(), req.Command, req.Args...)
	if err != nil {
		_, _ = fmt.Fprintf(w, "data: {\"error\": %q}\n\n", err.Error())
		flusher.Flush()
		return
	}

	_, _ = fmt.Fprintf(w, "data: %s\n\n", out)
	flusher.Flush()

	_, _ = fmt.Fprintf(w, "event: done\ndata: {\"exit_code\": 0}\n\n")
	flusher.Flush()
}
