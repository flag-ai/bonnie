package handlers

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"time"

	"github.com/flag-ai/bonnie/internal/container"
)

// PairedRunner is the subset of *container.Manager used by BenchmarkHandler.
// Defined as an interface so handler tests can plug in a fake runner.
type PairedRunner interface {
	PairedRun(ctx context.Context, spec container.PairedRunSpec, events chan<- container.PairedRunEvent) error
}

// BenchmarkHandler runs paired engine+benchmark containers and streams
// progress events via SSE.
type BenchmarkHandler struct {
	runner PairedRunner
	logger *slog.Logger
}

// NewBenchmarkHandler creates a BenchmarkHandler.
func NewBenchmarkHandler(runner PairedRunner, logger *slog.Logger) *BenchmarkHandler {
	return &BenchmarkHandler{runner: runner, logger: logger}
}

// defaultBenchmarkTimeout caps a single paired run when the client didn't
// specify one.
const defaultBenchmarkTimeout = time.Hour

// Run handles POST /api/v1/benchmark. It parses the PairedRunSpec, streams
// events as SSE `data: <json>\n\n` frames, and closes the connection when the
// run ends.
func (h *BenchmarkHandler) Run(w http.ResponseWriter, r *http.Request) {
	var spec container.PairedRunSpec
	if err := json.NewDecoder(r.Body).Decode(&spec); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if spec.RunID == "" {
		writeError(w, http.StatusBadRequest, "run_id is required")
		return
	}
	if err := container.ValidateRunID(spec.RunID); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if spec.Engine.Image == "" {
		writeError(w, http.StatusBadRequest, "engine.image is required")
		return
	}
	if spec.Benchmark.Image == "" {
		writeError(w, http.StatusBadRequest, "benchmark.image is required")
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

	// Apply run timeout.
	timeout := defaultBenchmarkTimeout
	if spec.TimeoutSeconds > 0 {
		timeout = time.Duration(spec.TimeoutSeconds) * time.Second
	}
	runCtx, cancel := context.WithTimeout(r.Context(), timeout)
	defer cancel()

	events := make(chan container.PairedRunEvent, 64)

	runErr := make(chan error, 1)
	go func() {
		runErr <- h.runner.PairedRun(runCtx, spec, events)
	}()

	encoder := json.NewEncoder(w)
	for {
		select {
		case <-r.Context().Done():
			// Client disconnected; cancel the run and drain quickly.
			cancel()
			// Wait briefly for the run goroutine to clean up, but don't block
			// forever — the defer in PairedRun handles cleanup.
			<-runErr
			return
		case ev, ok := <-events:
			if !ok {
				// Channel closed — PairedRun returned.
				err := <-runErr
				if err != nil {
					h.logger.Debug("paired run finished with error",
						"run_id", spec.RunID, "error", err)
				}
				return
			}
			if _, err := fmt.Fprint(w, "data: "); err != nil {
				return
			}
			if err := encoder.Encode(ev); err != nil {
				h.logger.Debug("sse encode", "error", err)
				return
			}
			// Encoder adds a trailing newline; SSE frames end in blank line,
			// so add one more.
			if _, err := fmt.Fprint(w, "\n"); err != nil {
				return
			}
			flusher.Flush()
		}
	}
}
