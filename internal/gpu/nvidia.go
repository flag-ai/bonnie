package gpu

import (
	"context"
	"fmt"
	"strconv"
	"strings"
)

// nvidiaQuery is the nvidia-smi CSV format query.
var nvidiaQuery = []string{
	"--query-gpu=index,name,memory.total,memory.free,utilization.gpu",
	"--format=csv,noheader,nounits",
}

// detectNVIDIA parses nvidia-smi output into Info slices.
func detectNVIDIA(ctx context.Context, runner CommandRunner) ([]Info, error) {
	out, err := runner.Run(ctx, "nvidia-smi", nvidiaQuery...)
	if err != nil {
		return nil, fmt.Errorf("nvidia-smi: %w", err)
	}

	return parseNVIDIAOutput(string(out))
}

// parseNVIDIAOutput parses CSV lines from nvidia-smi into Info slices.
func parseNVIDIAOutput(output string) ([]Info, error) {
	var gpus []Info

	for _, line := range strings.Split(strings.TrimSpace(output), "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}

		fields := strings.Split(line, ", ")
		if len(fields) != 5 {
			return nil, fmt.Errorf("nvidia-smi: expected 5 fields, got %d: %q", len(fields), line)
		}

		index, err := strconv.Atoi(strings.TrimSpace(fields[0]))
		if err != nil {
			return nil, fmt.Errorf("nvidia-smi: invalid index %q: %w", fields[0], err)
		}

		memTotal, err := strconv.ParseUint(strings.TrimSpace(fields[2]), 10, 64)
		if err != nil {
			return nil, fmt.Errorf("nvidia-smi: invalid memory.total %q: %w", fields[2], err)
		}

		memFree, err := strconv.ParseUint(strings.TrimSpace(fields[3]), 10, 64)
		if err != nil {
			return nil, fmt.Errorf("nvidia-smi: invalid memory.free %q: %w", fields[3], err)
		}

		util, err := strconv.Atoi(strings.TrimSpace(fields[4]))
		if err != nil {
			return nil, fmt.Errorf("nvidia-smi: invalid utilization %q: %w", fields[4], err)
		}

		gpus = append(gpus, Info{
			Index:       index,
			Name:        strings.TrimSpace(fields[1]),
			Vendor:      VendorNVIDIA,
			MemoryTotal: memTotal,
			MemoryFree:  memFree,
			Utilization: util,
		})
	}

	if len(gpus) == 0 {
		return nil, fmt.Errorf("nvidia-smi: no GPUs found in output")
	}

	return gpus, nil
}
