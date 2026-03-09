# BONNIE

**Bidirectional Orchestration and Node Interconnect Executor**

BONNIE is a GPU host agent for the [FLAG platform](https://github.com/flag-ai). It runs as a long-lived daemon on GPU nodes, providing hardware detection, container lifecycle management, and a REST API for orchestration. It uses [flag-commons](https://github.com/flag-ai/commons) for shared infrastructure (secrets, logging, config, health).

## Features

- **GPU Detection** — auto-detects NVIDIA, AMD, and Intel GPUs with vendor-specific tooling
- **GPU Metrics Polling** — continuously polls GPU utilization, memory, and temperature
- **Container Management** — full Docker container lifecycle (create, start, stop, restart, remove, logs) with automatic GPU device passthrough
- **REST API** — Chi-based HTTP API with bearer token authentication
- **Health Checks** — liveness and readiness endpoints with Docker connectivity checks
- **System Info** — host system information (OS, kernel, memory, CPU)

## Build

```bash
go build -ldflags "\
  -X github.com/flag-ai/commons/version.Version=0.1.0 \
  -X github.com/flag-ai/commons/version.Commit=$(git rev-parse --short HEAD) \
  -X github.com/flag-ai/commons/version.Date=$(date -u +%Y-%m-%d)" \
  ./cmd/bonnie
```

## Run

```bash
# Minimal (env-based secrets)
export BONNIE_AUTH_TOKEN="your-secret-token"
./bonnie

# With OpenBao for secrets
export OPENBAO_ADDR="https://bao.example.com"
export OPENBAO_TOKEN="s.xxxxx"
./bonnie
```

## Install (Linux)

An install script is provided for systemd-based Linux systems:

```bash
curl -fsSL https://raw.githubusercontent.com/flag-ai/bonnie/main/scripts/install.sh | bash
```

This downloads the latest release binary, creates a `bonnie` service user, and sets up a systemd unit. See `scripts/install.sh` for details.

## Configuration

All configuration is via environment variables (or OpenBao secrets).

| Variable | Default | Description |
|----------|---------|-------------|
| `BONNIE_AUTH_TOKEN` | *(required)* | Bearer token for API authentication |
| `BONNIE_LISTEN_ADDR` | `:7777` | HTTP listen address |
| `BONNIE_POLL_INTERVAL` | `10` | GPU metrics polling interval in seconds |
| `BONNIE_DOCKER_HOST` | `unix:///var/run/docker.sock` | Docker daemon socket |
| `LOG_LEVEL` | `info` | Log level (debug, info, warn, error) |
| `LOG_FORMAT` | `text` | Log format (text, json) |
| `OPENBAO_ADDR` | | OpenBao server address (optional) |
| `OPENBAO_TOKEN` | | OpenBao authentication token (optional) |

See `.env.example` for a template.

## API

All `/api/v1/*` endpoints require a `Authorization: Bearer <token>` header. Health and metrics endpoints are unauthenticated.

### Health & Metrics

| Method | Path | Description |
|--------|------|-------------|
| `GET` | `/health` | Liveness check |
| `GET` | `/ready` | Readiness check (includes Docker connectivity) |
| `GET` | `/metrics` | GPU metrics (Prometheus-compatible) |

### System

| Method | Path | Description |
|--------|------|-------------|
| `GET` | `/api/v1/system/info` | Host system information |

### GPU

| Method | Path | Description |
|--------|------|-------------|
| `GET` | `/api/v1/gpu/status` | Current GPU status and details |
| `GET` | `/api/v1/gpu/metrics` | GPU metrics snapshot |

### Containers

| Method | Path | Description |
|--------|------|-------------|
| `GET` | `/api/v1/containers` | List containers |
| `POST` | `/api/v1/containers` | Create a container (with GPU passthrough) |
| `GET` | `/api/v1/containers/{id}` | Inspect a container |
| `POST` | `/api/v1/containers/{id}/start` | Start a container |
| `POST` | `/api/v1/containers/{id}/stop` | Stop a container |
| `POST` | `/api/v1/containers/{id}/restart` | Restart a container |
| `DELETE` | `/api/v1/containers/{id}` | Remove a container |
| `GET` | `/api/v1/containers/{id}/logs` | Stream container logs |

### Exec

| Method | Path | Description |
|--------|------|-------------|
| `POST` | `/api/v1/exec` | Execute a command on the host |

## Test

```bash
go test -race ./...
```

## Lint

```bash
golangci-lint run ./...
```

## License

Apache 2.0 — see [LICENSE](LICENSE).
