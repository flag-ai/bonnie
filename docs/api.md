# BONNIE API Reference

Base URL: `http://<host>:7777`

## Authentication

All `/api/v1/*` endpoints require a Bearer token:

```
Authorization: Bearer <BONNIE_AUTH_TOKEN>
```

Health, readiness, and metrics endpoints are unauthenticated.

Unauthorized requests receive:
```json
{"error": "unauthorized"}
```

---

## Health & Metrics

### GET /health

Liveness check. Runs all registered health checkers.

**Response 200:**
```json
{
  "healthy": true,
  "version": "0.1.0 (commit: abc123, built: 2026-03-09)",
  "checks": [
    {"name": "docker", "healthy": true, "latency_ms": 2}
  ]
}
```

**Response 503:** Same format, `"healthy": false`.

### GET /ready

Readiness check. Verifies Docker socket connectivity.

**Response 200:**
```json
{"ready": true}
```

**Response 503:**
```json
{"ready": false, "error": "connection refused"}
```

### GET /metrics

GPU metrics in Prometheus text exposition format.

**Response 200:**
```
# HELP bonnie_gpu_memory_total_mib Total GPU memory in MiB.
# TYPE bonnie_gpu_memory_total_mib gauge
bonnie_gpu_memory_total_mib{index="0",name="NVIDIA RTX 4090",vendor="nvidia"} 24564

# HELP bonnie_gpu_memory_free_mib Free GPU memory in MiB.
# TYPE bonnie_gpu_memory_free_mib gauge
bonnie_gpu_memory_free_mib{index="0",name="NVIDIA RTX 4090",vendor="nvidia"} 22000

# HELP bonnie_gpu_utilization_percent GPU utilization percentage.
# TYPE bonnie_gpu_utilization_percent gauge
bonnie_gpu_utilization_percent{index="0",name="NVIDIA RTX 4090",vendor="nvidia"} 35

# HELP bonnie_gpu_count Total number of GPUs detected.
# TYPE bonnie_gpu_count gauge
bonnie_gpu_count{vendor="nvidia"} 1
```

---

## System

### GET /api/v1/system/info

Returns host system information and disk usage.

**Response 200:**
```json
{
  "system": {
    "hostname": "gpu-node-01",
    "os": "linux",
    "arch": "amd64",
    "kernel": "6.1.0-18-amd64",
    "cpu_model": "Intel(R) Core(TM) i9-13900K",
    "cpu_cores": 24,
    "memory_mb": 65536
  },
  "disk": {
    "total_gb": 500,
    "used_gb": 200,
    "available_gb": 300,
    "used_percent": "40%"
  }
}
```

---

## GPU

### GET /api/v1/gpu/status

Returns the latest GPU detection snapshot.

**Response 200:**
```json
{
  "vendor": "nvidia",
  "gpus": [
    {
      "index": 0,
      "name": "NVIDIA GeForce RTX 4090",
      "vendor": "nvidia",
      "memory_total_mib": 24564,
      "memory_free_mib": 22000,
      "utilization_percent": 35
    }
  ],
  "timestamp": "2026-03-09T12:00:00Z"
}
```

**CPU-only response:**
```json
{
  "vendor": "none",
  "gpus": null,
  "timestamp": "2026-03-09T12:00:00Z"
}
```

### GET /api/v1/gpu/metrics

Same as `/metrics` but within the authenticated API group.

---

## Containers

### GET /api/v1/containers

List all containers (including stopped).

**Response 200:**
```json
[
  {
    "id": "abc123def456...",
    "name": "/my-container",
    "image": "ubuntu:latest",
    "state": "running",
    "status": "Up 5 minutes",
    "created": 1709985600
  }
]
```

### POST /api/v1/containers

Create a new container.

**Request body:**
```json
{
  "name": "my-gpu-job",
  "image": "nvidia/cuda:12.0-base",
  "env": ["MODEL_PATH=/data/model"],
  "mounts": ["/host/data:/data:ro"],
  "gpu": true,
  "command": ["python", "train.py"]
}
```

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `name` | string | No | Container name |
| `image` | string | **Yes** | Docker image |
| `env` | string[] | No | Environment variables |
| `mounts` | string[] | No | Bind mounts (host:container:mode) |
| `gpu` | bool | No | Enable GPU passthrough |
| `command` | string[] | No | Override container CMD |

**Response 201:**
```json
{"id": "abc123def456..."}
```

**Response 400:**
```json
{"error": "image is required"}
```

### GET /api/v1/containers/{id}

Inspect a container. Returns full Docker inspect response.

**Response 200:** Docker ContainerJSON object.

**Response 404:**
```json
{"error": "inspect container abc123: No such container"}
```

### POST /api/v1/containers/{id}/start

Start a stopped container.

**Response 200:**
```json
{"status": "started"}
```

### POST /api/v1/containers/{id}/stop

Stop a running container.

**Response 200:**
```json
{"status": "stopped"}
```

### POST /api/v1/containers/{id}/restart

Restart a container.

**Response 200:**
```json
{"status": "restarted"}
```

### DELETE /api/v1/containers/{id}

Force-remove a container.

**Response 204:** No content.

### GET /api/v1/containers/{id}/logs

Stream container logs via Server-Sent Events (SSE).

**Headers:**
```
Content-Type: text/event-stream
Cache-Control: no-cache
Connection: keep-alive
```

**Event stream:**
```
data: 2026-03-09T12:00:00.000Z stdout: Starting application...

data: 2026-03-09T12:00:01.000Z stdout: Listening on :8080

```

The connection stays open until the client disconnects or the container stops.

---

## Exec

### POST /api/v1/exec

Execute a command on the host and stream output via SSE.

**Request body:**
```json
{
  "command": "nvidia-smi",
  "args": ["--query-gpu=name", "--format=csv,noheader"]
}
```

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `command` | string | **Yes** | Command to execute |
| `args` | string[] | No | Command arguments |

**Response (SSE stream):**
```
data: NVIDIA GeForce RTX 4090

event: done
data: {"exit_code": 0}

```

**Error response (SSE):**
```
data: {"error": "exec: \"nonexistent\": executable file not found in $PATH"}
```

---

## Error Responses

All error responses use:
```json
{"error": "description of the error"}
```

| Status | Meaning |
|--------|---------|
| 400 | Bad request (invalid JSON, missing required field) |
| 401 | Unauthorized (missing or invalid Bearer token) |
| 404 | Resource not found |
| 500 | Internal server error |
| 503 | Service unavailable (health/ready check failed) |
