// Package gpu provides GPU detection and metrics polling for BONNIE.
package gpu

import "time"

// Vendor identifies the GPU manufacturer.
type Vendor string

// Supported GPU vendors.
const (
	VendorNVIDIA  Vendor = "nvidia"
	VendorAMD     Vendor = "amd"
	VendorIntel   Vendor = "intel"
	VendorUnknown Vendor = "none"
)

// Info describes a single GPU detected on the host.
type Info struct {
	Index       int    `json:"index"`
	Name        string `json:"name"`
	Vendor      Vendor `json:"vendor"`
	MemoryTotal uint64 `json:"memory_total_mib"`
	MemoryFree  uint64 `json:"memory_free_mib"`
	Utilization int    `json:"utilization_percent"`
}

// Snapshot is a point-in-time view of all GPUs on the host.
type Snapshot struct {
	Vendor    Vendor    `json:"vendor"`
	GPUs      []Info    `json:"gpus"`
	Timestamp time.Time `json:"timestamp"`
}
