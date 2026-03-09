package container

import (
	"context"
	"fmt"
	"log/slog"

	containertypes "github.com/docker/docker/api/types/container"
	networktypes "github.com/docker/docker/api/types/network"

	"github.com/flag-ai/bonnie/internal/gpu"
)

// CreateRequest describes a container to create.
type CreateRequest struct {
	Name    string            `json:"name"`
	Image   string            `json:"image"`
	Env     []string          `json:"env,omitempty"`
	Mounts  []string          `json:"mounts,omitempty"`
	Ports   map[string]string `json:"ports,omitempty"`
	GPU     bool              `json:"gpu"`
	Command []string          `json:"command,omitempty"`
}

// Info is a summary of a container's state.
type Info struct {
	ID      string `json:"id"`
	Name    string `json:"name"`
	Image   string `json:"image"`
	State   string `json:"state"`
	Status  string `json:"status"`
	Created int64  `json:"created"`
}

// Manager manages Docker containers.
type Manager struct {
	client    DockerClient
	gpuVendor gpu.Vendor
	logger    *slog.Logger
}

// NewManager creates a container Manager.
func NewManager(client DockerClient, gpuVendor gpu.Vendor, logger *slog.Logger) *Manager {
	return &Manager{
		client:    client,
		gpuVendor: gpuVendor,
		logger:    logger,
	}
}

// Create creates a new container.
func (m *Manager) Create(ctx context.Context, req CreateRequest) (string, error) {
	config := &containertypes.Config{
		Image: req.Image,
		Env:   req.Env,
		Cmd:   req.Command,
	}

	hostConfig := &containertypes.HostConfig{
		Binds: req.Mounts,
	}

	if req.GPU {
		InjectGPU(hostConfig, m.gpuVendor)
	}

	resp, err := m.client.ContainerCreate(ctx, config, hostConfig, &networktypes.NetworkingConfig{}, nil, req.Name)
	if err != nil {
		return "", fmt.Errorf("create container: %w", err)
	}

	m.logger.Info("container created", "id", shortID(resp.ID), "name", req.Name, "image", req.Image)
	return resp.ID, nil
}

// Start starts a container.
func (m *Manager) Start(ctx context.Context, id string) error {
	if err := m.client.ContainerStart(ctx, id, containertypes.StartOptions{}); err != nil {
		return fmt.Errorf("start container %s: %w", id, err)
	}
	m.logger.Info("container started", "id", shortID(id))
	return nil
}

// Stop stops a container.
func (m *Manager) Stop(ctx context.Context, id string) error {
	if err := m.client.ContainerStop(ctx, id, containertypes.StopOptions{}); err != nil {
		return fmt.Errorf("stop container %s: %w", id, err)
	}
	m.logger.Info("container stopped", "id", shortID(id))
	return nil
}

// Restart restarts a container.
func (m *Manager) Restart(ctx context.Context, id string) error {
	if err := m.client.ContainerRestart(ctx, id, containertypes.StopOptions{}); err != nil {
		return fmt.Errorf("restart container %s: %w", id, err)
	}
	m.logger.Info("container restarted", "id", shortID(id))
	return nil
}

// Remove removes a container.
func (m *Manager) Remove(ctx context.Context, id string, force bool) error {
	if err := m.client.ContainerRemove(ctx, id, containertypes.RemoveOptions{Force: force}); err != nil {
		return fmt.Errorf("remove container %s: %w", id, err)
	}
	m.logger.Info("container removed", "id", shortID(id))
	return nil
}

// Inspect returns detailed container information.
func (m *Manager) Inspect(ctx context.Context, id string) (containertypes.InspectResponse, error) {
	info, err := m.client.ContainerInspect(ctx, id)
	if err != nil {
		return containertypes.InspectResponse{}, fmt.Errorf("inspect container %s: %w", id, err)
	}
	return info, nil
}

// List returns all containers (including stopped).
func (m *Manager) List(ctx context.Context) ([]Info, error) {
	containers, err := m.client.ContainerList(ctx, containertypes.ListOptions{All: true})
	if err != nil {
		return nil, fmt.Errorf("list containers: %w", err)
	}

	result := make([]Info, len(containers))
	for i, c := range containers {
		name := ""
		if len(c.Names) > 0 {
			name = c.Names[0]
		}
		result[i] = Info{
			ID:      c.ID,
			Name:    name,
			Image:   c.Image,
			State:   c.State,
			Status:  c.Status,
			Created: c.Created,
		}
	}

	return result, nil
}

// shortID truncates a container ID to 12 characters for logging.
func shortID(id string) string {
	if len(id) > 12 {
		return id[:12]
	}
	return id
}
