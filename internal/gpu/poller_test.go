package gpu

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestPoller_InitialDetection(t *testing.T) {
	t.Parallel()

	runner := &mockRunner{outputs: map[string]mockOutput{
		"nvidia-smi": {data: []byte("0, Test GPU, 8192, 7000, 10\n")},
	}}

	detector := NewDetector(runner, newTestLogger())
	poller := NewPoller(detector, time.Hour, newTestLogger())

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	poller.Start(ctx)

	snap := poller.Latest()
	assert.Equal(t, VendorNVIDIA, snap.Vendor)
	require.Len(t, snap.GPUs, 1)
}

func TestPoller_ContextCancellation(t *testing.T) {
	t.Parallel()

	runner := &mockRunner{outputs: map[string]mockOutput{
		"nvidia-smi": {err: fmt.Errorf("not found")},
		"rocm-smi":   {err: fmt.Errorf("not found")},
		"xpu-smi":    {err: fmt.Errorf("not found")},
	}}

	detector := NewDetector(runner, newTestLogger())
	poller := NewPoller(detector, 10*time.Millisecond, newTestLogger())

	ctx, cancel := context.WithCancel(context.Background())
	poller.Start(ctx)

	// Let it run a few polls
	time.Sleep(50 * time.Millisecond)
	cancel()

	// Should still return latest snapshot after cancel
	snap := poller.Latest()
	assert.Equal(t, VendorUnknown, snap.Vendor)
}

func TestPoller_Subscribe(t *testing.T) {
	t.Parallel()

	runner := &mockRunner{outputs: map[string]mockOutput{
		"nvidia-smi": {data: []byte("0, Test GPU, 8192, 7000, 10\n")},
	}}

	detector := NewDetector(runner, newTestLogger())
	poller := NewPoller(detector, 20*time.Millisecond, newTestLogger())

	ch := poller.Subscribe()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	poller.Start(ctx)

	select {
	case snap := <-ch:
		assert.Equal(t, VendorNVIDIA, snap.Vendor)
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for subscription update")
	}
}
