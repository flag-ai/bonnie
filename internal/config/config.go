// Package config provides BONNIE-specific configuration loading.
package config

import (
	"context"
	"fmt"
	"log/slog"
	"strconv"

	"github.com/flag-ai/commons/logging"
	"github.com/flag-ai/commons/secrets"
)

// Config holds all BONNIE configuration.
type Config struct {
	// Component name, always "bonnie".
	Component string

	// LogLevel is the minimum log level (debug, info, warn, error).
	LogLevel string

	// LogFormat is the log output format (text, json).
	LogFormat string

	// ListenAddr is the HTTP listen address.
	ListenAddr string

	// AuthToken is the bearer token for API authentication.
	AuthToken string

	// PollInterval is the GPU polling interval in seconds.
	PollInterval int

	// DockerHost is the Docker daemon socket path.
	DockerHost string
}

// Load builds a BONNIE Config by reading environment variables via the secrets provider.
// Unlike commons config.LoadBase, DATABASE_URL is not required.
func Load(ctx context.Context, provider secrets.Provider) (*Config, error) {
	if provider == nil {
		return nil, fmt.Errorf("config: secrets provider is required")
	}

	authToken, err := provider.Get(ctx, "BONNIE_AUTH_TOKEN")
	if err != nil {
		return nil, fmt.Errorf("config: BONNIE_AUTH_TOKEN is required: %w", err)
	}

	pollStr := provider.GetOrDefault(ctx, "BONNIE_POLL_INTERVAL", "10")
	pollInterval, err := strconv.Atoi(pollStr)
	if err != nil {
		return nil, fmt.Errorf("config: BONNIE_POLL_INTERVAL must be an integer: %w", err)
	}

	return &Config{
		Component:    "bonnie",
		LogLevel:     provider.GetOrDefault(ctx, "LOG_LEVEL", "info"),
		LogFormat:    provider.GetOrDefault(ctx, "LOG_FORMAT", "text"),
		ListenAddr:   provider.GetOrDefault(ctx, "BONNIE_LISTEN_ADDR", ":7777"),
		AuthToken:    authToken,
		PollInterval: pollInterval,
		DockerHost:   provider.GetOrDefault(ctx, "BONNIE_DOCKER_HOST", "unix:///var/run/docker.sock"),
	}, nil
}

// Logger creates a configured logger from the config.
func (c *Config) Logger() *slog.Logger {
	return logging.New(c.Component,
		logging.WithLevel(logging.ParseLevel(c.LogLevel)),
		logging.WithFormat(logging.Format(c.LogFormat)),
	)
}
