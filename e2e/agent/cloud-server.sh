#!/usr/bin/env bash
# Cloud-mode staqly for the real-agent suite (--env=cloud), built from this checkout: the
# `staqly` binary and mcp-stdio, `staqly serve` in the background with a temporary data
# directory, and a signed-up workspace whose admin key the suite uses.
#
#   eval "$(e2e/agent/cloud-server.sh start)"     # prints the STAQLY_* variables run.mjs needs
#   node e2e/agent/run.mjs --env=cloud --agent=opencode
#   e2e/agent/cloud-server.sh stop                # stops the server, deletes its data
#
# CLOUD_PORT (default 33333; dashboard CLOUD_PORT+5000) and STAQLY_CLOUD_DIR (default
# $TMPDIR/staqly-agent-cloud) pick where it runs. Cloud mode runs on PostgreSQL:
# STAQLY_E2E_DATABASE_URL points at a server; this run gets its own schema. In GitHub Actions
# the variables also go to $GITHUB_ENV. Needs Go, curl and jq.
set -euo pipefail

ROOT=$(cd "$(dirname "$0")/../.." && pwd)
PORT=${CLOUD_PORT:-33333}
DIR=${STAQLY_CLOUD_DIR:-${TMPDIR:-/tmp}/staqly-agent-cloud}
DIR=${DIR%/}
URL=http://127.0.0.1:$PORT

case "${1:-}" in
start)
  : "${STAQLY_E2E_DATABASE_URL:?cloud mode needs PostgreSQL: set STAQLY_E2E_DATABASE_URL=postgres://...}"
  sep='?'; [[ $STAQLY_E2E_DATABASE_URL == *\?* ]] && sep='&'
  DATABASE_URL="${STAQLY_E2E_DATABASE_URL}${sep}search_path=agent_$(date +%s)"
  export DATABASE_URL
  mkdir -p "$DIR/bin" "$DIR/data"
  (cd "$ROOT/apps/cli" && go build -o "$DIR/bin/staqly" ./cmd/staqly)
  (cd "$ROOT/apps/api" && go build -o "$DIR/bin/mcp-stdio" ./cmd/mcp-stdio)
  # webhooks and watchers reach the suite's receiver on this machine's LAN address;
  # signups trust X-Forwarded-For so the suite's extra workspaces are not rate limited
  STAQLY_MODE=cloud AUTH_DEV_SIGNIN=true PUBLIC_API_URL=$URL WEBHOOK_ALLOW_PRIVATE=true WATCHER_ALLOW_PRIVATE=true \
    NOTIFY_ALLOW_PRIVATE=true WEBHOOK_RETRY_BASE=1s TRUST_PROXY=true COOKIE_SECURE=false \
    TAGGER_COMMAND=off TAGGER_URL='' STAQLY_API_KEY='' LOG_LEVEL=info \
    nohup "$DIR/bin/staqly" serve --data "$DIR/data" --port "$PORT" --dashboard-port $((PORT + 5000)) \
    >"$DIR/server.log" 2>&1 &
  echo $! >"$DIR/server.pid"
  for _ in $(seq 1 100); do
    curl -fsS "$URL/health" >/dev/null 2>&1 && break
    sleep 0.2
  done
  curl -fsS "$URL/health" >/dev/null || { cat "$DIR/server.log" >&2; exit 1; }
  key=$(curl -fsS -X POST "$URL/v1/workspaces" -H 'content-type: application/json' \
    -H 'x-forwarded-for: 198.51.100.1' -d '{"name":"agent-e2e"}' | jq -er .api_key.key)
  # mcp-stdio works on the same data with the server's settings: run_watcher fetches from that process
  stdio_env="{\"STAQLY_MODE\":\"cloud\",\"AUTH_DEV_SIGNIN\":\"true\",\"DATABASE_URL\":\"$DATABASE_URL\",\"DATA_DIR\":\"$DIR/data\",\"LOG_LEVEL\":\"warn\",\"WEBHOOK_ALLOW_PRIVATE\":\"true\",\"WATCHER_ALLOW_PRIVATE\":\"true\",\"NOTIFY_ALLOW_PRIVATE\":\"true\"}"
  vars=(
    "STAQLY_CLOUD_URL=$URL"
    "STAQLY_API_KEY=$key"
    "STAQLY_STDIO=[\"$DIR/bin/mcp-stdio\"]"
    "STAQLY_STDIO_ENV=$stdio_env"
    "STAQLY_OPERATOR=[\"$DIR/bin/staqly\",\"serve\",\"--data\",\"$DIR/data\"]"
    "DATABASE_URL=$DATABASE_URL"
  )
  for v in "${vars[@]}"; do
    printf 'export %s=%q\n' "${v%%=*}" "${v#*=}"
    if [ -n "${GITHUB_ENV:-}" ]; then
      echo "$v" >>"$GITHUB_ENV"
    fi
  done
  ;;
stop)
  if [ -f "$DIR/server.pid" ]; then
    kill "$(cat "$DIR/server.pid")" 2>/dev/null || true
  fi
  rm -rf "$DIR"
  ;;
*)
  echo "usage: $0 start|stop" >&2
  exit 64
  ;;
esac
