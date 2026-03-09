package container

import (
	"testing"

	containertypes "github.com/docker/docker/api/types/container"
	"github.com/stretchr/testify/assert"

	"github.com/flag-ai/bonnie/internal/gpu"
)

func TestInjectGPU_NVIDIA(t *testing.T) {
	t.Parallel()

	hc := &containertypes.HostConfig{}
	InjectGPU(hc, gpu.VendorNVIDIA)

	assert.Len(t, hc.DeviceRequests, 1)
	assert.Equal(t, -1, hc.DeviceRequests[0].Count)
	assert.Equal(t, [][]string{{"gpu"}}, hc.DeviceRequests[0].Capabilities)
}

func TestInjectGPU_AMD(t *testing.T) {
	t.Parallel()

	hc := &containertypes.HostConfig{}
	InjectGPU(hc, gpu.VendorAMD)

	assert.Len(t, hc.Devices, 2)
	assert.Equal(t, "/dev/kfd", hc.Devices[0].PathOnHost)
	assert.Equal(t, "/dev/dri", hc.Devices[1].PathOnHost)
	assert.Contains(t, hc.SecurityOpt, "seccomp=unconfined")
	assert.Contains(t, hc.GroupAdd, "video")
	assert.Contains(t, hc.GroupAdd, "render")
}

func TestInjectGPU_Intel(t *testing.T) {
	t.Parallel()

	hc := &containertypes.HostConfig{}
	InjectGPU(hc, gpu.VendorIntel)

	assert.Len(t, hc.Devices, 1)
	assert.Equal(t, "/dev/dri", hc.Devices[0].PathOnHost)
	assert.Contains(t, hc.GroupAdd, "video")
	assert.Contains(t, hc.GroupAdd, "render")
}

func TestInjectGPU_Unknown(t *testing.T) {
	t.Parallel()

	hc := &containertypes.HostConfig{}
	InjectGPU(hc, gpu.VendorUnknown)

	assert.Empty(t, hc.Devices)
	assert.Empty(t, hc.DeviceRequests)
}
