package gpu

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strconv"
	"strings"
)

// detectAMD parses rocm-smi JSON output into Info slices.
func detectAMD(ctx context.Context, runner CommandRunner) ([]Info, error) {
	out, err := runner.Run(ctx, "rocm-smi", "--showid", "--showuse", "--showmeminfo", "vram", "--json")
	if err != nil {
		return nil, fmt.Errorf("rocm-smi: %w", err)
	}

	return parseAMDOutput(out)
}

// rocmDevice represents a single GPU entry from rocm-smi JSON output.
type rocmDevice struct {
	CardSeries   string `json:"Card Series"`
	GPUUse       string `json:"GPU use (%)"`
	VRAMTotalMem string `json:"VRAM Total Memory (B)"`
	VRAMTotalUse string `json:"VRAM Total Used Memory (B)"`
}

// parseAMDOutput parses rocm-smi JSON into Info slices.
func parseAMDOutput(data []byte) ([]Info, error) {
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(data, &raw); err != nil {
		return nil, fmt.Errorf("rocm-smi: invalid JSON: %w", err)
	}

	// Sort keys for deterministic GPU index assignment
	var cardKeys []string
	for key := range raw {
		if strings.HasPrefix(key, "card") {
			cardKeys = append(cardKeys, key)
		}
	}
	sort.Strings(cardKeys)

	var gpus []Info
	index := 0

	for _, key := range cardKeys {
		val := raw[key]

		var dev rocmDevice
		if err := json.Unmarshal(val, &dev); err != nil {
			return nil, fmt.Errorf("rocm-smi: failed to parse %s: %w", key, err)
		}

		totalBytes, err := strconv.ParseUint(dev.VRAMTotalMem, 10, 64)
		if err != nil {
			return nil, fmt.Errorf("rocm-smi: invalid VRAM total %q: %w", dev.VRAMTotalMem, err)
		}

		usedBytes, err := strconv.ParseUint(dev.VRAMTotalUse, 10, 64)
		if err != nil {
			return nil, fmt.Errorf("rocm-smi: invalid VRAM used %q: %w", dev.VRAMTotalUse, err)
		}

		totalMiB := totalBytes / (1024 * 1024)
		freeMiB := (totalBytes - usedBytes) / (1024 * 1024)

		util := 0
		if dev.GPUUse != "" {
			// GPU use can have decimal values like "0.0"
			utilFloat, err := strconv.ParseFloat(dev.GPUUse, 64)
			if err != nil {
				return nil, fmt.Errorf("rocm-smi: invalid GPU use %q: %w", dev.GPUUse, err)
			}
			util = int(utilFloat)
		}

		gpus = append(gpus, Info{
			Index:       index,
			Name:        dev.CardSeries,
			Vendor:      VendorAMD,
			MemoryTotal: totalMiB,
			MemoryFree:  freeMiB,
			Utilization: util,
		})
		index++
	}

	if len(gpus) == 0 {
		return nil, fmt.Errorf("rocm-smi: no GPUs found in output")
	}

	return gpus, nil
}
