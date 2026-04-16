// Package storage provides an on-disk cache of staged model artifacts for BONNIE.
//
// Models are fetched from upstream sources (HuggingFace, NFS shares) and placed
// under a configurable root directory. The store keeps a JSON index at
// <dir>/index.json describing every entry: its id, source, size, files, and
// fetch/last-used timestamps. Writes are serialised through an RWMutex and
// fetches are deduplicated per (source, model_id) via singleflight so concurrent
// callers never race to download the same model twice.
package storage

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"sort"
	"sync"
	"time"

	"golang.org/x/sync/singleflight"
)

// Entry describes a staged model on disk.
type Entry struct {
	ID         string    `json:"id"`
	Source     string    `json:"source"`
	ModelID    string    `json:"model_id"`
	Path       string    `json:"path"`
	SizeBytes  int64     `json:"size_bytes"`
	Files      []string  `json:"files"`
	FetchedAt  time.Time `json:"fetched_at"`
	LastUsedAt time.Time `json:"last_used_at"`
}

// FetchRequest describes a model to stage.
type FetchRequest struct {
	Source      string   `json:"source"`
	ModelID     string   `json:"model_id"`
	Dest        string   `json:"dest,omitempty"`
	Patterns    []string `json:"patterns,omitempty"`
	MountSource string   `json:"mount_source,omitempty"`
	Subpath     string   `json:"subpath,omitempty"`
}

// Fetcher downloads or copies model artifacts from an upstream source into dest.
// Implementations must not mutate the FetchRequest.
type Fetcher interface {
	// Fetch stages the model described by req into dest and returns the list of
	// artifact file paths (relative to dest) that were produced.
	Fetch(ctx context.Context, req FetchRequest, dest string) (files []string, err error)
}

// ErrNotFound is returned when an entry is not present in the index.
var ErrNotFound = errors.New("storage: entry not found")

// Store manages the on-disk model cache and its JSON index.
type Store struct {
	dir      string
	logger   *slog.Logger
	fetchers map[string]Fetcher

	mu      sync.RWMutex
	entries map[string]Entry  // id -> entry
	byKey   map[string]string // "source:model_id" -> id

	inflight singleflight.Group
}

// NewStore creates the storage directory and loads (or initialises) the index.
func NewStore(dir string, logger *slog.Logger, hfToken string) (*Store, error) {
	if logger == nil {
		logger = slog.Default()
	}
	if dir == "" {
		return nil, fmt.Errorf("storage: NewStore: dir is required")
	}

	if err := os.MkdirAll(dir, 0o750); err != nil {
		return nil, fmt.Errorf("storage: NewStore: %w", err)
	}

	s := &Store{
		dir:    dir,
		logger: logger,
		fetchers: map[string]Fetcher{
			"huggingface": &huggingfaceFetcher{token: hfToken, logger: logger},
			"nfs":         &nfsFetcher{logger: logger},
		},
		entries: map[string]Entry{},
		byKey:   map[string]string{},
	}

	if err := s.loadIndex(); err != nil {
		return nil, fmt.Errorf("storage: NewStore: load index: %w", err)
	}

	return s, nil
}

// NewStoreWithFetchers is a test-oriented constructor that lets callers override
// the fetcher map.
func NewStoreWithFetchers(dir string, logger *slog.Logger, fetchers map[string]Fetcher) (*Store, error) {
	if logger == nil {
		logger = slog.Default()
	}
	if dir == "" {
		return nil, fmt.Errorf("storage: NewStoreWithFetchers: dir is required")
	}
	if err := os.MkdirAll(dir, 0o750); err != nil {
		return nil, fmt.Errorf("storage: NewStoreWithFetchers: %w", err)
	}
	s := &Store{
		dir:      dir,
		logger:   logger,
		fetchers: fetchers,
		entries:  map[string]Entry{},
		byKey:    map[string]string{},
	}
	if err := s.loadIndex(); err != nil {
		return nil, fmt.Errorf("storage: NewStoreWithFetchers: load index: %w", err)
	}
	return s, nil
}

// indexPath returns the absolute path to the index file.
func (s *Store) indexPath() string { return filepath.Join(s.dir, "index.json") }

// loadIndex reads index.json from disk into memory; a missing file is ok.
func (s *Store) loadIndex() error {
	data, err := os.ReadFile(s.indexPath())
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	if len(data) == 0 {
		return nil
	}
	var entries []Entry
	if err := json.Unmarshal(data, &entries); err != nil {
		return fmt.Errorf("parse index: %w", err)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	for i := range entries {
		e := &entries[i]
		s.entries[e.ID] = *e
		s.byKey[keyFor(e.Source, e.ModelID)] = e.ID
	}
	return nil
}

// saveIndexLocked atomically writes the current in-memory index to disk.
// Caller must hold s.mu (read or write lock).
func (s *Store) saveIndexLocked() error {
	entries := make([]Entry, 0, len(s.entries))
	for id := range s.entries {
		entries = append(entries, s.entries[id])
	}
	sort.Slice(entries, func(i, j int) bool { return entries[i].ID < entries[j].ID })

	data, err := json.MarshalIndent(entries, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal index: %w", err)
	}

	tmp, err := os.CreateTemp(s.dir, ".index.json.*")
	if err != nil {
		return fmt.Errorf("tempfile: %w", err)
	}
	tmpName := tmp.Name()
	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		_ = os.Remove(tmpName)
		return fmt.Errorf("write index: %w", err)
	}
	if err := tmp.Close(); err != nil {
		_ = os.Remove(tmpName)
		return fmt.Errorf("close index: %w", err)
	}
	if err := os.Rename(tmpName, s.indexPath()); err != nil {
		_ = os.Remove(tmpName)
		return fmt.Errorf("rename index: %w", err)
	}
	return nil
}

// Fetch stages the model described by req. The operation is idempotent:
// repeat calls for the same (source, model_id) return the existing entry and
// update its LastUsedAt timestamp. Concurrent calls for the same model block
// until the first completes.
//
//nolint:gocritic // hugeParam: signature is part of the public API.
func (s *Store) Fetch(ctx context.Context, req FetchRequest) (Entry, error) {
	if req.Source == "" {
		return Entry{}, fmt.Errorf("storage: fetch: source is required")
	}
	if req.ModelID == "" {
		return Entry{}, fmt.Errorf("storage: fetch: model_id is required")
	}
	fetcher, ok := s.fetchers[req.Source]
	if !ok {
		return Entry{}, fmt.Errorf("storage: fetch: unknown source %q", req.Source)
	}
	if req.Source == "nfs" && req.MountSource == "" {
		return Entry{}, fmt.Errorf("storage: fetch: mount_source is required for nfs")
	}

	key := keyFor(req.Source, req.ModelID)

	// Fast path: already present.
	if existing, ok := s.lookupByKey(key); ok {
		if err := s.markUsed(existing.ID); err != nil {
			return Entry{}, fmt.Errorf("storage: fetch: %w", err)
		}
		// Re-read after mark to pick up LastUsedAt.
		s.mu.RLock()
		e := s.entries[existing.ID]
		s.mu.RUnlock()
		return e, nil
	}

	// Deduplicate concurrent fetches via singleflight.
	val, err, _ := s.inflight.Do(key, func() (any, error) {
		// Double-check after taking the lead.
		if existing, ok := s.lookupByKey(key); ok {
			if err := s.markUsed(existing.ID); err != nil {
				return Entry{}, err
			}
			s.mu.RLock()
			e := s.entries[existing.ID]
			s.mu.RUnlock()
			return e, nil
		}
		return s.doFetch(ctx, req, fetcher)
	})
	if err != nil {
		return Entry{}, fmt.Errorf("storage: fetch: %w", err)
	}
	return val.(Entry), nil
}

// doFetch performs the actual fetch. Assumes no existing entry for the key.
//
//nolint:gocritic // hugeParam: we intentionally take FetchRequest by value.
func (s *Store) doFetch(ctx context.Context, req FetchRequest, fetcher Fetcher) (Entry, error) {
	id := entryID(req.Source, req.ModelID)
	dest := req.Dest
	if dest == "" {
		dest = filepath.Join(s.dir, id)
	}
	if err := os.MkdirAll(dest, 0o750); err != nil {
		return Entry{}, fmt.Errorf("create dest: %w", err)
	}

	s.logger.Info("fetching model",
		"source", req.Source, "model_id", req.ModelID, "dest", dest)

	files, err := fetcher.Fetch(ctx, req, dest)
	if err != nil {
		// On failure, clean up any partial directory we created inside our own root.
		if req.Dest == "" {
			_ = os.RemoveAll(dest)
		}
		return Entry{}, fmt.Errorf("fetcher: %w", err)
	}

	size, discovered, err := measureTree(dest)
	if err != nil {
		return Entry{}, fmt.Errorf("measure: %w", err)
	}
	if len(files) == 0 {
		files = discovered
	}
	sort.Strings(files)

	now := time.Now().UTC()
	e := Entry{
		ID:         id,
		Source:     req.Source,
		ModelID:    req.ModelID,
		Path:       dest,
		SizeBytes:  size,
		Files:      files,
		FetchedAt:  now,
		LastUsedAt: now,
	}

	s.mu.Lock()
	s.entries[e.ID] = e
	s.byKey[keyFor(e.Source, e.ModelID)] = e.ID
	if err := s.saveIndexLocked(); err != nil {
		// Roll back in-memory state to match disk.
		delete(s.entries, e.ID)
		delete(s.byKey, keyFor(e.Source, e.ModelID))
		s.mu.Unlock()
		return Entry{}, fmt.Errorf("save index: %w", err)
	}
	s.mu.Unlock()

	s.logger.Info("model staged",
		"id", e.ID, "size_bytes", e.SizeBytes, "files", len(e.Files))

	return e, nil
}

// List returns all staged models, sorted by ID for determinism.
func (s *Store) List(_ context.Context) ([]Entry, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]Entry, 0, len(s.entries))
	for id := range s.entries {
		out = append(out, s.entries[id])
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out, nil
}

// Get returns a single entry by ID.
func (s *Store) Get(_ context.Context, id string) (Entry, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	e, ok := s.entries[id]
	if !ok {
		return Entry{}, ErrNotFound
	}
	return e, nil
}

// Delete removes the on-disk files and index entry for id. Returns
// ErrNotFound if the id is unknown.
func (s *Store) Delete(_ context.Context, id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	e, ok := s.entries[id]
	if !ok {
		return ErrNotFound
	}
	// Only remove paths that live under our storage root — if the caller
	// supplied an explicit dest outside the root we leave it to them.
	if underRoot(s.dir, e.Path) {
		if err := os.RemoveAll(e.Path); err != nil {
			return fmt.Errorf("storage: delete: remove path: %w", err)
		}
	}
	delete(s.entries, id)
	delete(s.byKey, keyFor(e.Source, e.ModelID))
	if err := s.saveIndexLocked(); err != nil {
		// Best-effort: re-insert in memory if we can't persist.
		s.entries[id] = e
		s.byKey[keyFor(e.Source, e.ModelID)] = id
		return fmt.Errorf("storage: delete: save index: %w", err)
	}
	s.logger.Info("model deleted", "id", id, "path", e.Path)
	return nil
}

// MarkUsed updates the LastUsedAt timestamp for id.
func (s *Store) MarkUsed(_ context.Context, id string) error {
	return s.markUsed(id)
}

func (s *Store) markUsed(id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	e, ok := s.entries[id]
	if !ok {
		return ErrNotFound
	}
	e.LastUsedAt = time.Now().UTC()
	s.entries[id] = e
	return s.saveIndexLocked()
}

func (s *Store) lookupByKey(key string) (Entry, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	id, ok := s.byKey[key]
	if !ok {
		return Entry{}, false
	}
	e, ok := s.entries[id]
	return e, ok
}

// keyFor returns the singleflight / index key for (source, model_id).
func keyFor(source, modelID string) string { return source + ":" + modelID }

// entryID produces a stable short id for a (source, model_id) pair.
func entryID(source, modelID string) string {
	sum := sha256.Sum256([]byte(keyFor(source, modelID)))
	return hex.EncodeToString(sum[:])[:16]
}

// measureTree walks root, summing file sizes and returning their relative paths.
func measureTree(root string) (size int64, files []string, err error) {
	err = filepath.WalkDir(root, func(path string, d os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if d.IsDir() {
			return nil
		}
		info, infoErr := d.Info()
		if infoErr != nil {
			return infoErr
		}
		size += info.Size()
		rel, relErr := filepath.Rel(root, path)
		if relErr != nil {
			return relErr
		}
		files = append(files, rel)
		return nil
	})
	if err != nil {
		return 0, nil, err
	}
	sort.Strings(files)
	return size, files, nil
}

// underRoot reports whether path is inside root (after symlink-free cleaning).
func underRoot(root, path string) bool {
	absRoot, err := filepath.Abs(root)
	if err != nil {
		return false
	}
	absPath, err := filepath.Abs(path)
	if err != nil {
		return false
	}
	rel, err := filepath.Rel(absRoot, absPath)
	if err != nil {
		return false
	}
	return rel != ".." && !filepathHasDotDotPrefix(rel)
}

func filepathHasDotDotPrefix(rel string) bool {
	if len(rel) < 2 {
		return false
	}
	return rel[0] == '.' && rel[1] == '.' && (len(rel) == 2 || rel[2] == filepath.Separator)
}
