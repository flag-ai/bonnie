# BONNIE Architecture

## Overview

BONNIE (Bidirectional Orchestration and Node Interconnect Executor) is a GPU host agent in the FLAG platform. It runs as a long-lived daemon on GPU nodes, exposing hardware capabilities and container management via a REST API. KARR and KITT delegate GPU workloads to BONNIE instances.

## Component Diagram

```
┌─────────────────────────────────────────────────┐
│                   BONNIE                         │
│                                                  │
│  ┌──────────┐  ┌───────────┐  ┌──────────────┐  │
│  │  Config   │  │  Secrets  │  │   Logger     │  │
│  │  Loader   │  │  Provider │  │   (slog)     │  │
│  └────┬─────┘  └─────┬─────┘  └──────┬───────┘  │
│       │               │               │          │
│  ┌────▼───────────────▼───────────────▼───────┐  │
│  │              Chi HTTP Router               │  │
│  │  ┌─────────┐ ┌────────┐ ┌──────────────┐  │  │
│  │  │  Auth   │ │Logging │ │  Recovery     │  │  │
│  │  │  MW     │ │  MW    │ │  MW           │  │  │
│  │  └─────────┘ └────────┘ └──────────────┘  │  │
│  │                                            │  │
│  │  /health  /ready  /metrics                 │  │
│  │  /api/v1/gpu/*  /api/v1/containers/*       │  │
│  │  /api/v1/system/*  /api/v1/exec            │  │
│  └────────────────────────────────────────────┘  │
│       │               │               │          │
│  ┌────▼─────┐  ┌──────▼──────┐  ┌────▼───────┐  │
│  │   GPU    │  │  Container  │  │  System    │  │
│  │ Detector │  │  Manager    │  │  Info      │  │
│  │ + Poller │  │  + Runtime  │  │            │  │
│  └────┬─────┘  └──────┬──────┘  └────────────┘  │
│       │                │                         │
└───────┼────────────────┼─────────────────────────┘
        │                │
   nvidia-smi        Docker SDK
   rocm-smi          /var/run/docker.sock
   xpu-smi
```

## Package Responsibilities

| Package | Path | Responsibility |
|---------|------|----------------|
| `config` | `internal/config` | BONNIE-specific config loading via secrets provider |
| `gpu` | `internal/gpu` | GPU detection, vendor parsers, polling, CommandRunner interface |
| `container` | `internal/container` | Docker client interface, container CRUD, GPU runtime injection, log streaming |
| `system` | `internal/system` | Host system information collection |
| `api` | `internal/api` | Chi router wiring |
| `handlers` | `internal/api/handlers` | HTTP request handlers |
| `middleware` | `internal/api/middleware` | Auth, logging, panic recovery |

## Data Flow

### GPU Detection

1. `Detector.Detect()` tries vendor tools in order: NVIDIA → AMD → Intel
2. Each vendor parser invokes the CLI tool via `CommandRunner` interface
3. Parses stdout (CSV for NVIDIA, JSON for AMD, CSV for Intel)
4. Returns a `Snapshot` with all detected GPUs
5. `Poller` re-runs detection on a configurable interval
6. Subscribers receive snapshots via buffered channels (used for SSE)

### Container Lifecycle

1. API handler receives `CreateRequest` (image, env, mounts, GPU flag)
2. `Manager.Create()` builds Docker container config
3. If GPU=true, `InjectGPU()` modifies HostConfig based on detected vendor:
   - NVIDIA: DeviceRequests with `gpu` capability
   - AMD: `/dev/kfd` + `/dev/dri` device mounts, video/render groups
   - Intel: `/dev/dri` device mount, video/render groups
4. Delegates to Docker SDK for actual container operations

### Request Flow

1. Request enters Chi router
2. Recovery middleware catches panics
3. Logging middleware records method, path, status, duration
4. Auth middleware validates Bearer token (skips /health, /ready, /metrics)
5. Route-matched handler processes request
6. JSON response (or SSE stream for logs/exec)

## Technology Stack

- **Language:** Go 1.24+
- **Router:** Chi v5
- **Docker:** Docker SDK v28
- **Shared library:** flag-commons (secrets, logging, config, health, version)
- **Secrets:** OpenBao with env var fallback
- **Logging:** stdlib log/slog via flag-commons
- **CI:** GitHub Actions (lint, test, security)

## Bootstrap Flow

1. Register signal handler (SIGINT, SIGTERM)
2. Create secrets provider (OpenBao → env fallback)
3. Load BONNIE config via custom `config.Load()` (no DATABASE_URL required)
4. Create structured logger
5. Detect GPUs, start poller goroutine
6. Create Docker client and container manager
7. Create health registry with Docker socket checker
8. Build Chi router with all handlers and middleware
9. Start HTTP server
10. Block until signal or server error
11. Graceful shutdown (10s timeout)

## Branch Protection

- PRs required to merge to main (0 approvals — solo dev)
- Required status checks: Lint, Test, Security
- No force push, no branch deletion
- Admin bypass enabled
