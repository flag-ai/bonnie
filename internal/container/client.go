// Package container provides Docker container management for BONNIE.
package container

import (
	"context"
	"io"

	"github.com/docker/docker/api/types"
	containertypes "github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/image"
	networktypes "github.com/docker/docker/api/types/network"
	ocispec "github.com/opencontainers/image-spec/specs-go/v1"
)

// DockerClient abstracts the Docker SDK methods used by BONNIE.
type DockerClient interface {
	ContainerCreate(ctx context.Context, config *containertypes.Config, hostConfig *containertypes.HostConfig, networkingConfig *networktypes.NetworkingConfig, platform *ocispec.Platform, containerName string) (containertypes.CreateResponse, error)
	ContainerStart(ctx context.Context, containerID string, options containertypes.StartOptions) error
	ContainerStop(ctx context.Context, containerID string, options containertypes.StopOptions) error
	ContainerRestart(ctx context.Context, containerID string, options containertypes.StopOptions) error
	ContainerRemove(ctx context.Context, containerID string, options containertypes.RemoveOptions) error
	ContainerInspect(ctx context.Context, containerID string) (containertypes.InspectResponse, error)
	ContainerList(ctx context.Context, options containertypes.ListOptions) ([]containertypes.Summary, error)
	ContainerLogs(ctx context.Context, containerID string, options containertypes.LogsOptions) (io.ReadCloser, error)
	ImagePull(ctx context.Context, refStr string, options image.PullOptions) (io.ReadCloser, error)
	Ping(ctx context.Context) (types.Ping, error)
	Close() error
}
