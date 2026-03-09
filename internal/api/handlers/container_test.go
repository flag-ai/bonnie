package handlers_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/flag-ai/bonnie/internal/api/handlers"
	"github.com/flag-ai/bonnie/internal/container"
	"github.com/flag-ai/bonnie/internal/gpu"
)

func newContainerHandler() *handlers.ContainerHandler {
	client := &mockDockerClient{}
	mgr := container.NewManager(client, gpu.VendorUnknown, newTestLogger())
	return handlers.NewContainerHandler(mgr, client, newTestLogger())
}

func TestContainerList(t *testing.T) {
	t.Parallel()

	h := newContainerHandler()

	req := httptest.NewRequest(http.MethodGet, "/api/v1/containers", nil)
	rec := httptest.NewRecorder()

	h.List(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)
}

func TestContainerCreate_MissingImage(t *testing.T) {
	t.Parallel()

	h := newContainerHandler()

	body := `{"name": "test"}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/containers", strings.NewReader(body))
	rec := httptest.NewRecorder()

	h.Create(rec, req)

	assert.Equal(t, http.StatusBadRequest, rec.Code)

	var resp map[string]string
	err := json.NewDecoder(rec.Body).Decode(&resp)
	require.NoError(t, err)
	assert.Equal(t, "image is required", resp["error"])
}

func TestContainerCreate_InvalidBody(t *testing.T) {
	t.Parallel()

	h := newContainerHandler()

	req := httptest.NewRequest(http.MethodPost, "/api/v1/containers", strings.NewReader("not json"))
	rec := httptest.NewRecorder()

	h.Create(rec, req)

	assert.Equal(t, http.StatusBadRequest, rec.Code)
}

func TestContainerCreate_Success(t *testing.T) {
	t.Parallel()

	h := newContainerHandler()

	body := `{"name": "test", "image": "ubuntu:latest"}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/containers", strings.NewReader(body))
	rec := httptest.NewRecorder()

	h.Create(rec, req)

	assert.Equal(t, http.StatusCreated, rec.Code)

	var resp map[string]string
	err := json.NewDecoder(rec.Body).Decode(&resp)
	require.NoError(t, err)
	assert.NotEmpty(t, resp["id"])
}

func TestContainerStart(t *testing.T) {
	t.Parallel()

	h := newContainerHandler()

	r := chi.NewRouter()
	r.Post("/containers/{id}/start", h.Start)

	req := httptest.NewRequest(http.MethodPost, "/containers/abc123def456/start", nil)
	rec := httptest.NewRecorder()

	r.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)
}

func TestContainerStop(t *testing.T) {
	t.Parallel()

	h := newContainerHandler()

	r := chi.NewRouter()
	r.Post("/containers/{id}/stop", h.Stop)

	req := httptest.NewRequest(http.MethodPost, "/containers/abc123def456/stop", nil)
	rec := httptest.NewRecorder()

	r.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)
}

func TestContainerRestart(t *testing.T) {
	t.Parallel()

	h := newContainerHandler()

	r := chi.NewRouter()
	r.Post("/containers/{id}/restart", h.Restart)

	req := httptest.NewRequest(http.MethodPost, "/containers/abc123def456/restart", nil)
	rec := httptest.NewRecorder()

	r.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)
}

func TestContainerRemove(t *testing.T) {
	t.Parallel()

	h := newContainerHandler()

	r := chi.NewRouter()
	r.Delete("/containers/{id}", h.Remove)

	req := httptest.NewRequest(http.MethodDelete, "/containers/abc123def456", nil)
	rec := httptest.NewRecorder()

	r.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusNoContent, rec.Code)
}
