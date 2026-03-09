package container

import (
	"context"
	"io"

	containertypes "github.com/docker/docker/api/types/container"
)

// LogStream reads container logs and writes them to the given writer.
// It blocks until the context is cancelled or the log stream ends.
func LogStream(ctx context.Context, client DockerClient, containerID string, follow bool, w io.Writer) error {
	opts := containertypes.LogsOptions{
		ShowStdout: true,
		ShowStderr: true,
		Follow:     follow,
		Timestamps: true,
	}

	reader, err := client.ContainerLogs(ctx, containerID, opts)
	if err != nil {
		return err
	}
	defer func() { _ = reader.Close() }()

	_, err = io.Copy(w, reader)
	return err
}
