package handlers_test

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/flag-ai/bonnie/internal/api/handlers"
	"github.com/flag-ai/bonnie/internal/gpu"
)

type mockRunner struct {
	outputs map[string]struct {
		data []byte
		err  error
	}
}

func (m *mockRunner) Run(_ context.Context, name string, _ ...string) ([]byte, error) {
	if out, ok := m.outputs[name]; ok {
		return out.data, out.err
	}
	return nil, fmt.Errorf("command not found: %s", name)
}

func TestGPUStatus(t *testing.T) {
	t.Parallel()

	runner := &mockRunner{outputs: map[string]struct {
		data []byte
		err  error
	}{
		"nvidia-smi": {data: []byte("0, Test GPU, 8192, 7000, 25\n")},
	}}

	detector := gpu.NewDetector(runner, newTestLogger())
	poller := gpu.NewPoller(detector, time.Hour, newTestLogger())

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	poller.Start(ctx)

	h := handlers.NewGPUHandler(poller, newTestLogger())

	req := httptest.NewRequest(http.MethodGet, "/api/v1/gpu/status", http.NoBody)
	rec := httptest.NewRecorder()

	h.Status(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)

	var snap gpu.Snapshot
	err := json.NewDecoder(rec.Body).Decode(&snap)
	require.NoError(t, err)
	assert.Equal(t, gpu.VendorNVIDIA, snap.Vendor)
	require.Len(t, snap.GPUs, 1)
	assert.Equal(t, "Test GPU", snap.GPUs[0].Name)
}

func TestGPUMetrics(t *testing.T) {
	t.Parallel()

	runner := &mockRunner{outputs: map[string]struct {
		data []byte
		err  error
	}{
		"nvidia-smi": {data: []byte("0, Test GPU, 8192, 7000, 25\n")},
	}}

	detector := gpu.NewDetector(runner, newTestLogger())
	poller := gpu.NewPoller(detector, time.Hour, newTestLogger())

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	poller.Start(ctx)

	h := handlers.NewGPUHandler(poller, newTestLogger())

	req := httptest.NewRequest(http.MethodGet, "/api/v1/gpu/metrics", http.NoBody)
	rec := httptest.NewRecorder()

	h.Metrics(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)
	body := rec.Body.String()
	assert.Contains(t, body, "bonnie_gpu_memory_total_mib")
	assert.Contains(t, body, "bonnie_gpu_memory_free_mib")
	assert.Contains(t, body, "bonnie_gpu_utilization_percent")
	assert.Contains(t, body, "bonnie_gpu_count")
}
