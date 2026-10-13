# syntax=docker/dockerfile:1
#
# staq.ly documentation site — static export served by Caddy.
#
# Hosted separately from staq.ly itself (it is NOT part of the root
# compose.yml). The build context is apps/docs; the API reference is generated
# from the OpenAPI contract, which comes in through a BuildKit *named context*
# called `api` (= apps/api). From the repository root:
#
#   docker build --build-context api=apps/api -t staqly-docs apps/docs
#   docker run --rm -p 8090:8090 staqly-docs        # → http://localhost:8090
#
# Output: plain static files (HTML, JS, CSS, search index). You can also copy
# /srv to any static host (S3, Pages, Netlify, Caddy…).

# ─────────────────────────────────────────────────────────────────────────────
# Stage 1: dependencies (cached unless package-lock.json changes)
# ─────────────────────────────────────────────────────────────────────────────
FROM node:24-alpine AS deps
WORKDIR /docs
COPY package.json package-lock.json ./
RUN --mount=type=cache,target=/root/.npm \
    npm ci --no-audit --no-fund

# ─────────────────────────────────────────────────────────────────────────────
# Stage 2: build the static export (next build → out/) and check links
# ─────────────────────────────────────────────────────────────────────────────
FROM node:24-alpine AS build
WORKDIR /docs
ENV NEXT_TELEMETRY_DISABLED=1

# The OpenAPI 3.2 contract from the `api` named context. Locally the build
# reads ../api/openapi.yaml; OPENAPI_SPEC points it here instead.
COPY --from=api openapi.yaml /spec/openapi.yaml
ENV OPENAPI_SPEC=/spec/openapi.yaml

COPY --from=deps /docs/node_modules ./node_modules
COPY . .

# `npm run build` = next build + scripts/check-links.mjs (fails on broken links).
RUN npm run build

# ─────────────────────────────────────────────────────────────────────────────
# Stage 3: serve — Caddy on port 8090 as a non-root user
# ─────────────────────────────────────────────────────────────────────────────
FROM caddy:2-alpine AS runtime

# Unprivileged user; Caddy writes its state under XDG dirs, so point them at
# directories this user owns. Port 8090 needs no extra capabilities.
RUN addgroup -S docs && adduser -S -G docs -H -s /sbin/nologin docs && \
    mkdir -p /home/docs/.local/share /home/docs/.config && \
    chown -R docs:docs /home/docs
ENV XDG_DATA_HOME=/home/docs/.local/share \
    XDG_CONFIG_HOME=/home/docs/.config

# Caddyfile: pretty URLs (/docs/x → /docs/x.html), branded 404, compression,
# cache and security headers, content types for the search index, .md, .yaml.
# auto_https is off: terminate TLS in front (see the Caddyfile to change that).
COPY Caddyfile /etc/caddy/Caddyfile
COPY --from=build /docs/out /srv

USER docs
EXPOSE 8090
HEALTHCHECK --interval=30s --timeout=3s --start-period=5s --retries=3 \
    CMD wget -qO- http://127.0.0.1:8090/ >/dev/null || exit 1
CMD ["caddy", "run", "--config", "/etc/caddy/Caddyfile", "--adapter", "caddyfile"]
