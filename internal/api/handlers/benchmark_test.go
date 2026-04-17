package handlers_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/flag-ai/bonnie/internal/api/handlers"
	"github.com/flag-ai/bonnie/internal/container"
)

type fakeRunner struct {
	events []container.PairedRunEvent
	err    error
	// block, if non-nil, forces PairedRun to wait until closed before
	// sending events — simulating a long-running benchmark.
	block chan struct{}
}

//nolint:gocritic // hugeParam: signature matches PairedRunner interface.
func (f *fakeRunner) PairedRun(ctx context.Context, _ container.PairedRunSpec, events chan<- container.PairedRunEvent) error {
	defer close(events)
	if f.block != nil {
		select {
		case <-f.block:
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	for i := range f.events {
		select {
		case events <- f.events[i]:
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	return f.err
}

func TestBenchmark_StreamsEvents(t *testing.T) {
	t.Parallel()

	runner := &fakeRunner{
		events: []container.PairedRunEvent{
			{Type: "status", Phase: "starting-engine", Source: "orchestrator", Timestamp: time.Now().UTC()},
			{Type: "progress", Source: "engine", Line: "hello", Timestamp: time.Now().UTC()},
			{Type: "result", Phase: "done", Results: json.RawMessage(`{"ok":true}`), DurationMs: 42, Timestamp: time.Now().UTC()},
		},
	}
	h := handlers.NewBenchmarkHandler(runner, newTestLogger())

	body := `{"run_id":"r1","engine":{"image":"e"},"benchmark":{"image":"b","kind":"container"}}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/benchmark", strings.NewReader(body))
	rec := httptest.NewRecorder()

	h.Run(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)
	assert.Equal(t, "text/event-stream", rec.Header().Get("Content-Type"))

	frames := parseSSEFrames(t, rec.Body.String())
	require.Len(t, frames, 3)
	assert.Equal(t, "status", frames[0].Type)
	assert.Equal(t, "starting-engine", frames[0].Phase)
	assert.Equal(t, "progress", frames[1].Type)
	assert.Equal(t, "hello", frames[1].Line)
	assert.Equal(t, "result", frames[2].Type)
	assert.JSONEq(t, `{"ok":true}`, string(frames[2].Results))
}

func TestBenchmark_InvalidBody(t *testing.T) {
	t.Parallel()

	h := handlers.NewBenchmarkHandler(&fakeRunner{}, newTestLogger())
	req := httptest.NewRequest(http.MethodPost, "/api/v1/benchmark", strings.NewReader("not json"))
	rec := httptest.NewRecorder()

	h.Run(rec, req)
	assert.Equal(t, http.StatusBadRequest, rec.Code)
}

func TestBenchmark_MissingFields(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name string
		body string
		want string
	}{
		{
			name: "missing run_id",
			body: `{"engine":{"image":"e"},"benchmark":{"image":"b"}}`,
			want: "run_id is required",
		},
		{
			name: "invalid run_id characters",
			body: `{"run_id":"r/../evil","engine":{"image":"e"},"benchmark":{"image":"b"}}`,
			want: "invalid",
		},
		{
			name: "missing engine.image",
			body: `{"run_id":"r","benchmark":{"image":"b"}}`,
			want: "engine.image is required",
		},
		{
			name: "missing benchmark.image",
			body: `{"run_id":"r","engine":{"image":"e"}}`,
			want: "benchmark.image is required",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			h := handlers.NewBenchmarkHandler(&fakeRunner{}, newTestLogger())
			req := httptest.NewRequest(http.MethodPost, "/api/v1/benchmark", strings.NewReader(tc.body))
			rec := httptest.NewRecorder()
			h.Run(rec, req)
			assert.Equal(t, http.StatusBadRequest, rec.Code)
			assert.Contains(t, rec.Body.String(), tc.want)
		})
	}
}

// parseSSEFrames decodes a stream of `data: <json>\n\n` frames back into
// PairedRunEvent values for assertion.
func parseSSEFrames(t *testing.T, body string) []container.PairedRunEvent {
	t.Helper()
	var out []container.PairedRunEvent
	for _, raw := range strings.Split(body, "\n\n") {
		raw = strings.TrimSpace(raw)
		if raw == "" {
			continue
		}
		if !strings.HasPrefix(raw, "data: ") {
			continue
		}
		payload := strings.TrimPrefix(raw, "data: ")
		var ev container.PairedRunEvent
		require.NoError(t, json.Unmarshal([]byte(payload), &ev))
		out = append(out, ev)
	}
	return out
}
