package handlers_test

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/docker/docker/api/types"
	containertypes "github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/image"
	networktypes "github.com/docker/docker/api/types/network"
	ocispec "github.com/opencontainers/image-spec/specs-go/v1"
	"github.com/stretchr/testify/assert"

	"github.com/flag-ai/bonnie/internal/api/handlers"
	"github.com/flag-ai/commons/health"
)

// mockDockerClient implements container.DockerClient for handler tests.
type mockDockerClient struct {
	pingErr error
}

func (m *mockDockerClient) ContainerCreate(_ context.Context, _ *containertypes.Config, _ *containertypes.HostConfig, _ *networktypes.NetworkingConfig, _ *ocispec.Platform, _ string) (containertypes.CreateResponse, error) {
	return containertypes.CreateResponse{ID: "mock-container-id-123456"}, nil
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
	return containertypes.InspectResponse{}, nil
}

func (m *mockDockerClient) ContainerList(_ context.Context, _ containertypes.ListOptions) ([]containertypes.Summary, error) {
	return nil, nil
}

func (m *mockDockerClient) ContainerLogs(_ context.Context, _ string, _ containertypes.LogsOptions) (io.ReadCloser, error) {
	return nil, nil
}

func (m *mockDockerClient) ImagePull(_ context.Context, _ string, _ image.PullOptions) (io.ReadCloser, error) {
	return nil, nil
}

func (m *mockDockerClient) Ping(_ context.Context) (types.Ping, error) {
	return types.Ping{}, m.pingErr
}

func (m *mockDockerClient) Close() error { return nil }

func newTestLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

func TestHealth_Healthy(t *testing.T) {
	t.Parallel()

	registry := health.NewRegistry()
	docker := &mockDockerClient{}
	h := handlers.NewHealthHandler(registry, docker, newTestLogger())

	req := httptest.NewRequest(http.MethodGet, "/health", nil)
	rec := httptest.NewRecorder()

	h.Health(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)

	var report map[string]any
	err := json.NewDecoder(rec.Body).Decode(&report)
	assert.NoError(t, err)
	assert.Equal(t, true, report["healthy"])
}

func TestReady_DockerUp(t *testing.T) {
	t.Parallel()

	docker := &mockDockerClient{}
	h := handlers.NewHealthHandler(health.NewRegistry(), docker, newTestLogger())

	req := httptest.NewRequest(http.MethodGet, "/ready", nil)
	rec := httptest.NewRecorder()

	h.Ready(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)

	var body map[string]any
	err := json.NewDecoder(rec.Body).Decode(&body)
	assert.NoError(t, err)
	assert.Equal(t, true, body["ready"])
}

func TestReady_DockerDown(t *testing.T) {
	t.Parallel()

	docker := &mockDockerClient{pingErr: fmt.Errorf("connection refused")}
	h := handlers.NewHealthHandler(health.NewRegistry(), docker, newTestLogger())

	req := httptest.NewRequest(http.MethodGet, "/ready", nil)
	rec := httptest.NewRecorder()

	h.Ready(rec, req)

	assert.Equal(t, http.StatusServiceUnavailable, rec.Code)

	var body map[string]any
	err := json.NewDecoder(rec.Body).Decode(&body)
	assert.NoError(t, err)
	assert.Equal(t, false, body["ready"])
	// Error details are logged, not returned to client
	assert.NotContains(t, body, "error")
}
