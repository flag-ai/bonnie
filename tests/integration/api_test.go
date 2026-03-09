package integration_test

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/docker/docker/api/types"
	containertypes "github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/image"
	networktypes "github.com/docker/docker/api/types/network"
	ocispec "github.com/opencontainers/image-spec/specs-go/v1"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/flag-ai/bonnie/internal/api"
	"github.com/flag-ai/bonnie/internal/container"
	"github.com/flag-ai/bonnie/internal/gpu"
	"github.com/flag-ai/commons/health"
)

// mockDockerClient for integration tests.
type mockDockerClient struct {
	containers []containertypes.Summary
}

func (m *mockDockerClient) ContainerCreate(_ context.Context, _ *containertypes.Config, _ *containertypes.HostConfig, _ *networktypes.NetworkingConfig, _ *ocispec.Platform, _ string) (containertypes.CreateResponse, error) {
	return containertypes.CreateResponse{ID: "integration-test-id"}, nil
}

func (m *mockDockerClient) ContainerStart(_ context.Context, _ string, _ containertypes.StartOptions) error {
	return nil
}

func (m *mockDockerClient) ContainerStop(_ context.Context, _ string, _ containertypes.StopOptions) error {
	return nil
}

func (m *mockDockerClient) ContainerRestart(_ context.Context, _ string, _ containertypes.StopOptions) error {
	return nil
}

func (m *mockDockerClient) ContainerRemove(_ context.Context, _ string, _ containertypes.RemoveOptions) error {
	return nil
}

func (m *mockDockerClient) ContainerInspect(_ context.Context, _ string) (containertypes.InspectResponse, error) {
	return containertypes.InspectResponse{
		ContainerJSONBase: &containertypes.ContainerJSONBase{ID: "test"},
	}, nil
}

func (m *mockDockerClient) ContainerList(_ context.Context, _ containertypes.ListOptions) ([]containertypes.Summary, error) {
	return m.containers, nil
}

func (m *mockDockerClient) ContainerLogs(_ context.Context, _ string, _ containertypes.LogsOptions) (io.ReadCloser, error) {
	return io.NopCloser(strings.NewReader("test log line\n")), nil
}

func (m *mockDockerClient) ImagePull(_ context.Context, _ string, _ image.PullOptions) (io.ReadCloser, error) {
	return nil, nil
}

func (m *mockDockerClient) Ping(_ context.Context) (types.Ping, error) {
	return types.Ping{}, nil
}

func (m *mockDockerClient) Close() error { return nil }

type mockCommandRunner struct{}

func (m *mockCommandRunner) Run(_ context.Context, name string, _ ...string) ([]byte, error) {
	switch name {
	case "nvidia-smi":
		return []byte("0, Test GPU, 8192, 7000, 10\n"), nil
	case "uname":
		return []byte("6.1.0-test\n"), nil
	case "df":
		return []byte("     1G-blocks      Used     Avail Use%\n          500G      200G      300G  40%\n"), nil
	default:
		return nil, fmt.Errorf("command not found: %s", name)
	}
}

func setupRouter(t *testing.T) *httptest.Server {
	t.Helper()

	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	runner := &mockCommandRunner{}
	docker := &mockDockerClient{}

	detector := gpu.NewDetector(runner, logger)
	poller := gpu.NewPoller(detector, time.Hour, logger)

	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	poller.Start(ctx)

	mgr := container.NewManager(docker, gpu.VendorNVIDIA, logger)
	registry := health.NewRegistry()

	router := api.NewRouter(&api.RouterConfig{
		Logger:    logger,
		AuthToken: "test-token",
		Registry:  registry,
		Docker:    docker,
		Manager:   mgr,
		Poller:    poller,
		Runner:    runner,
	})

	return httptest.NewServer(router)
}

func TestIntegration_HealthEndpoint(t *testing.T) {
	t.Parallel()

	srv := setupRouter(t)
	defer srv.Close()

	resp, err := http.Get(srv.URL + "/health")
	require.NoError(t, err)
	defer func() { _ = resp.Body.Close() }()

	assert.Equal(t, http.StatusOK, resp.StatusCode)

	var report map[string]any
	decErr := json.NewDecoder(resp.Body).Decode(&report)
	require.NoError(t, decErr)
	assert.Equal(t, true, report["healthy"])
}

func TestIntegration_ReadyEndpoint(t *testing.T) {
	t.Parallel()

	srv := setupRouter(t)
	defer srv.Close()

	resp, err := http.Get(srv.URL + "/ready")
	require.NoError(t, err)
	defer func() { _ = resp.Body.Close() }()

	assert.Equal(t, http.StatusOK, resp.StatusCode)
}

func TestIntegration_MetricsEndpoint(t *testing.T) {
	t.Parallel()

	srv := setupRouter(t)
	defer srv.Close()

	resp, err := http.Get(srv.URL + "/metrics")
	require.NoError(t, err)
	defer func() { _ = resp.Body.Close() }()

	assert.Equal(t, http.StatusOK, resp.StatusCode)
	assert.Contains(t, resp.Header.Get("Content-Type"), "text/plain")
}

func TestIntegration_AuthRequired(t *testing.T) {
	t.Parallel()

	srv := setupRouter(t)
	defer srv.Close()

	resp, err := http.Get(srv.URL + "/api/v1/system/info")
	require.NoError(t, err)
	defer func() { _ = resp.Body.Close() }()

	assert.Equal(t, http.StatusUnauthorized, resp.StatusCode)
}

func TestIntegration_AuthWithToken(t *testing.T) {
	t.Parallel()

	srv := setupRouter(t)
	defer srv.Close()

	req, _ := http.NewRequest(http.MethodGet, srv.URL+"/api/v1/system/info", http.NoBody)
	req.Header.Set("Authorization", "Bearer test-token")

	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	defer func() { _ = resp.Body.Close() }()

	assert.Equal(t, http.StatusOK, resp.StatusCode)
}

func TestIntegration_GPUStatus(t *testing.T) {
	t.Parallel()

	srv := setupRouter(t)
	defer srv.Close()

	req, _ := http.NewRequest(http.MethodGet, srv.URL+"/api/v1/gpu/status", http.NoBody)
	req.Header.Set("Authorization", "Bearer test-token")

	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	defer func() { _ = resp.Body.Close() }()

	assert.Equal(t, http.StatusOK, resp.StatusCode)

	var snap gpu.Snapshot
	decErr := json.NewDecoder(resp.Body).Decode(&snap)
	require.NoError(t, decErr)
	assert.Equal(t, gpu.VendorNVIDIA, snap.Vendor)
}

func TestIntegration_ContainerLifecycle(t *testing.T) {
	t.Parallel()

	srv := setupRouter(t)
	defer srv.Close()

	// Create container
	body := `{"name": "test", "image": "ubuntu:latest", "gpu": true}`
	req, _ := http.NewRequest(http.MethodPost, srv.URL+"/api/v1/containers", strings.NewReader(body))
	req.Header.Set("Authorization", "Bearer test-token")
	req.Header.Set("Content-Type", "application/json")

	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	defer func() { _ = resp.Body.Close() }()

	assert.Equal(t, http.StatusCreated, resp.StatusCode)

	var createResp map[string]string
	decErr := json.NewDecoder(resp.Body).Decode(&createResp)
	require.NoError(t, decErr)
	assert.NotEmpty(t, createResp["id"])

	// List containers
	req, _ = http.NewRequest(http.MethodGet, srv.URL+"/api/v1/containers", http.NoBody)
	req.Header.Set("Authorization", "Bearer test-token")

	resp2, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	defer func() { _ = resp2.Body.Close() }()

	assert.Equal(t, http.StatusOK, resp2.StatusCode)
}
