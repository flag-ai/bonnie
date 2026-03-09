package gpu

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestParseNVIDIAOutput_SingleGPU(t *testing.T) {
	t.Parallel()

	output := "0, NVIDIA GeForce RTX 4090, 24564, 22000, 35\n"

	gpus, err := parseNVIDIAOutput(output)
	require.NoError(t, err)
	require.Len(t, gpus, 1)

	assert.Equal(t, 0, gpus[0].Index)
	assert.Equal(t, "NVIDIA GeForce RTX 4090", gpus[0].Name)
	assert.Equal(t, VendorNVIDIA, gpus[0].Vendor)
	assert.Equal(t, uint64(24564), gpus[0].MemoryTotal)
	assert.Equal(t, uint64(22000), gpus[0].MemoryFree)
	assert.Equal(t, 35, gpus[0].Utilization)
}

func TestParseNVIDIAOutput_MultiGPU(t *testing.T) {
	t.Parallel()

	output := `0, NVIDIA A100-SXM4-80GB, 81920, 80000, 10
1, NVIDIA A100-SXM4-80GB, 81920, 75000, 45
`

	gpus, err := parseNVIDIAOutput(output)
	require.NoError(t, err)
	require.Len(t, gpus, 2)

	assert.Equal(t, 0, gpus[0].Index)
	assert.Equal(t, 1, gpus[1].Index)
	assert.Equal(t, uint64(80000), gpus[0].MemoryFree)
	assert.Equal(t, uint64(75000), gpus[1].MemoryFree)
}

func TestParseNVIDIAOutput_Empty(t *testing.T) {
	t.Parallel()

	_, err := parseNVIDIAOutput("")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "no GPUs found")
}

func TestParseNVIDIAOutput_InvalidFields(t *testing.T) {
	t.Parallel()

	_, err := parseNVIDIAOutput("0, GPU Name, bad")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "expected 5 fields")
}

func TestParseNVIDIAOutput_InvalidIndex(t *testing.T) {
	t.Parallel()

	_, err := parseNVIDIAOutput("x, GPU, 1000, 500, 10")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "invalid index")
}

func TestParseNVIDIAOutput_InvalidMemory(t *testing.T) {
	t.Parallel()

	_, err := parseNVIDIAOutput("0, GPU, bad, 500, 10")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "invalid memory.total")
}

func TestParseNVIDIAOutput_InvalidUtilization(t *testing.T) {
	t.Parallel()

	_, err := parseNVIDIAOutput("0, GPU, 1000, 500, bad")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "invalid utilization")
}
