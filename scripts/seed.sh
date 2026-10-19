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
# Prints one line per request: HTTP status + what was created.

set -euo pipefail
cd "$(dirname "$0")/.."

api="${STAQLY_API_URL:-http://localhost:3333}"

# ── Helpers ──────────────────────────────────────────────────────────────────

auth=(-H "User-Agent: staqly-seed")

key="${STAQLY_API_KEY:-}"
if [ -z "$key" ]; then
  key=$(docker exec "${STAQLY_CONTAINER:-staqly}" server token 2>/dev/null || true)
fi
if [ -z "$key" ]; then
  echo "No key: set STAQLY_API_KEY (locally: the access token, \`staqly serve token\`)" >&2
  exit 1
fi
auth+=(-H "Authorization: Bearer $key")

# post_json <path> <label> <json>
post_json() {
  curl -s -o /dev/null -w "%{http_code} $2\n" "${auth[@]}" \
    -H "Content-Type: application/json" \
    -X POST "$api$1" -d "$3"
}

# post_file <path> <label> <file> <item json>
post_file() {
  curl -s -o /dev/null -w "%{http_code} $2\n" "${auth[@]}" \
    -X POST "$api$1" -F "file=@$3" -F "item=$4"
}

# ── Collections ──────────────────────────────────────────────────────────────

post_json /v1/collections "collection Instagram Avatar" \
  '{"name": "Instagram Avatar", "description": "AI avatar posts awaiting review", "default_view": "gallery"}'

post_json /v1/collections "collection AI Research" \
  '{"name": "AI Research", "description": "Summaries and findings from research agents", "default_view": "list"}'

post_json /v1/collections "collection Automation Results" \
  '{"name": "Automation Results", "description": "Structured output from n8n workflows", "default_view": "table"}'

# ── Images: four avatar posts waiting for review ─────────────────────────────

captions=(
  "Golden hour on the rooftop ☀️ #aiavatar"
  "Neon nights in the city 🌃"
  "Slow morning, good coffee ☕"
  "Sunset state of mind 🌅"
)

for i in 0 1 2 3; do
  item=$(cat <<JSON
{
  "title": "Avatar post #$((i + 1))",
  "content": "${captions[$i]}",
  "source": "seed",
  "metadata": {"prompt": "photorealistic avatar, scene $i", "model": "mock-higgsfield-v1", "seed": $((1000 + i))}
}
JSON
)
  post_file "/v1/collections/Instagram%20Avatar/items" "image $i" "e2e/fixtures/avatar$i.png" "$item"
done

# ── Text, link and JSON items ────────────────────────────────────────────────

post_json "/v1/collections/AI%20Research/items" "text" '{
  "title": "SQLite WAL mode in production",
  "content": "WAL allows concurrent readers with one writer.\nUse VACUUM INTO for online backups.",
  "source": "seed",
  "metadata": {"agent": "research-bot", "confidence": 0.9}
}'

post_json "/v1/collections/AI%20Research/items" "link" '{
  "title": "Model Context Protocol specification",
  "url": "https://modelcontextprotocol.io/specification",
  "source": "seed"
}'

post_json "/v1/collections/Automation%20Results/items" "json" '{
  "title": "Nightly trend scan",
  "content": {"trends": ["film grain", "pastel", "street style"], "posts_scanned": 120},
  "status": "approved",
  "source": "seed"
}'
