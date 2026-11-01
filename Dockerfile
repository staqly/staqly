# syntax=docker/dockerfile:1
#
# staqly: one image, one process. The API (REST + MCP), the background workers
# and the dashboard (embedded static export) run in `server`, like `staqly serve`.
#
#   :3333  API + MCP (publish it on 127.0.0.1 only: -p 127.0.0.1:3333:3333)
#   :8080  dashboard (streams /api/* to the API in the same process)
#   /data  SQLite database + uploads: mount a volume
#
# Released pre-built as ghcr.io/staqly/staqly:<version> (and :latest); nobody
# needs to build it. From a checkout (build context = the repository root):
#   docker build -t staqly .
#
# Binaries in the image:
#   server      the server, plus operator commands: token [--rotate] (local mode's
#               access token: `docker exec staqly server token`), backup, workspaces,
#               recovery-key, set-plan, stats, connect-link
#   mcp-stdio   MCP over STDIO: `docker exec -i staqly mcp-stdio`

# ─────────────────────────────────────────────────────────────────────────────
# Stage 1: dashboard static export (only the dashboard's slice of the pnpm
# workspace, exactly what pnpm-lock.yaml says)
# ─────────────────────────────────────────────────────────────────────────────
FROM --platform=$BUILDPLATFORM node:26.13.0-alpine AS node
ENV NEXT_TELEMETRY_DISABLED=1 PNPM_HOME=/pnpm
WORKDIR /repo
# pnpm (packageManager in package.json) fetched once into its own layer, not on every source change
COPY package.json ./
# Node 26 no longer bundles Corepack. Install the exact version declared by the workspace.
RUN npm install --global "$(node -p 'require("./package.json").packageManager')"

FROM node AS prune
COPY . .
# the turbo version in pnpm-lock.yaml
RUN pnpm dlx turbo@2.11.10 prune @staqly/platform --docker

FROM node AS export
COPY --from=prune /repo/out/json/ .
RUN --mount=type=cache,id=pnpm,target=/pnpm/store pnpm install --frozen-lockfile
# No build-time URLs: the API address shown in snippets comes from the API's
# PUBLIC_API_URL at runtime (GET /v1/setup), so one pre-built image fits all.
COPY --from=prune /repo/out/full/ .
RUN pnpm --filter @staqly/platform build

# ─────────────────────────────────────────────────────────────────────────────
# Stage 2: Go binaries with the export embedded. Runs on the build machine's
# architecture and cross-compiles (amd64/arm64): much faster than QEMU.
# ─────────────────────────────────────────────────────────────────────────────
FROM --platform=$BUILDPLATFORM golang:1.27-alpine AS build
WORKDIR /src
ARG VERSION=dev
ARG TARGETOS TARGETARCH

COPY apps/api/go.mod apps/api/go.sum ./
RUN --mount=type=cache,target=/go/pkg/mod go mod download
COPY apps/api/ .
RUN find web -mindepth 1 ! -name .gitkeep -delete
COPY --from=export /repo/apps/platform/out ./web/

# modernc.org/sqlite is pure Go: CGO off, fully static binaries.
RUN --mount=type=cache,target=/go/pkg/mod \
    --mount=type=cache,target=/root/.cache/go-build \
    export CGO_ENABLED=0 GOOS="$TARGETOS" GOARCH="$TARGETARCH" && \
    go build -trimpath -ldflags "-s -w -X main.version=${VERSION}" -o /out/server    ./cmd/server && \
    go build -trimpath -ldflags "-s -w -X main.version=${VERSION}" -o /out/mcp-stdio ./cmd/mcp-stdio

# ─────────────────────────────────────────────────────────────────────────────
# Stage 3: runtime
# ─────────────────────────────────────────────────────────────────────────────
FROM alpine:3.24

# ca-certificates: HTTPS for webhooks, watchers and hosted models
# tzdata:          local times in logs when TZ is set
# The HEALTHCHECK below uses BusyBox's wget, already in the base image.
# Unprivileged user; /data is created here so a fresh named volume inherits it.
RUN apk add --no-cache ca-certificates tzdata && \
    adduser -D -H -u 10001 staqly && \
    mkdir -p /data/uploads /data/backups && \
    chown -R staqly:staqly /data

COPY --from=build /out/server /out/mcp-stdio /usr/local/bin/

USER staqly

ENV DATA_DIR=/data \
    ADDR=:3333 \
    DASHBOARD_ADDR=:8080

VOLUME /data
EXPOSE 3333 8080

HEALTHCHECK --interval=10s --timeout=3s --start-period=5s --retries=5 \
    CMD wget -qO- http://127.0.0.1:3333/health && wget -qO /dev/null http://127.0.0.1:8080/login || exit 1

CMD ["server"]
