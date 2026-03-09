package handlers_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/flag-ai/bonnie/internal/api/handlers"
)

func TestSystemInfo(t *testing.T) {
	t.Parallel()

	runner := &mockRunner{outputs: map[string]struct {
		data []byte
		err  error
	}{
		"uname": {data: []byte("6.1.0-test\n")},
		"df":    {data: []byte("     1G-blocks      Used     Avail Use%\n          500G      200G      300G  40%\n")},
	}}

	h := handlers.NewSystemHandler(runner, newTestLogger())

	req := httptest.NewRequest(http.MethodGet, "/api/v1/system/info", http.NoBody)
	rec := httptest.NewRecorder()

	h.Info(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)

	var body map[string]any
	err := json.NewDecoder(rec.Body).Decode(&body)
	require.NoError(t, err)
	assert.Contains(t, body, "system")
}
