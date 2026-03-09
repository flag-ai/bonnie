package gpu

import (
	"context"
	"log/slog"
	"time"
)

// Detector discovers GPUs on the host by trying vendor-specific tools.
type Detector struct {
	runner CommandRunner
	logger *slog.Logger
}

// NewDetector creates a Detector with the given command runner and logger.
func NewDetector(runner CommandRunner, logger *slog.Logger) *Detector {
	return &Detector{
		runner: runner,
		logger: logger,
	}
}

// Detect tries NVIDIA → AMD → Intel in order. Returns a CPU-only snapshot on fallback.
func (d *Detector) Detect(ctx context.Context) Snapshot {
	// Try NVIDIA first
	gpus, err := detectNVIDIA(ctx, d.runner)
	if err == nil {
		d.logger.Debug("detected NVIDIA GPUs", "count", len(gpus))
		return Snapshot{Vendor: VendorNVIDIA, GPUs: gpus, Timestamp: time.Now()}
	}
	d.logger.Debug("nvidia-smi not available", "error", err)

	// Try AMD
	gpus, err = detectAMD(ctx, d.runner)
	if err == nil {
		d.logger.Debug("detected AMD GPUs", "count", len(gpus))
		return Snapshot{Vendor: VendorAMD, GPUs: gpus, Timestamp: time.Now()}
	}
	d.logger.Debug("rocm-smi not available", "error", err)

	// Try Intel
	gpus, err = detectIntel(ctx, d.runner)
	if err == nil {
		d.logger.Debug("detected Intel GPUs", "count", len(gpus))
		return Snapshot{Vendor: VendorIntel, GPUs: gpus, Timestamp: time.Now()}
	}
	d.logger.Debug("xpu-smi not available", "error", err)

	// CPU-only fallback
	d.logger.Info("no GPU detected, running in CPU-only mode")
	return Snapshot{Vendor: VendorUnknown, GPUs: nil, Timestamp: time.Now()}
}
