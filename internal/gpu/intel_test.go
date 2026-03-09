package gpu

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestParseIntelOutput_SingleGPU(t *testing.T) {
	t.Parallel()

	output := `DeviceID,DeviceName,MemoryPhysicalSize,MemoryFree
0,Intel Data Center GPU Max 1550,65536,60000
`

	gpus, err := parseIntelOutput(output)
	require.NoError(t, err)
	require.Len(t, gpus, 1)

	assert.Equal(t, 0, gpus[0].Index)
	assert.Equal(t, "Intel Data Center GPU Max 1550", gpus[0].Name)
	assert.Equal(t, VendorIntel, gpus[0].Vendor)
	assert.Equal(t, uint64(65536), gpus[0].MemoryTotal)
	assert.Equal(t, uint64(60000), gpus[0].MemoryFree)
	assert.Equal(t, 0, gpus[0].Utilization)
}

func TestParseIntelOutput_MultiGPU(t *testing.T) {
	t.Parallel()

	output := `DeviceID,DeviceName,MemoryPhysicalSize,MemoryFree
0,Intel Arc A770,16384,14000
1,Intel Arc A770,16384,12000
`

	gpus, err := parseIntelOutput(output)
	require.NoError(t, err)
	require.Len(t, gpus, 2)

	assert.Equal(t, 0, gpus[0].Index)
	assert.Equal(t, 1, gpus[1].Index)
}

func TestParseIntelOutput_Empty(t *testing.T) {
	t.Parallel()

	_, err := parseIntelOutput("DeviceID,DeviceName,MemoryPhysicalSize,MemoryFree\n")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "no GPUs found")
}

func TestParseIntelOutput_InvalidFields(t *testing.T) {
	t.Parallel()

	_, err := parseIntelOutput("0,GPU")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "expected at least 4 fields")
}

func TestParseIntelOutput_InvalidDeviceID(t *testing.T) {
	t.Parallel()

	_, err := parseIntelOutput("x,GPU,1000,500")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "invalid DeviceID")
}
