# go-services/Dockerfile — builds the Go microservices for the dev shard (Phase 1+)
# Usage from project root: docker compose -f docker-compose.dev.yml build logger nats-bridge ...
FROM golang:1.22-alpine AS builder

# g++ is needed to cgo-compile the vendored Recast/Detour C++ sources used
# by navmesh-bake and pathfinding-api (Task 6.2).
RUN apk add --no-cache g++
ENV CGO_ENABLED=1

WORKDIR /src
# Copy go mod first for caching
COPY go.mod go.sum* ./
RUN go mod download

COPY . .
RUN mkdir -p /out \
 && for svc in logger nats-bridge sheet-api gm-api campaign-api quest-compiler navmesh-bake pathfinding-api; do \
      echo "building $svc"; \
      go build -o /out/$svc ./cmd/$svc/; \
    done

# Minimal runtime image
FROM alpine:3.19
# libstdc++/libgcc: runtime deps of navmesh-bake/pathfinding-api, which
# cgo-link the vendored Recast/Detour C++ code (Task 6.2).
RUN apk add --no-cache ca-certificates tzdata wget libstdc++ libgcc \
 && adduser -D -u 1000 ryzom
WORKDIR /app
COPY --from=builder /out/* /app/
# Synthetic placeholder navmesh (real Atys terrain unavailable — see
# PROGRESS.md Task 6.2) so pathfinding-api has something to serve by default.
COPY --from=builder /src/cmd/navmesh-bake/testdata/*.navmesh /app/navmeshes/
USER ryzom
# Default to logger; override command per service in compose
CMD ["/app/logger"]
