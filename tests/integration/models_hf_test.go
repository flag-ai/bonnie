//go:build integration

package integration_test

import (
	"context"
	"io"
	"log/slog"
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/flag-ai/bonnie/internal/storage"
)

// TestHFFetch_Real downloads a tiny HuggingFace model to verify the real
// huggingfaceFetcher wiring. Skipped unless BONNIE_INTEGRATION_HF=1 so CI
// doesn't hit the HF CDN on every run.
func TestHFFetch_Real(t *testing.T) {
	if os.Getenv("BONNIE_INTEGRATION_HF") != "1" {
		t.Skip("set BONNIE_INTEGRATION_HF=1 to run")
	}

	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	store, err := storage.NewStore(t.TempDir(), logger, os.Getenv("HF_TOKEN"))
	require.NoError(t, err)

	// hf-internal-testing/tiny-random-bert is ~4MB, ideal for CI.
	entry, err := store.Fetch(context.Background(), storage.FetchRequest{
		Source:  "huggingface",
		ModelID: "hf-internal-testing/tiny-random-bert",
	})
	require.NoError(t, err)
	assert.NotEmpty(t, entry.Files)
	assert.Greater(t, entry.SizeBytes, int64(0))
}
