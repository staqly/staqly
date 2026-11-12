#!/usr/bin/env bash
#
# Load demo data through the public API, so events and webhooks fire exactly
# as they would for a real agent.
#
# Usage:
#   make seed                (or: scripts/seed.sh)
#
# Every request needs a key: locally the access token, read from the staqly
# container (`docker exec staqly server token`) when STAQLY_API_KEY is unset.
# Cloud mode:
#   STAQLY_API_URL=https://... STAQLY_API_KEY={API_KEY} scripts/seed.sh
#
# Prints one line per request: HTTP status + what was created. Stops with exit 1 at the
# first request the API refuses. Running it again adds the posts again (collections are kept).

set -euo pipefail
umask 077
cd "$(dirname "$0")/.."

api="${STAQLY_API_URL:-http://localhost:3333}"

# ── Helpers ──────────────────────────────────────────────────────────────────

key="${STAQLY_API_KEY:-}"
if [ -z "$key" ]; then
  key=$(docker exec "${STAQLY_CONTAINER:-staqly}" server token 2>/dev/null || true)
fi
if [ -z "$key" ]; then
  echo "No key: set STAQLY_API_KEY (locally: the access token, \`staqly serve token\`)" >&2
  exit 1
fi

tmp=$(mktemp -d)
trap 'rm -rf "$tmp"' EXIT

# Request headers live in a 0600 file so the key never appears in argv (visible to every
# user via ps). printf is a shell builtin.
headers="$tmp/headers"
printf 'User-Agent: staqly-seed\nAuthorization: Bearer %s\n' "$key" >"$headers"

# request <label> <accepted statuses, e.g. "201|409"> <curl arguments...>
# The response body is left in $tmp/resp.
request() {
  local label=$1 accepted=$2 status
  shift 2

  if ! status=$(curl -sS -o "$tmp/resp" -w '%{http_code}' -H "@$headers" "$@"); then
    echo "$label: no answer from $api" >&2
    exit 1
  fi
  echo "$status $label"

  if ! [[ "$status" =~ ^($accepted)$ ]]; then
    cat "$tmp/resp" >&2
    echo >&2
    exit 1
  fi
}

# collection <name> <description>: 409 = it is already there (an earlier run).
collection() {
  request "collection $1" "201|409" -H "Content-Type: application/json" \
    -X POST "$api/v1/collections" -d "{\"name\": \"$1\", \"description\": \"$2\"}"
}

# post <label> <json>
post() {
  request "$1" "201" -H "Content-Type: application/json" -X POST "$api/v1/posts" -d "$2"
}

# ── Collections ──────────────────────────────────────────────────────────────

collection "Instagram Avatar" "AI avatar posts waiting to be published"
collection "AI Research" "Summaries and findings from research agents"
collection "Automation Results" "Structured output from n8n workflows"

# ── Four avatar drafts, each with its picture ────────────────────────────────

captions=(
  "Golden hour on the rooftop ☀️ #aiavatar"
  "Neon nights in the city 🌃"
  "Slow morning, good coffee ☕"
  "Sunset state of mind 🌅"
)

for i in 0 1 2 3; do
  # reuse=true: a second run does not store the same picture twice.
  request "picture $i" "201|200" -X POST "$api/v1/media?reuse=true" -F "file=@e2e/fixtures/avatar$i.png"
  # shortcut: reads "id" from the API's compact JSON with grep, no jq or python needed.
  media=$(grep -o '"id":"[^"]*"' "$tmp/resp" | head -n 1 | cut -d '"' -f 4)

  if [ -z "$media" ]; then
    echo "picture $i: no media id in the answer" >&2
    exit 1
  fi

  post "post with picture $i" "$(cat <<JSON
{
  "collection": "Instagram Avatar",
  "title": "Avatar post #$((i + 1))",
  "content": "${captions[$i]}",
  "media": ["$media"],
  "tags": ["avatar"],
  "source": "seed",
  "metadata": {"prompt": "photorealistic avatar, scene $i", "model": "mock-higgsfield-v1", "seed": $((1000 + i))}
}
JSON
)"
done

# ── Text drafts ──────────────────────────────────────────────────────────────

post "text post" '{
  "collection": "AI Research",
  "title": "SQLite WAL mode in production",
  "content": "WAL allows concurrent readers with one writer.\nUse VACUUM INTO for online backups.",
  "source": "seed",
  "metadata": {"agent": "research-bot", "confidence": 0.9}
}'

post "post with a link" '{
  "collection": "AI Research",
  "title": "Model Context Protocol specification",
  "content": "Worth a read: https://modelcontextprotocol.io/specification",
  "source": "seed"
}'

post "post with metadata" '{
  "collection": "Automation Results",
  "title": "Nightly trend scan",
  "content": "Trends this week: film grain, pastel, street style.",
  "source": "seed",
  "metadata": {"trends": ["film grain", "pastel", "street style"], "posts_scanned": 120}
}'
