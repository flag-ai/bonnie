package container

import (
	containertypes "github.com/docker/docker/api/types/container"

	"github.com/flag-ai/bonnie/internal/gpu"
)

// InjectGPU modifies the host config to enable GPU access based on the detected vendor.
func InjectGPU(hostConfig *containertypes.HostConfig, vendor gpu.Vendor) {
	switch vendor {
	case gpu.VendorNVIDIA:
		injectNVIDIA(hostConfig)
	case gpu.VendorAMD:
		injectAMD(hostConfig)
	case gpu.VendorIntel:
		injectIntel(hostConfig)
	}
}

// injectNVIDIA adds NVIDIA GPU access via DeviceRequests (NVIDIA Container Toolkit).
func injectNVIDIA(hostConfig *containertypes.HostConfig) {
	hostConfig.DeviceRequests = append(hostConfig.DeviceRequests, containertypes.DeviceRequest{
		Count:        -1, // All GPUs
		Capabilities: [][]string{{"gpu"}},
	})
}

// injectAMD adds AMD GPU access via device mounts (/dev/kfd, /dev/dri).
func injectAMD(hostConfig *containertypes.HostConfig) {
	hostConfig.Devices = append(hostConfig.Devices,
		containertypes.DeviceMapping{
			PathOnHost:        "/dev/kfd",
			PathInContainer:   "/dev/kfd",
			CgroupPermissions: "rwm",
		},
		containertypes.DeviceMapping{
			PathOnHost:        "/dev/dri",
			PathInContainer:   "/dev/dri",
			CgroupPermissions: "rwm",
		},
	)
	hostConfig.SecurityOpt = append(hostConfig.SecurityOpt, "seccomp=unconfined")
	hostConfig.GroupAdd = append(hostConfig.GroupAdd, "video", "render")
}

// injectIntel adds Intel GPU access via device mount (/dev/dri).
func injectIntel(hostConfig *containertypes.HostConfig) {
	hostConfig.Devices = append(hostConfig.Devices,
		containertypes.DeviceMapping{
			PathOnHost:        "/dev/dri",
			PathInContainer:   "/dev/dri",
			CgroupPermissions: "rwm",
		},
	)
	hostConfig.GroupAdd = append(hostConfig.GroupAdd, "video", "render")
}
