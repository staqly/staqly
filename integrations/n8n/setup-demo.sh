#!/usr/bin/env bash
# Prepares Staqly for the n8n Instagram Avatar demo:
#   1. creates the "Instagram Avatar" collection (if missing)
#   2. registers a webhook that sends review events to the n8n workflow
#   3. prints the webhook signing secret and a random demo token
# Usage:
#   ./integrations/n8n/setup-demo.sh [n8n-webhook-url]                       # local mode
#   STAQLY_API_URL=https://… STAQLY_API_KEY=stk_... ./integrations/n8n/setup-demo.sh <url>  # cloud mode
# Default webhook URL assumes n8n runs on the staqly Docker network (service "n8n").
# n8n on the host machine: http://host.docker.internal:5678/webhook/staqly-demo
# Needs curl and either jq or python3.
set -euo pipefail
umask 077
API="${STAQLY_API_URL:-http://localhost:3333}"
API="${API%/}"
HOOK_URL="${1:-http://n8n:5678/webhook/staqly-demo}"

if command -v jq >/dev/null 2>&1; then
  JSON=jq
elif command -v python3 >/dev/null 2>&1; then
  JSON=python3
else
  echo "setup-demo.sh needs jq or python3 to build and parse JSON" >&2
  exit 1
fi

tmp=$(mktemp -d)
trap 'rm -rf "$tmp"' EXIT

# Request headers live in a 0600 file so the API key never appears in argv
# (visible to every user via ps). printf is a shell builtin.
headers="$tmp/headers"
printf 'Content-Type: application/json\n' >"$headers"
if [ -n "${STAQLY_API_KEY:-}" ]; then
  printf 'Authorization: Bearer %s\n' "$STAQLY_API_KEY" >>"$headers"
fi

# api METHOD PATH [BODY_FILE] → response body in $tmp/resp, prints the HTTP status.
api() {
  local method=$1 path=$2 body=${3:-}
  local args=(-sS -o "$tmp/resp" -w '%{http_code}' -H "@$headers" -X "$method")
  [ -n "$body" ] && args+=(--data-binary "@$body")
  curl "${args[@]}" "$API$path"
}

# json_get FIELD < json → top-level string field (empty if missing).
json_get() {
  if [ "$JSON" = jq ]; then
    jq -r --arg f "$1" '.[$f] // empty'
  else
    python3 -c 'import json,sys; v=json.load(sys.stdin).get(sys.argv[1]); print("" if v is None else v)' "$1"
  fi
}

# Collection (values are constants, but built the same way as below).
if [ "$JSON" = jq ]; then
  jq -n '{name: "Instagram Avatar", description: "AI avatar posts awaiting review (n8n demo)", default_view: "gallery"}' >"$tmp/collection.json"
else
  python3 -c 'import json; print(json.dumps({"name": "Instagram Avatar", "description": "AI avatar posts awaiting review (n8n demo)", "default_view": "gallery"}))' >"$tmp/collection.json"
fi
code=$(api POST /v1/collections "$tmp/collection.json")
case "$code" in
  201) echo "Created collection Instagram Avatar" ;;
  409) echo "Collection Instagram Avatar already exists" ;;
  *) echo "Creating collection failed: HTTP $code" >&2; cat "$tmp/resp" >&2; echo >&2; exit 1 ;;
esac

code=$(api GET "/v1/collections/Instagram%20Avatar")
[ "$code" = 200 ] || { echo "Reading collection failed: HTTP $code" >&2; exit 1; }
col=$(json_get id <"$tmp/resp")
[ -n "$col" ] || { echo "Unexpected response: collection has no id" >&2; exit 1; }

# Webhook body: untrusted values (URL, IDs) are passed as data, never spliced into JSON text.
if [ "$JSON" = jq ]; then
  jq -n --arg url "$HOOK_URL" --arg col "$col" '{
    url: $url,
    description: "n8n Instagram Avatar demo",
    events: ["item.approved", "item.rejected", "item.regenerate_requested", "item.publish_requested"],
    collection_ids: [$col]
  }' >"$tmp/webhook.json"
else
  HOOK_URL="$HOOK_URL" COL="$col" python3 -c 'import json, os; print(json.dumps({
    "url": os.environ["HOOK_URL"],
    "description": "n8n Instagram Avatar demo",
    "events": ["item.approved", "item.rejected", "item.regenerate_requested", "item.publish_requested"],
    "collection_ids": [os.environ["COL"]],
  }))' >"$tmp/webhook.json"
fi
code=$(api POST /v1/webhooks "$tmp/webhook.json")
[ "$code" = 201 ] || [ "$code" = 200 ] || { echo "Registering webhook failed: HTTP $code" >&2; cat "$tmp/resp" >&2; echo >&2; exit 1; }
secret=$(json_get secret <"$tmp/resp")
[ -n "$secret" ] || { echo "Unexpected response: webhook has no secret" >&2; exit 1; }

# Random token that protects the "Generate via webhook" trigger.
if command -v openssl >/dev/null 2>&1; then
  token=$(openssl rand -hex 24)
else
  token=$(python3 -c 'import secrets; print(secrets.token_hex(24))' 2>/dev/null || od -An -N24 -tx1 /dev/urandom | tr -d ' \n')
fi

echo "Registered webhook -> $HOOK_URL"
echo
echo "1. Paste the webhook secret into the 'Verify & parse' node (CONFIG.webhookSecret)."
echo "   Until you do, the node rejects every event:"
echo "     $secret"
echo "2. Paste the demo token into the 'Check demo token' node (DEMO_TOKEN):"
echo "     $token"
echo "   Then trigger generation over HTTP with:"
echo "     curl -X POST http://localhost:5678/webhook/staqly-demo-generate -H 'X-Demo-Token: $token'"
