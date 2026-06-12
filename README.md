# go-services

Go service bundle for the Ryzom modernization work.

## Contents

- `cmd/logger` - NeL logger service shim
- `cmd/nats-bridge` - mirror-to-NATS bridge
- `cmd/sheet-api` - PostgreSQL-backed sheet API
- `cmd/gm-api` - GM command API
- `cmd/proxy` - admin reload endpoint used by the current sheet invalidation slice

## Repository Split

`go-services` is the service bundle repo. The WebSocket proxy remains a separate
project in `go-proxy`.

## Build

```bash
go build ./...
```

## Test

```bash
go test ./...
```

## Notes

- The module path is `github.com/boneysan/ryzom/go-services`.
- NATS is optional for some local workflows; services fall back to no-op
  publishing when it is disabled.
