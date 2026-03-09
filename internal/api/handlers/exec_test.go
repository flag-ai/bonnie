package handlers_test

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/flag-ai/bonnie/internal/api/handlers"
	"github.com/flag-ai/bonnie/internal/gpu"
)

func TestExec_MissingCommand(t *testing.T) {
	t.Parallel()

	runner := &gpu.ExecRunner{}
	h := handlers.NewExecHandler(runner, newTestLogger())

	body := `{"args": ["test"]}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/exec", strings.NewReader(body))
	rec := httptest.NewRecorder()

	h.Exec(rec, req)

	assert.Equal(t, http.StatusBadRequest, rec.Code)
}

func TestExec_InvalidBody(t *testing.T) {
	t.Parallel()

	runner := &gpu.ExecRunner{}
	h := handlers.NewExecHandler(runner, newTestLogger())

	req := httptest.NewRequest(http.MethodPost, "/api/v1/exec", strings.NewReader("not json"))
	rec := httptest.NewRecorder()

	h.Exec(rec, req)

	assert.Equal(t, http.StatusBadRequest, rec.Code)
}

func TestExec_Success(t *testing.T) {
	t.Parallel()

	runner := &mockRunner{outputs: map[string]struct {
		data []byte
		err  error
	}{
		"echo": {data: []byte("hello world")},
	}}

	h := handlers.NewExecHandler(runner, newTestLogger())

	body := `{"command": "echo", "args": ["hello", "world"]}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/exec", strings.NewReader(body))
	rec := httptest.NewRecorder()

	h.Exec(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)
	assert.Contains(t, rec.Body.String(), "hello world")
}
