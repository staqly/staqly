#!/usr/bin/env bash
# Prepares Staqly for the n8n Instagram Avatar demo:
#   1. creates the "Instagram Avatar" collection (if missing)
#   2. registers a webhook that sends review events to the n8n workflow
# Usage:
#   ./integrations/n8n/setup-demo.sh [n8n-webhook-url]                       # local mode
#   STAQLY_API_URL=https://… STAQLY_API_KEY=stk_... ./integrations/n8n/setup-demo.sh <url>  # cloud mode
# Default webhook URL assumes n8n runs in this compose project (service "n8n").
# n8n on the host machine: http://host.docker.internal:5678/webhook/staqly-demo
set -euo pipefail
API="${STAQLY_API_URL:-http://localhost:3333}"
HOOK_URL="${1:-http://n8n:5678/webhook/staqly-demo}"
auth=(-H "Content-Type: application/json")
[ -n "${STAQLY_API_KEY:-}" ] && auth+=(-H "Authorization: Bearer ${STAQLY_API_KEY}")

code=$(curl -s -o /dev/null -w '%{http_code}' "${auth[@]}" -X POST "$API/v1/collections" \
  -d '{"name":"Instagram Avatar","description":"AI avatar posts awaiting review (n8n demo)","default_view":"gallery"}')
case "$code" in 201) echo "Created collection Instagram Avatar";; 409) echo "Collection Instagram Avatar already exists";; *) echo "Creating collection failed: HTTP $code" >&2; exit 1;; esac
col=$(curl -sf "${auth[@]}" "$API/v1/collections/Instagram%20Avatar" | sed -E 's/^\{"id":"([^"]+)".*/\1/')

resp=$(curl -sf "${auth[@]}" -X POST "$API/v1/webhooks" -d "{
  \"url\": \"$HOOK_URL\",
  \"description\": \"n8n Instagram Avatar demo\",
  \"events\": [\"item.approved\", \"item.rejected\", \"item.regenerate_requested\", \"item.publish_requested\"],
  \"collection_ids\": [\"$col\"]
}")
secret=$(printf '%s' "$resp" | sed -E 's/.*"secret":"([^"]+)".*/\1/')
echo "Registered webhook -> $HOOK_URL"
echo
echo "Paste this secret into the 'Verify & parse' node (CONFIG.webhookSecret) to enable signature checks:"
echo "  $secret"
