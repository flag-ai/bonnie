package gpu

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// mockRunner implements CommandRunner for testing.
type mockRunner struct {
	outputs map[string]mockOutput
}

type mockOutput struct {
	data []byte
	err  error
}

func (m *mockRunner) Run(_ context.Context, name string, _ ...string) ([]byte, error) {
	if out, ok := m.outputs[name]; ok {
		return out.data, out.err
	}
	return nil, fmt.Errorf("command not found: %s", name)
}

func newTestLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

func TestDetect_NVIDIA(t *testing.T) {
	t.Parallel()

	runner := &mockRunner{outputs: map[string]mockOutput{
		"nvidia-smi": {data: []byte("0, NVIDIA RTX 4090, 24564, 22000, 35\n")},
	}}

	detector := NewDetector(runner, newTestLogger())
	snap := detector.Detect(context.Background())

	assert.Equal(t, VendorNVIDIA, snap.Vendor)
	require.Len(t, snap.GPUs, 1)
	assert.Equal(t, "NVIDIA RTX 4090", snap.GPUs[0].Name)
}

func TestDetect_AMD_Fallback(t *testing.T) {
	t.Parallel()

	runner := &mockRunner{outputs: map[string]mockOutput{
		"nvidia-smi": {err: fmt.Errorf("not found")},
		"rocm-smi": {data: []byte(`{
			"card0": {
				"Card Series": "Radeon RX 7900 XTX",
				"GPU use (%)": "0.0",
				"VRAM Total Memory (B)": "25769803776",
				"VRAM Total Used Memory (B)": "0"
			}
		}`)},
	}}

	detector := NewDetector(runner, newTestLogger())
	snap := detector.Detect(context.Background())

	assert.Equal(t, VendorAMD, snap.Vendor)
	require.Len(t, snap.GPUs, 1)
}

func TestDetect_Intel_Fallback(t *testing.T) {
	t.Parallel()

	runner := &mockRunner{outputs: map[string]mockOutput{
		"nvidia-smi": {err: fmt.Errorf("not found")},
		"rocm-smi":   {err: fmt.Errorf("not found")},
		"xpu-smi":    {data: []byte("0,Intel Arc A770,16384,14000\n")},
	}}

	detector := NewDetector(runner, newTestLogger())
	snap := detector.Detect(context.Background())

	assert.Equal(t, VendorIntel, snap.Vendor)
	require.Len(t, snap.GPUs, 1)
}

func TestDetect_CPU_Fallback(t *testing.T) {
	t.Parallel()

	runner := &mockRunner{outputs: map[string]mockOutput{
		"nvidia-smi": {err: fmt.Errorf("not found")},
		"rocm-smi":   {err: fmt.Errorf("not found")},
		"xpu-smi":    {err: fmt.Errorf("not found")},
	}}

	detector := NewDetector(runner, newTestLogger())
	snap := detector.Detect(context.Background())

	assert.Equal(t, VendorUnknown, snap.Vendor)
	assert.Empty(t, snap.GPUs)
}
