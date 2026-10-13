# syntax=docker/dockerfile:1
#
# staq.ly dashboard — Next.js standalone server on :8080.
#
# The browser only talks to this server; requests to /api/* are proxied to the
# Go API. Because Next.js resolves rewrites at BUILD time, the API address is a
# build argument (compose passes http://api:3333).
#
# Build:  docker build --build-arg API_INTERNAL_URL=http://api:3333 -t staqly-web apps/web

# ─────────────────────────────────────────────────────────────────────────────
# Stage 1: dependencies (exactly what package-lock.json says)
# ─────────────────────────────────────────────────────────────────────────────
FROM node:26-alpine AS deps
WORKDIR /app

# libc6-compat: some native optional deps expect glibc symbols on Alpine.
RUN apk add --no-cache libc6-compat

COPY package.json package-lock.json ./
RUN npm ci --no-audit --no-fund

# ─────────────────────────────────────────────────────────────────────────────
# Stage 2: build the standalone server
# ─────────────────────────────────────────────────────────────────────────────
FROM node:26-alpine AS build
WORKDIR /app

# Where the dashboard proxies /api/* (baked into the build).
ARG API_INTERNAL_URL=http://api:3333
# Optional public API URL shown in integration snippets.
ARG NEXT_PUBLIC_API_URL=

ENV API_INTERNAL_URL=${API_INTERNAL_URL} \
    NEXT_PUBLIC_API_URL=${NEXT_PUBLIC_API_URL} \
    NEXT_TELEMETRY_DISABLED=1

COPY --from=deps /app/node_modules ./node_modules
COPY . .
RUN npm run build

# ─────────────────────────────────────────────────────────────────────────────
# Stage 3: runtime (no node_modules, no sources — just the standalone output)
# ─────────────────────────────────────────────────────────────────────────────
FROM node:26-alpine AS runner
WORKDIR /app

ENV NODE_ENV=production \
    NEXT_TELEMETRY_DISABLED=1 \
    HOSTNAME=0.0.0.0 \
    PORT=8080

# Unprivileged user.
RUN addgroup -S -g 1001 nodejs && \
    adduser  -S -u 1001 -G nodejs nextjs

# standalone = server.js + the minimal node_modules it needs; static assets
# and public/ are not included in it and must be copied alongside.
COPY --from=build --chown=nextjs:nodejs /app/.next/standalone ./
COPY --from=build --chown=nextjs:nodejs /app/.next/static     ./.next/static
COPY --from=build --chown=nextjs:nodejs /app/public           ./public

USER nextjs
EXPOSE 8080

# /login renders without an API round trip, so this checks Next.js itself.
HEALTHCHECK --interval=30s --timeout=3s --start-period=10s --retries=3 \
    CMD wget -qO- http://127.0.0.1:8080/login >/dev/null || exit 1

CMD ["node", "server.js"]
