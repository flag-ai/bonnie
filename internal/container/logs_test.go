package container_test

import (
	"bytes"
	"context"
	"io"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/flag-ai/bonnie/internal/container"
)

func TestLogStream(t *testing.T) {
	t.Parallel()

	client := &mockDockerClient{
		logsReader: io.NopCloser(strings.NewReader("line 1\nline 2\n")),
	}

	var buf bytes.Buffer
	err := container.LogStream(context.Background(), client, "test-id", false, &buf)
	require.NoError(t, err)
	assert.Equal(t, "line 1\nline 2\n", buf.String())
}

func TestLogStream_Error(t *testing.T) {
	t.Parallel()

	client := &mockDockerClient{
		logsErr: assert.AnError,
	}

	var buf bytes.Buffer
	err := container.LogStream(context.Background(), client, "test-id", false, &buf)
	require.Error(t, err)
}
