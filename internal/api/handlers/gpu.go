package handlers

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"

	"github.com/flag-ai/bonnie/internal/gpu"
)

// GPUHandler serves GPU status and metrics endpoints.
type GPUHandler struct {
	poller *gpu.Poller
	logger *slog.Logger
}

// NewGPUHandler creates a GPUHandler.
func NewGPUHandler(poller *gpu.Poller, logger *slog.Logger) *GPUHandler {
	return &GPUHandler{poller: poller, logger: logger}
}

// Status returns the latest GPU snapshot as JSON.
func (h *GPUHandler) Status(w http.ResponseWriter, _ *http.Request) {
	snap := h.poller.Latest()

	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(snap); err != nil {
		h.logger.Error("failed to encode gpu status", "error", err)
	}
}

// Metrics returns GPU metrics in Prometheus text exposition format.
func (h *GPUHandler) Metrics(w http.ResponseWriter, _ *http.Request) {
	snap := h.poller.Latest()

	w.Header().Set("Content-Type", "text/plain; version=0.0.4; charset=utf-8")

	writeMetric(w, "# HELP bonnie_gpu_memory_total_mib Total GPU memory in MiB.")
	writeMetric(w, "# TYPE bonnie_gpu_memory_total_mib gauge")
	for _, g := range snap.GPUs {
		writeMetric(w, fmt.Sprintf("bonnie_gpu_memory_total_mib{index=\"%d\",name=%q,vendor=%q} %d",
			g.Index, g.Name, g.Vendor, g.MemoryTotal))
	}

	writeMetric(w, "# HELP bonnie_gpu_memory_free_mib Free GPU memory in MiB.")
	writeMetric(w, "# TYPE bonnie_gpu_memory_free_mib gauge")
	for _, g := range snap.GPUs {
		writeMetric(w, fmt.Sprintf("bonnie_gpu_memory_free_mib{index=\"%d\",name=%q,vendor=%q} %d",
			g.Index, g.Name, g.Vendor, g.MemoryFree))
	}

	writeMetric(w, "# HELP bonnie_gpu_utilization_percent GPU utilization percentage.")
	writeMetric(w, "# TYPE bonnie_gpu_utilization_percent gauge")
	for _, g := range snap.GPUs {
		writeMetric(w, fmt.Sprintf("bonnie_gpu_utilization_percent{index=\"%d\",name=%q,vendor=%q} %d",
			g.Index, g.Name, g.Vendor, g.Utilization))
	}

	writeMetric(w, "# HELP bonnie_gpu_count Total number of GPUs detected.")
	writeMetric(w, "# TYPE bonnie_gpu_count gauge")
	writeMetric(w, fmt.Sprintf("bonnie_gpu_count{vendor=%q} %d", snap.Vendor, len(snap.GPUs)))
}

func writeMetric(w http.ResponseWriter, line string) {
	_, _ = fmt.Fprintln(w, line)
}
