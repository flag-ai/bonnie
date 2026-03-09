package gpu

import (
	"context"
	"fmt"
	"strconv"
	"strings"
)

// detectIntel parses xpu-smi output into Info slices.
func detectIntel(ctx context.Context, runner CommandRunner) ([]Info, error) {
	out, err := runner.Run(ctx, "xpu-smi", "discovery", "--dump", "1,3,6,7")
	if err != nil {
		return nil, fmt.Errorf("xpu-smi: %w", err)
	}

	return parseIntelOutput(string(out))
}

// parseIntelOutput parses xpu-smi CSV dump output into Info slices.
// Expected columns: DeviceID, DeviceName, MemoryPhysicalSize, MemoryFree
func parseIntelOutput(output string) ([]Info, error) {
	var gpus []Info

	lines := strings.Split(strings.TrimSpace(output), "\n")
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "DeviceID") {
			continue
		}

		fields := strings.Split(line, ",")
		if len(fields) < 4 {
			return nil, fmt.Errorf("xpu-smi: expected at least 4 fields, got %d: %q", len(fields), line)
		}

		index, err := strconv.Atoi(strings.TrimSpace(fields[0]))
		if err != nil {
			return nil, fmt.Errorf("xpu-smi: invalid DeviceID %q: %w", fields[0], err)
		}

		// xpu-smi reports memory in MiB
		memTotal, err := strconv.ParseUint(strings.TrimSpace(fields[2]), 10, 64)
		if err != nil {
			return nil, fmt.Errorf("xpu-smi: invalid MemoryPhysicalSize %q: %w", fields[2], err)
		}

		memFree, err := strconv.ParseUint(strings.TrimSpace(fields[3]), 10, 64)
		if err != nil {
			return nil, fmt.Errorf("xpu-smi: invalid MemoryFree %q: %w", fields[3], err)
		}

		gpus = append(gpus, Info{
			Index:       index,
			Name:        strings.TrimSpace(fields[1]),
			Vendor:      VendorIntel,
			MemoryTotal: memTotal,
			MemoryFree:  memFree,
			Utilization: 0, // xpu-smi discovery doesn't provide utilization
		})
	}

	if len(gpus) == 0 {
		return nil, fmt.Errorf("xpu-smi: no GPUs found in output")
	}

	return gpus, nil
}
