# syntax=docker/dockerfile:1
#
# staq.ly API — Go backend (REST, MCP, webhook worker, watchers).
#
# Produces a small Alpine image with two static binaries:
#   server      REST API + MCP (Streamable HTTP) + background workers, :3333
#               plus operator subcommands: backup, workspaces, recovery-key,
#               set-plan, connect-link
#   mcp-stdio   MCP over STDIO, started per agent session with
#               `docker exec -i staqly-api mcp-stdio`
#
# Build:  docker build -t staqly-api apps/api
# Data:   everything lives in /data (SQLite database + uploads) — mount a volume.

# ─────────────────────────────────────────────────────────────────────────────
# Stage 1: build
# ─────────────────────────────────────────────────────────────────────────────
FROM golang:1.27-alpine AS build
WORKDIR /src

# Dependencies first, so source changes don't invalidate the module cache layer.
COPY go.mod go.sum ./
RUN --mount=type=cache,target=/go/pkg/mod \
    go mod download

COPY . .

# Version string embedded in the binaries (shown in /health and MCP server info).
ARG VERSION=dev

# modernc.org/sqlite is pure Go, so CGO stays off and the binaries are fully
# static. -trimpath and -s -w keep them reproducible and small.
RUN --mount=type=cache,target=/go/pkg/mod \
    --mount=type=cache,target=/root/.cache/go-build \
    CGO_ENABLED=0 go build -trimpath -ldflags "-s -w -X main.version=${VERSION}" -o /out/server     ./cmd/server && \
    CGO_ENABLED=0 go build -trimpath -ldflags "-s -w -X main.version=${VERSION}" -o /out/mcp-stdio ./cmd/mcp-stdio

# ─────────────────────────────────────────────────────────────────────────────
# Stage 2: runtime
# ─────────────────────────────────────────────────────────────────────────────
FROM alpine:3.22

# ca-certificates: HTTPS for webhooks and watchers
# tzdata:          correct local times in logs if TZ is set
# wget:            used by the HEALTHCHECK below
RUN apk add --no-cache ca-certificates tzdata wget

# Unprivileged user. /data is created here so a fresh named volume inherits
# this ownership on first mount.
RUN adduser -D -H -u 10001 staqly && \
    mkdir -p /data/uploads /data/backups && \
    chown -R staqly:staqly /data

COPY --from=build /out/server /out/mcp-stdio /usr/local/bin/

USER staqly

ENV DATA_DIR=/data \
    ADDR=:3333

VOLUME /data
EXPOSE 3333

HEALTHCHECK --interval=10s --timeout=3s --start-period=5s --retries=5 \
    CMD wget -qO- http://127.0.0.1:3333/health || exit 1

CMD ["server"]
