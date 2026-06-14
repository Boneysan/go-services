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

## GM API Party Routing

`cmd/gm-api` publishes authenticated GM commands to NATS for EGS-side Lua
handlers. Party routing is used by the co-op shard split work to tell the Go
proxy which frontend should receive a party.

| Endpoint | NATS subject | Required JSON | Notes |
| --- | --- | --- | --- |
| `POST /gm/party/frontend` | `gm.set_party_frontend` | `party_id` | `addr` sets the frontend address; an empty `addr` clears the route. |
| `POST /gm/instance/frontend` | `gm.set_instance_frontend` | `instance_id` | `addr` sets the frontend for an AI/world instance; an empty `addr` clears routes for parties assigned to it. |
| `POST /gm/party/instance` | `gm.assign_party_instance` | `party_id`, `instance_id` | Assigns a party to an instance and republishes the route when that instance has a frontend. |
| `POST /gm/party/instance/clear` | `gm.clear_party_instance` | `party_id` | Removes the party's instance assignment and clears its frontend route. |

The commands are accepted asynchronously with `202 Accepted`. Handler tests in
`cmd/gm-api/handlers_test.go` cover required-field validation, subjects, and
envelope command names.

## Notes

- The module path is `github.com/boneysan/ryzom/go-services`.
- NATS is optional for some local workflows; services fall back to no-op
  publishing when it is disabled.
