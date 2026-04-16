package storage_test

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/flag-ai/bonnie/internal/storage"
)

func newTestLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

// fakeFetcher writes a single file whose contents depend on the model id so
// we can assert later that each model landed its own bytes.
type fakeFetcher struct {
	calls   atomic.Int64
	block   chan struct{} // if non-nil, Fetch blocks until closed
	fetched chan string   // non-nil: send one message per call (model id)
	err     error
}

//nolint:gocritic // hugeParam: signature matches Fetcher interface.
func (f *fakeFetcher) Fetch(_ context.Context, req storage.FetchRequest, dest string) ([]string, error) {
	f.calls.Add(1)
	if f.fetched != nil {
		f.fetched <- req.ModelID
	}
	if f.block != nil {
		<-f.block
	}
	if f.err != nil {
		return nil, f.err
	}
	name := "weights.bin"
	body := []byte("contents of " + req.ModelID)
	if err := os.WriteFile(filepath.Join(dest, name), body, 0o600); err != nil {
		return nil, err
	}
	// Also a subdirectory to exercise tree measurement.
	if err := os.MkdirAll(filepath.Join(dest, "config"), 0o750); err != nil {
		return nil, err
	}
	if err := os.WriteFile(filepath.Join(dest, "config", "tokenizer.json"), []byte("{}"), 0o600); err != nil {
		return nil, err
	}
	return nil, nil
}

func newStore(t *testing.T, fetcher storage.Fetcher) (store *storage.Store, dir string) {
	t.Helper()
	dir = t.TempDir()
	s, err := storage.NewStoreWithFetchers(dir, newTestLogger(), map[string]storage.Fetcher{
		"huggingface": fetcher,
		"nfs":         fetcher,
	})
	require.NoError(t, err)
	return s, dir
}

func TestStore_FetchIdempotent(t *testing.T) {
	t.Parallel()

	f := &fakeFetcher{}
	s, _ := newStore(t, f)

	ctx := context.Background()
	req := storage.FetchRequest{Source: "huggingface", ModelID: "org/model"}

	first, err := s.Fetch(ctx, req)
	require.NoError(t, err)
	assert.Equal(t, int64(1), f.calls.Load())
	assert.NotEmpty(t, first.ID)
	assert.Equal(t, "huggingface", first.Source)
	assert.Equal(t, "org/model", first.ModelID)
	assert.Greater(t, first.SizeBytes, int64(0))
	assert.NotEmpty(t, first.Files)

	// Second call must return the same entry without calling the fetcher again.
	second, err := s.Fetch(ctx, req)
	require.NoError(t, err)
	assert.Equal(t, int64(1), f.calls.Load())
	assert.Equal(t, first.ID, second.ID)
	assert.Equal(t, first.Path, second.Path)
	// LastUsedAt should advance (or at least be >= the original).
	assert.False(t, second.LastUsedAt.Before(first.LastUsedAt))
}

func TestStore_FetchConcurrentDeduplicates(t *testing.T) {
	t.Parallel()

	f := &fakeFetcher{
		block:   make(chan struct{}),
		fetched: make(chan string, 16),
	}
	s, _ := newStore(t, f)

	const n = 8
	req := storage.FetchRequest{Source: "huggingface", ModelID: "org/shared"}

	results := make([]storage.Entry, n)
	errs := make([]error, n)
	var wg sync.WaitGroup
	wg.Add(n)
	for i := 0; i < n; i++ {
		go func(i int) {
			defer wg.Done()
			results[i], errs[i] = s.Fetch(context.Background(), req)
		}(i)
	}

	// Wait for the fetch to actually start, then unblock it.
	select {
	case <-f.fetched:
	case <-time.After(2 * time.Second):
		t.Fatal("fetcher was never called")
	}
	close(f.block)
	wg.Wait()

	// Only one actual fetch should have happened.
	assert.Equal(t, int64(1), f.calls.Load())

	id := results[0].ID
	for i := range results {
		require.NoError(t, errs[i])
		assert.Equal(t, id, results[i].ID)
	}
}

func TestStore_FetchDifferentModelsCoexist(t *testing.T) {
	t.Parallel()

	f := &fakeFetcher{}
	s, _ := newStore(t, f)

	ctx := context.Background()
	a, err := s.Fetch(ctx, storage.FetchRequest{Source: "huggingface", ModelID: "org/a"})
	require.NoError(t, err)
	b, err := s.Fetch(ctx, storage.FetchRequest{Source: "huggingface", ModelID: "org/b"})
	require.NoError(t, err)

	assert.NotEqual(t, a.ID, b.ID)
	assert.Equal(t, int64(2), f.calls.Load())

	entries, err := s.List(ctx)
	require.NoError(t, err)
	assert.Len(t, entries, 2)
}

func TestStore_FetchRejectsUnknownSource(t *testing.T) {
	t.Parallel()

	s, _ := newStore(t, &fakeFetcher{})
	_, err := s.Fetch(context.Background(), storage.FetchRequest{Source: "ftp", ModelID: "x"})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "unknown source")
}

func TestStore_FetchValidatesRequired(t *testing.T) {
	t.Parallel()

	s, _ := newStore(t, &fakeFetcher{})
	_, err := s.Fetch(context.Background(), storage.FetchRequest{Source: "huggingface"})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "model_id is required")

	_, err = s.Fetch(context.Background(), storage.FetchRequest{ModelID: "org/x"})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "source is required")
}

func TestStore_FetchNFSRequiresMountSource(t *testing.T) {
	t.Parallel()

	s, _ := newStore(t, &fakeFetcher{})
	_, err := s.Fetch(context.Background(), storage.FetchRequest{Source: "nfs", ModelID: "x"})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "mount_source is required")
}

func TestStore_FetchCleansUpOnFetcherError(t *testing.T) {
	t.Parallel()

	f := &fakeFetcher{err: fmt.Errorf("boom")}
	s, dir := newStore(t, f)

	_, err := s.Fetch(context.Background(), storage.FetchRequest{Source: "huggingface", ModelID: "org/bad"})
	require.Error(t, err)

	// The per-entry directory should have been cleaned up; only the index
	// directory itself should remain (and there should be no index file yet).
	entries, err := s.List(context.Background())
	require.NoError(t, err)
	assert.Empty(t, entries)

	ents, readErr := os.ReadDir(dir)
	require.NoError(t, readErr)
	for _, e := range ents {
		assert.Falsef(t, e.IsDir(), "expected no model dirs left, found %s", e.Name())
	}
}

func TestStore_Delete(t *testing.T) {
	t.Parallel()

	f := &fakeFetcher{}
	s, _ := newStore(t, f)
	ctx := context.Background()

	e, err := s.Fetch(ctx, storage.FetchRequest{Source: "huggingface", ModelID: "org/to-delete"})
	require.NoError(t, err)

	require.NoError(t, s.Delete(ctx, e.ID))

	_, err = s.Get(ctx, e.ID)
	require.ErrorIs(t, err, storage.ErrNotFound)

	_, err = os.Stat(e.Path)
	assert.True(t, os.IsNotExist(err), "path should be removed")
}

func TestStore_DeleteUnknown(t *testing.T) {
	t.Parallel()

	s, _ := newStore(t, &fakeFetcher{})
	err := s.Delete(context.Background(), "does-not-exist")
	require.ErrorIs(t, err, storage.ErrNotFound)
}

func TestStore_PersistsAcrossReopens(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	s, err := storage.NewStoreWithFetchers(dir, newTestLogger(), map[string]storage.Fetcher{
		"huggingface": &fakeFetcher{},
	})
	require.NoError(t, err)

	e, err := s.Fetch(context.Background(), storage.FetchRequest{Source: "huggingface", ModelID: "org/persist"})
	require.NoError(t, err)

	// Reopen with a brand-new Store; entries should reload.
	s2, err := storage.NewStoreWithFetchers(dir, newTestLogger(), map[string]storage.Fetcher{
		"huggingface": &fakeFetcher{},
	})
	require.NoError(t, err)

	got, err := s2.Get(context.Background(), e.ID)
	require.NoError(t, err)
	assert.Equal(t, e.ID, got.ID)
	assert.Equal(t, e.Path, got.Path)
	assert.Equal(t, e.SizeBytes, got.SizeBytes)
}

func TestStore_MarkUsedUpdatesTimestamp(t *testing.T) {
	t.Parallel()

	s, _ := newStore(t, &fakeFetcher{})
	ctx := context.Background()

	e, err := s.Fetch(ctx, storage.FetchRequest{Source: "huggingface", ModelID: "org/used"})
	require.NoError(t, err)

	original := e.LastUsedAt
	time.Sleep(5 * time.Millisecond)

	require.NoError(t, s.MarkUsed(ctx, e.ID))

	updated, err := s.Get(ctx, e.ID)
	require.NoError(t, err)
	assert.True(t, updated.LastUsedAt.After(original))
}

func TestStore_MarkUsedUnknown(t *testing.T) {
	t.Parallel()

	s, _ := newStore(t, &fakeFetcher{})
	err := s.MarkUsed(context.Background(), "missing")
	require.ErrorIs(t, err, storage.ErrNotFound)
}

func TestStore_ListReflectsDeletes(t *testing.T) {
	t.Parallel()

	s, _ := newStore(t, &fakeFetcher{})
	ctx := context.Background()

	a, err := s.Fetch(ctx, storage.FetchRequest{Source: "huggingface", ModelID: "org/a"})
	require.NoError(t, err)
	_, err = s.Fetch(ctx, storage.FetchRequest{Source: "huggingface", ModelID: "org/b"})
	require.NoError(t, err)

	require.NoError(t, s.Delete(ctx, a.ID))

	entries, err := s.List(ctx)
	require.NoError(t, err)
	require.Len(t, entries, 1)
	assert.Equal(t, "org/b", entries[0].ModelID)
}

func TestNFSFetcher_CopyDirectory(t *testing.T) {
	t.Parallel()

	mount := t.TempDir()
	sub := "snapshots/v1"
	srcDir := filepath.Join(mount, sub)
	require.NoError(t, os.MkdirAll(filepath.Join(srcDir, "nested"), 0o750))
	require.NoError(t, os.WriteFile(filepath.Join(srcDir, "a.bin"), []byte("A"), 0o600))
	require.NoError(t, os.WriteFile(filepath.Join(srcDir, "nested", "b.bin"), []byte("BB"), 0o600))

	dir := t.TempDir()
	s, err := storage.NewStore(dir, newTestLogger(), "")
	require.NoError(t, err)

	e, err := s.Fetch(context.Background(), storage.FetchRequest{
		Source:      "nfs",
		ModelID:     "org/from-nfs",
		MountSource: mount,
		Subpath:     sub,
	})
	require.NoError(t, err)
	assert.Equal(t, int64(3), e.SizeBytes) // "A" (1) + "BB" (2)
	assert.Contains(t, e.Files, "a.bin")
	assert.Contains(t, e.Files, filepath.Join("nested", "b.bin"))
}

func TestNFSFetcher_MissingSource(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	s, err := storage.NewStore(dir, newTestLogger(), "")
	require.NoError(t, err)

	_, err = s.Fetch(context.Background(), storage.FetchRequest{
		Source:      "nfs",
		ModelID:     "org/missing",
		MountSource: t.TempDir(),
		Subpath:     "does-not-exist",
	})
	require.Error(t, err)
}

func TestHuggingFaceFetcher_MissingBinary(t *testing.T) {
	// Cannot t.Parallel because t.Setenv mutates process-wide state.

	// Force PATH to a directory that contains no huggingface-cli.
	empty := t.TempDir()
	t.Setenv("PATH", empty)

	dir := t.TempDir()
	s, err := storage.NewStore(dir, newTestLogger(), "")
	require.NoError(t, err)

	_, err = s.Fetch(context.Background(), storage.FetchRequest{
		Source:  "huggingface",
		ModelID: "org/x",
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "huggingface-cli")
}

func TestStore_NewStoreRejectsEmptyDir(t *testing.T) {
	t.Parallel()

	_, err := storage.NewStore("", newTestLogger(), "")
	require.Error(t, err)
}

func TestStore_ErrNotFoundIsSentinel(t *testing.T) {
	t.Parallel()

	s, _ := newStore(t, &fakeFetcher{})
	_, err := s.Get(context.Background(), "nope")
	assert.True(t, errors.Is(err, storage.ErrNotFound))
}
