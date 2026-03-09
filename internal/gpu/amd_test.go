package gpu

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestParseAMDOutput_SingleGPU(t *testing.T) {
	t.Parallel()

	data := []byte(`{
		"card0": {
			"Card Series": "Radeon RX 7900 XTX",
			"GPU use (%)": "15.0",
			"VRAM Total Memory (B)": "25769803776",
			"VRAM Total Used Memory (B)": "1073741824"
		}
	}`)

	gpus, err := parseAMDOutput(data)
	require.NoError(t, err)
	require.Len(t, gpus, 1)

	assert.Equal(t, "Radeon RX 7900 XTX", gpus[0].Name)
	assert.Equal(t, VendorAMD, gpus[0].Vendor)
	assert.Equal(t, uint64(24576), gpus[0].MemoryTotal) // 25769803776 / 1048576
	assert.Equal(t, uint64(23552), gpus[0].MemoryFree)  // (25769803776 - 1073741824) / 1048576
	assert.Equal(t, 15, gpus[0].Utilization)
}

func TestParseAMDOutput_InvalidJSON(t *testing.T) {
	t.Parallel()

	_, err := parseAMDOutput([]byte("not json"))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "invalid JSON")
}

func TestParseAMDOutput_NoCards(t *testing.T) {
	t.Parallel()

	_, err := parseAMDOutput([]byte(`{"system": {}}`))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "no GPUs found")
}

func TestParseAMDOutput_ZeroUtilization(t *testing.T) {
	t.Parallel()

	data := []byte(`{
		"card0": {
			"Card Series": "Radeon RX 7900 XTX",
			"GPU use (%)": "0.0",
			"VRAM Total Memory (B)": "25769803776",
			"VRAM Total Used Memory (B)": "0"
		}
	}`)

	gpus, err := parseAMDOutput(data)
	require.NoError(t, err)
	assert.Equal(t, 0, gpus[0].Utilization)
}
