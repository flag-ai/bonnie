# BONNIE

**Bidirectional Orchestration and Node Interconnect Executor**

BONNIE is a long-running orchestration daemon for the [FLAG platform](https://github.com/flag-ai). It uses [flag-commons](https://github.com/flag-ai/commons) for shared infrastructure (secrets, logging, config, health).

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
# Minimal (env-based secrets, defaults)
export DATABASE_URL="postgres://user:pass@localhost:5432/bonnie?sslmode=disable"
./bonnie

# With OpenBao
export OPENBAO_ADDR="https://bao.example.com"
export OPENBAO_TOKEN="s.xxxxx"
./bonnie
```

## Configuration

| Variable | Default | Description |
|----------|---------|-------------|
| `DATABASE_URL` | *(required)* | PostgreSQL connection string |
| `LISTEN_ADDR` | `:8080` | HTTP listen address |
| `LOG_LEVEL` | `info` | Log level (debug, info, warn, error) |
| `LOG_FORMAT` | `text` | Log format (text, json) |
| `OPENBAO_ADDR` | | OpenBao server address |
| `OPENBAO_TOKEN` | | OpenBao authentication token |

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
