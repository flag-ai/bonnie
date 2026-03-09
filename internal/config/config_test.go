package config_test

import (
	"context"
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/flag-ai/bonnie/internal/config"
)

// mockProvider implements secrets.Provider for testing.
type mockProvider struct {
	values map[string]string
}

func (m *mockProvider) Get(_ context.Context, key string) (string, error) {
	v, ok := m.values[key]
	if !ok {
		return "", fmt.Errorf("key not found: %s", key)
	}
	return v, nil
}

func (m *mockProvider) GetOrDefault(_ context.Context, key, defaultVal string) string {
	if v, ok := m.values[key]; ok {
		return v
	}
	return defaultVal
}

func TestLoad_Defaults(t *testing.T) {
	t.Parallel()

	provider := &mockProvider{values: map[string]string{
		"BONNIE_AUTH_TOKEN": "test-token",
	}}

	cfg, err := config.Load(context.Background(), provider)
	require.NoError(t, err)

	assert.Equal(t, "bonnie", cfg.Component)
	assert.Equal(t, "info", cfg.LogLevel)
	assert.Equal(t, "text", cfg.LogFormat)
	assert.Equal(t, ":7777", cfg.ListenAddr)
	assert.Equal(t, "test-token", cfg.AuthToken)
	assert.Equal(t, 10, cfg.PollInterval)
	assert.Equal(t, "unix:///var/run/docker.sock", cfg.DockerHost)
}

func TestLoad_CustomValues(t *testing.T) {
	t.Parallel()

	provider := &mockProvider{values: map[string]string{
		"BONNIE_AUTH_TOKEN":    "my-token",
		"LOG_LEVEL":            "debug",
		"LOG_FORMAT":           "json",
		"BONNIE_LISTEN_ADDR":   ":9999",
		"BONNIE_POLL_INTERVAL": "30",
		"BONNIE_DOCKER_HOST":   "tcp://localhost:2375",
	}}

	cfg, err := config.Load(context.Background(), provider)
	require.NoError(t, err)

	assert.Equal(t, "debug", cfg.LogLevel)
	assert.Equal(t, "json", cfg.LogFormat)
	assert.Equal(t, ":9999", cfg.ListenAddr)
	assert.Equal(t, "my-token", cfg.AuthToken)
	assert.Equal(t, 30, cfg.PollInterval)
	assert.Equal(t, "tcp://localhost:2375", cfg.DockerHost)
}

func TestLoad_MissingAuthToken(t *testing.T) {
	t.Parallel()

	provider := &mockProvider{values: map[string]string{}}

	_, err := config.Load(context.Background(), provider)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "BONNIE_AUTH_TOKEN")
}

func TestLoad_InvalidPollInterval(t *testing.T) {
	t.Parallel()

	provider := &mockProvider{values: map[string]string{
		"BONNIE_AUTH_TOKEN":    "token",
		"BONNIE_POLL_INTERVAL": "not-a-number",
	}}

	_, err := config.Load(context.Background(), provider)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "BONNIE_POLL_INTERVAL")
}

func TestLoad_ZeroPollInterval(t *testing.T) {
	t.Parallel()

	provider := &mockProvider{values: map[string]string{
		"BONNIE_AUTH_TOKEN":    "token",
		"BONNIE_POLL_INTERVAL": "0",
	}}

	_, err := config.Load(context.Background(), provider)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "BONNIE_POLL_INTERVAL")
}

func TestLoad_NilProvider(t *testing.T) {
	t.Parallel()

	_, err := config.Load(context.Background(), nil)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "secrets provider is required")
}

func TestConfig_Logger(t *testing.T) {
	t.Parallel()

	provider := &mockProvider{values: map[string]string{
		"BONNIE_AUTH_TOKEN": "token",
	}}

	cfg, err := config.Load(context.Background(), provider)
	require.NoError(t, err)

	logger := cfg.Logger()
	assert.NotNil(t, logger)
}
