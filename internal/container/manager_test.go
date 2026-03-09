package container_test

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"testing"

	"github.com/docker/docker/api/types"
	containertypes "github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/image"
	networktypes "github.com/docker/docker/api/types/network"
	ocispec "github.com/opencontainers/image-spec/specs-go/v1"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/flag-ai/bonnie/internal/container"
	"github.com/flag-ai/bonnie/internal/gpu"
)

type mockDockerClient struct {
	createResp containertypes.CreateResponse
	createErr  error
	startErr   error
	stopErr    error
	restartErr error
	removeErr  error
	inspectRes containertypes.InspectResponse
	inspectErr error
	listRes    []containertypes.Summary
	listErr    error
	logsReader io.ReadCloser
	logsErr    error
	pingRes    types.Ping
	pingErr    error
}

func (m *mockDockerClient) ContainerCreate(_ context.Context, _ *containertypes.Config, _ *containertypes.HostConfig, _ *networktypes.NetworkingConfig, _ *ocispec.Platform, _ string) (containertypes.CreateResponse, error) {
	return m.createResp, m.createErr
}

func (m *mockDockerClient) ContainerStart(_ context.Context, _ string, _ containertypes.StartOptions) error {
	return m.startErr
}

func (m *mockDockerClient) ContainerStop(_ context.Context, _ string, _ containertypes.StopOptions) error {
	return m.stopErr
}

func (m *mockDockerClient) ContainerRestart(_ context.Context, _ string, _ containertypes.StopOptions) error {
	return m.restartErr
}

func (m *mockDockerClient) ContainerRemove(_ context.Context, _ string, _ containertypes.RemoveOptions) error {
	return m.removeErr
}

func (m *mockDockerClient) ContainerInspect(_ context.Context, _ string) (containertypes.InspectResponse, error) {
	return m.inspectRes, m.inspectErr
}

func (m *mockDockerClient) ContainerList(_ context.Context, _ containertypes.ListOptions) ([]containertypes.Summary, error) {
	return m.listRes, m.listErr
}

func (m *mockDockerClient) ContainerLogs(_ context.Context, _ string, _ containertypes.LogsOptions) (io.ReadCloser, error) {
	return m.logsReader, m.logsErr
}

func (m *mockDockerClient) ImagePull(_ context.Context, _ string, _ image.PullOptions) (io.ReadCloser, error) {
	return nil, nil
}

func (m *mockDockerClient) Ping(_ context.Context) (types.Ping, error) {
	return m.pingRes, m.pingErr
}

func (m *mockDockerClient) Close() error { return nil }

func newTestLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

func TestManager_Create(t *testing.T) {
	t.Parallel()

	client := &mockDockerClient{
		createResp: containertypes.CreateResponse{ID: "abc123def456"},
	}

	mgr := container.NewManager(client, gpu.VendorNVIDIA, newTestLogger())

	id, err := mgr.Create(context.Background(), &container.CreateRequest{
		Name:  "test-container",
		Image: "ubuntu:latest",
		GPU:   true,
	})

	require.NoError(t, err)
	assert.Equal(t, "abc123def456", id)
}

func TestManager_Create_Error(t *testing.T) {
	t.Parallel()

	client := &mockDockerClient{
		createErr: fmt.Errorf("image not found"),
	}

	mgr := container.NewManager(client, gpu.VendorUnknown, newTestLogger())

	_, err := mgr.Create(context.Background(), &container.CreateRequest{
		Image: "nonexistent:latest",
	})

	require.Error(t, err)
	assert.Contains(t, err.Error(), "image not found")
}

func TestManager_Start(t *testing.T) {
	t.Parallel()

	client := &mockDockerClient{}
	mgr := container.NewManager(client, gpu.VendorUnknown, newTestLogger())

	err := mgr.Start(context.Background(), "abc123def456")
	require.NoError(t, err)
}

func TestManager_Stop(t *testing.T) {
	t.Parallel()

	client := &mockDockerClient{}
	mgr := container.NewManager(client, gpu.VendorUnknown, newTestLogger())

	err := mgr.Stop(context.Background(), "abc123def456")
	require.NoError(t, err)
}

func TestManager_Restart(t *testing.T) {
	t.Parallel()

	client := &mockDockerClient{}
	mgr := container.NewManager(client, gpu.VendorUnknown, newTestLogger())

	err := mgr.Restart(context.Background(), "abc123def456")
	require.NoError(t, err)
}

func TestManager_Remove(t *testing.T) {
	t.Parallel()

	client := &mockDockerClient{}
	mgr := container.NewManager(client, gpu.VendorUnknown, newTestLogger())

	err := mgr.Remove(context.Background(), "abc123def456", true)
	require.NoError(t, err)
}

func TestManager_Inspect(t *testing.T) {
	t.Parallel()

	client := &mockDockerClient{
		inspectRes: containertypes.InspectResponse{
			ContainerJSONBase: &containertypes.ContainerJSONBase{
				ID:   "abc123def456",
				Name: "/test",
			},
		},
	}

	mgr := container.NewManager(client, gpu.VendorUnknown, newTestLogger())

	info, err := mgr.Inspect(context.Background(), "abc123def456")
	require.NoError(t, err)
	assert.Equal(t, "abc123def456", info.ID)
}

func TestManager_List(t *testing.T) {
	t.Parallel()

	client := &mockDockerClient{
		listRes: []containertypes.Summary{
			{
				ID:      "abc123",
				Names:   []string{"/test1"},
				Image:   "ubuntu:latest",
				State:   "running",
				Status:  "Up 5 minutes",
				Created: 1000,
			},
			{
				ID:      "def456",
				Names:   []string{"/test2"},
				Image:   "nginx:latest",
				State:   "exited",
				Status:  "Exited (0) 10 minutes ago",
				Created: 900,
			},
		},
	}

	mgr := container.NewManager(client, gpu.VendorUnknown, newTestLogger())

	containers, err := mgr.List(context.Background())
	require.NoError(t, err)
	require.Len(t, containers, 2)

	assert.Equal(t, "abc123", containers[0].ID)
	assert.Equal(t, "test1", containers[0].Name)
	assert.Equal(t, "running", containers[0].State)
}

func TestManager_List_Error(t *testing.T) {
	t.Parallel()

	client := &mockDockerClient{
		listErr: fmt.Errorf("docker daemon not running"),
	}

	mgr := container.NewManager(client, gpu.VendorUnknown, newTestLogger())

	_, err := mgr.List(context.Background())
	require.Error(t, err)
}
