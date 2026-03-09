// Package system provides host system information for BONNIE.
package system

import (
	"context"
	"fmt"
	"os"
	"runtime"
	"strconv"
	"strings"

	"github.com/flag-ai/bonnie/internal/gpu"
)

// Info describes the host system.
type Info struct {
	Hostname string `json:"hostname"`
	OS       string `json:"os"`
	Arch     string `json:"arch"`
	Kernel   string `json:"kernel"`
	CPUModel string `json:"cpu_model"`
	CPUCores int    `json:"cpu_cores"`
	MemoryMB uint64 `json:"memory_mb"`
}

// Collect gathers system information from the host.
func Collect(ctx context.Context, runner gpu.CommandRunner) (*Info, error) {
	hostname, _ := os.Hostname()

	info := &Info{
		Hostname: hostname,
		OS:       runtime.GOOS,
		Arch:     runtime.GOARCH,
		CPUCores: runtime.NumCPU(),
	}

	// Kernel version
	if out, err := runner.Run(ctx, "uname", "-r"); err == nil {
		info.Kernel = strings.TrimSpace(string(out))
	}

	// CPU model from /proc/cpuinfo (Linux)
	if runtime.GOOS == "linux" {
		if data, err := os.ReadFile("/proc/cpuinfo"); err == nil {
			info.CPUModel = parseCPUModel(string(data))
		}
		if data, err := os.ReadFile("/proc/meminfo"); err == nil {
			info.MemoryMB = parseMemTotal(string(data))
		}
	}

	// macOS fallback
	if runtime.GOOS == "darwin" {
		if out, err := runner.Run(ctx, "sysctl", "-n", "machdep.cpu.brand_string"); err == nil {
			info.CPUModel = strings.TrimSpace(string(out))
		}
		if out, err := runner.Run(ctx, "sysctl", "-n", "hw.memsize"); err == nil {
			if bytes, err := strconv.ParseUint(strings.TrimSpace(string(out)), 10, 64); err == nil {
				info.MemoryMB = bytes / (1024 * 1024)
			}
		}
	}

	return info, nil
}

// parseCPUModel extracts the first "model name" from /proc/cpuinfo.
func parseCPUModel(data string) string {
	for _, line := range strings.Split(data, "\n") {
		if strings.HasPrefix(line, "model name") {
			parts := strings.SplitN(line, ":", 2)
			if len(parts) == 2 {
				return strings.TrimSpace(parts[1])
			}
		}
	}
	return ""
}

// parseMemTotal extracts MemTotal from /proc/meminfo and returns MiB.
func parseMemTotal(data string) uint64 {
	for _, line := range strings.Split(data, "\n") {
		if strings.HasPrefix(line, "MemTotal:") {
			fields := strings.Fields(line)
			if len(fields) >= 2 {
				kb, err := strconv.ParseUint(fields[1], 10, 64)
				if err != nil {
					return 0
				}
				return kb / 1024
			}
		}
	}
	return 0
}

// DiskUsage reports disk usage for the root filesystem.
type DiskUsage struct {
	TotalGB     float64 `json:"total_gb"`
	UsedGB      float64 `json:"used_gb"`
	AvailableGB float64 `json:"available_gb"`
	UsedPercent string  `json:"used_percent"`
}

// CollectDisk gets disk usage for the root filesystem.
func CollectDisk(ctx context.Context, runner gpu.CommandRunner) (*DiskUsage, error) {
	out, err := runner.Run(ctx, "df", "-BG", "--output=size,used,avail,pcent", "/")
	if err != nil {
		return nil, fmt.Errorf("df: %w", err)
	}

	return parseDFOutput(string(out))
}

// parseDFOutput parses df output with GB units.
func parseDFOutput(output string) (*DiskUsage, error) {
	lines := strings.Split(strings.TrimSpace(output), "\n")
	if len(lines) < 2 {
		return nil, fmt.Errorf("df: unexpected output format")
	}

	fields := strings.Fields(lines[1])
	if len(fields) < 4 {
		return nil, fmt.Errorf("df: expected 4 fields, got %d", len(fields))
	}

	total, _ := strconv.ParseFloat(strings.TrimSuffix(fields[0], "G"), 64)
	used, _ := strconv.ParseFloat(strings.TrimSuffix(fields[1], "G"), 64)
	avail, _ := strconv.ParseFloat(strings.TrimSuffix(fields[2], "G"), 64)

	return &DiskUsage{
		TotalGB:     total,
		UsedGB:      used,
		AvailableGB: avail,
		UsedPercent: strings.TrimSpace(fields[3]),
	}, nil
}
