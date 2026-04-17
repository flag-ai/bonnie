package handlers_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/flag-ai/bonnie/internal/api/handlers"
	"github.com/flag-ai/bonnie/internal/storage"
)

type fakeModelStore struct {
	fetchFn  func(ctx context.Context, req storage.FetchRequest) (storage.Entry, error)
	listFn   func(ctx context.Context) ([]storage.Entry, error)
	getFn    func(ctx context.Context, id string) (storage.Entry, error)
	deleteFn func(ctx context.Context, id string) error
}

//nolint:gocritic // hugeParam: signature matches ModelStore interface.
func (f *fakeModelStore) Fetch(ctx context.Context, req storage.FetchRequest) (storage.Entry, error) {
	return f.fetchFn(ctx, req)
}

func (f *fakeModelStore) List(ctx context.Context) ([]storage.Entry, error) {
	return f.listFn(ctx)
}

func (f *fakeModelStore) Get(ctx context.Context, id string) (storage.Entry, error) {
	return f.getFn(ctx, id)
}

func (f *fakeModelStore) Delete(ctx context.Context, id string) error {
	return f.deleteFn(ctx, id)
}

func TestModels_Fetch_Success(t *testing.T) {
	t.Parallel()

	entry := storage.Entry{
		ID: "abc", Source: "huggingface", ModelID: "org/x", Path: "/tmp/x",
		SizeBytes: 42, Files: []string{"weights.bin"},
		FetchedAt: time.Now(), LastUsedAt: time.Now(),
	}
	store := &fakeModelStore{
		fetchFn: func(_ context.Context, req storage.FetchRequest) (storage.Entry, error) {
			assert.Equal(t, "huggingface", req.Source)
			assert.Equal(t, "org/x", req.ModelID)
			return entry, nil
		},
	}
	h := handlers.NewModelsHandler(store, newTestLogger())

	body := `{"source":"huggingface","model_id":"org/x"}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/models/fetch", strings.NewReader(body))
	rec := httptest.NewRecorder()

	h.Fetch(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)
	var got storage.Entry
	require.NoError(t, json.NewDecoder(rec.Body).Decode(&got))
	assert.Equal(t, entry.ID, got.ID)
	assert.Equal(t, entry.Path, got.Path)
}

func TestModels_Fetch_InvalidBody(t *testing.T) {
	t.Parallel()

	h := handlers.NewModelsHandler(&fakeModelStore{}, newTestLogger())
	req := httptest.NewRequest(http.MethodPost, "/api/v1/models/fetch", strings.NewReader("not json"))
	rec := httptest.NewRecorder()

	h.Fetch(rec, req)
	assert.Equal(t, http.StatusBadRequest, rec.Code)
}

func TestModels_Fetch_MissingSource(t *testing.T) {
	t.Parallel()

	h := handlers.NewModelsHandler(&fakeModelStore{}, newTestLogger())
	body := `{"model_id":"org/x"}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/models/fetch", strings.NewReader(body))
	rec := httptest.NewRecorder()

	h.Fetch(rec, req)
	assert.Equal(t, http.StatusBadRequest, rec.Code)
	assert.Contains(t, rec.Body.String(), "source is required")
}

func TestModels_Fetch_MissingModelID(t *testing.T) {
	t.Parallel()

	h := handlers.NewModelsHandler(&fakeModelStore{}, newTestLogger())
	body := `{"source":"huggingface"}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/models/fetch", strings.NewReader(body))
	rec := httptest.NewRecorder()

	h.Fetch(rec, req)
	assert.Equal(t, http.StatusBadRequest, rec.Code)
	assert.Contains(t, rec.Body.String(), "model_id is required")
}

func TestModels_Fetch_StoreError(t *testing.T) {
	t.Parallel()

	store := &fakeModelStore{
		fetchFn: func(_ context.Context, _ storage.FetchRequest) (storage.Entry, error) {
			return storage.Entry{}, assert.AnError
		},
	}
	h := handlers.NewModelsHandler(store, newTestLogger())
	body := `{"source":"huggingface","model_id":"org/x"}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/models/fetch", strings.NewReader(body))
	rec := httptest.NewRecorder()

	h.Fetch(rec, req)
	assert.Equal(t, http.StatusInternalServerError, rec.Code)
}

func TestModels_List(t *testing.T) {
	t.Parallel()

	entries := []storage.Entry{
		{ID: "a", Source: "huggingface", ModelID: "org/a"},
		{ID: "b", Source: "nfs", ModelID: "org/b"},
	}
	store := &fakeModelStore{
		listFn: func(_ context.Context) ([]storage.Entry, error) {
			return entries, nil
		},
	}
	h := handlers.NewModelsHandler(store, newTestLogger())

	req := httptest.NewRequest(http.MethodGet, "/api/v1/models", http.NoBody)
	rec := httptest.NewRecorder()

	h.List(rec, req)
	assert.Equal(t, http.StatusOK, rec.Code)

	var got []storage.Entry
	require.NoError(t, json.NewDecoder(rec.Body).Decode(&got))
	assert.Len(t, got, 2)
}

func TestModels_List_EmptyReturnsArrayNotNull(t *testing.T) {
	t.Parallel()

	store := &fakeModelStore{
		listFn: func(_ context.Context) ([]storage.Entry, error) {
			return nil, nil
		},
	}
	h := handlers.NewModelsHandler(store, newTestLogger())
	req := httptest.NewRequest(http.MethodGet, "/api/v1/models", http.NoBody)
	rec := httptest.NewRecorder()

	h.List(rec, req)
	assert.Equal(t, http.StatusOK, rec.Code)
	// Expect "[]" not "null".
	assert.Equal(t, "[]\n", rec.Body.String())
}

func TestModels_List_StoreError(t *testing.T) {
	t.Parallel()

	store := &fakeModelStore{
		listFn: func(_ context.Context) ([]storage.Entry, error) {
			return nil, assert.AnError
		},
	}
	h := handlers.NewModelsHandler(store, newTestLogger())
	req := httptest.NewRequest(http.MethodGet, "/api/v1/models", http.NoBody)
	rec := httptest.NewRecorder()

	h.List(rec, req)
	assert.Equal(t, http.StatusInternalServerError, rec.Code)
}

func TestModels_Delete_Success(t *testing.T) {
	t.Parallel()

	store := &fakeModelStore{
		deleteFn: func(_ context.Context, id string) error {
			assert.Equal(t, "abc", id)
			return nil
		},
	}
	h := handlers.NewModelsHandler(store, newTestLogger())

	r := chi.NewRouter()
	r.Delete("/api/v1/models/{id}", h.Delete)

	req := httptest.NewRequest(http.MethodDelete, "/api/v1/models/abc", http.NoBody)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusNoContent, rec.Code)
}

func TestModels_Delete_NotFound(t *testing.T) {
	t.Parallel()

	store := &fakeModelStore{
		deleteFn: func(_ context.Context, _ string) error {
			return storage.ErrNotFound
		},
	}
	h := handlers.NewModelsHandler(store, newTestLogger())

	r := chi.NewRouter()
	r.Delete("/api/v1/models/{id}", h.Delete)

	req := httptest.NewRequest(http.MethodDelete, "/api/v1/models/abc", http.NoBody)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusNotFound, rec.Code)
	assert.Contains(t, rec.Body.String(), "model not found")
}

func TestModels_Delete_StoreError(t *testing.T) {
	t.Parallel()

	store := &fakeModelStore{
		deleteFn: func(_ context.Context, _ string) error {
			return assert.AnError
		},
	}
	h := handlers.NewModelsHandler(store, newTestLogger())

	r := chi.NewRouter()
	r.Delete("/api/v1/models/{id}", h.Delete)

	req := httptest.NewRequest(http.MethodDelete, "/api/v1/models/abc", http.NoBody)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusInternalServerError, rec.Code)
}
