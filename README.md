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
  -X github.com/flag-ai/commons/version.Version=$(cat VERSION) \
  -X github.com/flag-ai/commons/version.Commit=$(git rev-parse --short HEAD) \
  -X github.com/flag-ai/commons/version.Date=$(date -u +%Y-%m-%d)" \
  ./cmd/bonnie
```

The current version lives in the top-level `VERSION` file. Bump it per
[semver](https://semver.org) for every release (patch for fixes, minor for
features, major for breaking changes).

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
| `BONNIE_MODEL_STORAGE_DIR` | `/var/lib/bonnie/models` | On-disk cache for staged models |
| `BONNIE_HF_TOKEN` | | HuggingFace token for gated models (falls back to `HF_TOKEN`) |
| `HF_TOKEN` | | Standard HuggingFace token env var (fallback) |
| `LOG_LEVEL` | `info` | Log level (debug, info, warn, error) |
| `LOG_FORMAT` | `text` | Log format (text, json) |
| `OPENBAO_ADDR` | | OpenBao server address (optional) |
| `OPENBAO_TOKEN` | | OpenBao authentication token (optional) |

Model-storage and benchmark endpoints require [`huggingface-cli`](https://huggingface.co/docs/huggingface_hub/guides/cli) to be installed and on `PATH` for HuggingFace sources. NFS sources (`source: "nfs"`) use an already-mounted share — BONNIE does not mount filesystems itself.

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

### Models

On-disk cache of staged model artifacts. Used by DEVON to place models on a
BONNIE host and by KITT to reference them during benchmark runs.

| Method | Path | Description |
|--------|------|-------------|
| `GET` | `/api/v1/models` | List staged models |
| `POST` | `/api/v1/models/fetch` | Stage a model (idempotent) |
| `DELETE` | `/api/v1/models/{id}` | Remove a staged model |

**Fetch request (HuggingFace):**
```json
{
  "source": "huggingface",
  "model_id": "Qwen/Qwen2.5-7B-Instruct",
  "patterns": ["*.safetensors", "*.json"]
}
```

**Fetch request (pre-mounted NFS share):**
```json
{
  "source": "nfs",
  "model_id": "Qwen/Qwen2.5-7B-Instruct",
  "mount_source": "/mnt/models",
  "subpath": "qwen/qwen2.5-7b-instruct"
}
```

**Fetch response 200:**
```json
{
  "id": "a1b2c3d4e5f60718",
  "source": "huggingface",
  "model_id": "Qwen/Qwen2.5-7B-Instruct",
  "path": "/var/lib/bonnie/models/a1b2c3d4e5f60718",
  "size_bytes": 15234567890,
  "files": ["config.json", "model-00001-of-00004.safetensors", "..."],
  "fetched_at": "2026-04-16T14:30:00Z",
  "last_used_at": "2026-04-16T14:30:00Z"
}
```

Fetch is idempotent: repeat calls for the same `(source, model_id)` pair
return the existing entry and bump `last_used_at`. Concurrent fetches of the
same model are deduplicated so only one download happens.

**List response 200:** array of the entry objects above.

**Delete response:** `204 No Content` on success, `404 Not Found` if the id is
unknown.

### Benchmark

Runs a paired engine + benchmark container set on a private Docker network
with shared results volume, streaming progress via SSE.

| Method | Path | Description |
|--------|------|-------------|
| `POST` | `/api/v1/benchmark` | Run a paired engine+benchmark container set |

**Request body:**
```json
{
  "run_id": "01HXYZ...",
  "timeout_seconds": 3600,
  "engine": {
    "image": "vllm/vllm-openai:latest",
    "args": ["--model", "/model", "--host", "0.0.0.0"],
    "env": {"HF_HOME": "/model"},
    "ports": [8000],
    "model_path": "/var/lib/bonnie/models/a1b2c3d4e5f60718",
    "health_check": {"path": "/health", "port": 8000, "timeout_seconds": 300}
  },
  "benchmark": {
    "kind": "container",
    "image": "ghcr.io/flag-ai/kitt-humaneval:1.0",
    "args": ["--num-samples", "50"],
    "env": {},
    "config": {"temperature": 0.2}
  }
}
```

`benchmark.kind` is either `"yaml"` (BONNIE writes `benchmark.yaml_spec` to
`/config.yaml` inside the container before start) or `"container"` (BONNIE
writes `benchmark.config` to `/config.json`). The benchmark container
receives `ENGINE_URL=http://<engine-ip>:<port>` as an environment variable
and must write its final results to `/results/out.json`.

**Response:** `text/event-stream` of `data: <json>\n\n` frames. Event shape:

```json
{
  "type": "status | progress | result | error",
  "phase": "creating-network | starting-engine | engine-healthy | ...",
  "source": "orchestrator | engine | benchmark",
  "line": "raw log line (type=progress)",
  "timestamp": "2026-04-16T14:30:00Z",
  "results": {...},
  "duration_ms": 42000,
  "error": "..."
}
```

The final event is `{"type":"result","phase":"done","results":{...},"duration_ms":...}`.
Engine and benchmark containers, the run network, and the results volume are
always cleaned up on return, even on timeout or client disconnect.

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
