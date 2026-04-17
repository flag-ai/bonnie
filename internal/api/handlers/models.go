package handlers

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"

	"github.com/go-chi/chi/v5"

	"github.com/flag-ai/bonnie/internal/storage"
)

// ModelStore is the subset of *storage.Store used by ModelsHandler.
// Defined as an interface so handler tests can plug in a fake.
type ModelStore interface {
	Fetch(ctx context.Context, req storage.FetchRequest) (storage.Entry, error)
	List(ctx context.Context) ([]storage.Entry, error)
	Get(ctx context.Context, id string) (storage.Entry, error)
	Delete(ctx context.Context, id string) error
}

// ModelsHandler serves model-storage endpoints.
type ModelsHandler struct {
	store  ModelStore
	logger *slog.Logger
}

// NewModelsHandler creates a ModelsHandler.
func NewModelsHandler(store ModelStore, logger *slog.Logger) *ModelsHandler {
	return &ModelsHandler{store: store, logger: logger}
}

// Fetch stages a model on disk and returns the resulting entry.
func (h *ModelsHandler) Fetch(w http.ResponseWriter, r *http.Request) {
	var req storage.FetchRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	if req.Source == "" {
		writeError(w, http.StatusBadRequest, "source is required")
		return
	}
	if req.ModelID == "" {
		writeError(w, http.StatusBadRequest, "model_id is required")
		return
	}

	entry, err := h.store.Fetch(r.Context(), req)
	if err != nil {
		h.logger.Error("model fetch failed", "error", err,
			"source", req.Source, "model_id", req.ModelID)
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(entry); err != nil {
		h.logger.Error("failed to encode fetch response", "error", err)
	}
}

// List returns all staged models.
func (h *ModelsHandler) List(w http.ResponseWriter, r *http.Request) {
	entries, err := h.store.List(r.Context())
	if err != nil {
		h.logger.Error("list models failed", "error", err)
		writeError(w, http.StatusInternalServerError, "failed to list models")
		return
	}
	if entries == nil {
		entries = []storage.Entry{}
	}

	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(entries); err != nil {
		h.logger.Error("failed to encode list response", "error", err)
	}
}

// Delete removes a staged model.
func (h *ModelsHandler) Delete(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	if id == "" {
		writeError(w, http.StatusBadRequest, "id is required")
		return
	}

	if err := h.store.Delete(r.Context(), id); err != nil {
		if errors.Is(err, storage.ErrNotFound) {
			writeError(w, http.StatusNotFound, "model not found")
			return
		}
		h.logger.Error("delete model failed", "error", err, "id", id)
		writeError(w, http.StatusInternalServerError, "failed to delete model")
		return
	}

	w.WriteHeader(http.StatusNoContent)
}
