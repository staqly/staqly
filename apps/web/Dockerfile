# syntax=docker/dockerfile:1
#
# staqly dashboard: the static export of this app, served on :8080 by the
# small Go web server from apps/api (cmd/web), which also streams /api/* to
# the API (API_INTERNAL_URL, read at runtime; default http://api:3333).
# No Node.js at runtime.
#
# Needs a second build context called `api` (= apps/api). From the repo root:
#   docker build --build-context api=apps/api -t staqly-web apps/web
# Run:
#   docker run -e API_INTERNAL_URL=http://api:3333 -p 8080:8080 staqly-web

# ─────────────────────────────────────────────────────────────────────────────
# Stage 1: static export (exactly what package-lock.json says)
# ─────────────────────────────────────────────────────────────────────────────
FROM --platform=$BUILDPLATFORM node:24-alpine AS export
WORKDIR /app
ENV NEXT_TELEMETRY_DISABLED=1

COPY package.json package-lock.json ./
RUN npm ci --no-audit --no-fund

# No build-time URLs: the API address shown in snippets comes from the API's
# PUBLIC_API_URL at runtime (GET /v1/setup), so one pre-built image fits all.
COPY . .
RUN npm run build

# ─────────────────────────────────────────────────────────────────────────────
# Stage 2: the Go web server with the export embedded (cross-compiled)
# ─────────────────────────────────────────────────────────────────────────────
FROM --platform=$BUILDPLATFORM golang:1.27-alpine AS server
WORKDIR /src
ARG VERSION=dev
ARG TARGETOS TARGETARCH

COPY --from=api go.mod go.sum ./
RUN --mount=type=cache,target=/go/pkg/mod go mod download
COPY --from=api . .
COPY --from=export /app/out ./web/

RUN --mount=type=cache,target=/go/pkg/mod \
    --mount=type=cache,target=/root/.cache/go-build \
    CGO_ENABLED=0 GOOS="$TARGETOS" GOARCH="$TARGETARCH" \
    go build -trimpath -ldflags "-s -w -X main.version=${VERSION}" -o /out/web ./cmd/web

# ─────────────────────────────────────────────────────────────────────────────
# Stage 3: runtime (one static binary, unprivileged)
# ─────────────────────────────────────────────────────────────────────────────
FROM scratch
COPY --from=server /out/web /web

# Only a default: compose `environment:` or `-e` wins.
ENV API_INTERNAL_URL=http://api:3333 \
    ADDR=:8080

USER 10001:10001
EXPOSE 8080

# /login is a static page, so this checks the web server itself.
HEALTHCHECK --interval=30s --timeout=3s --start-period=5s --retries=3 \
    CMD ["/web", "health"]

ENTRYPOINT ["/web"]
