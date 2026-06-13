# go-services/Dockerfile — builds the Go microservices for the dev shard (Phase 1+)
# Usage from project root: docker compose -f docker-compose.dev.yml build logger nats-bridge ...
FROM golang:1.22-alpine AS builder

WORKDIR /src
# Copy go mod first for caching
COPY go.mod go.sum* ./
RUN go mod download

COPY . .
RUN mkdir -p /out \
 && for svc in logger nats-bridge sheet-api gm-api campaign-api quest-compiler proxy; do \
      echo "building $svc"; \
      go build -o /out/$svc ./cmd/$svc/; \
    done

# Minimal runtime image
FROM alpine:3.19
RUN apk add --no-cache ca-certificates tzdata wget \
 && adduser -D -u 1000 ryzom
WORKDIR /app
COPY --from=builder /out/* /app/
USER ryzom
# Default to logger; override command per service in compose
CMD ["/app/logger"]
