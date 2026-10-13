#!/usr/bin/env bash
# Loads demo data through the public API (so events/webhooks fire normally).
# Usage: scripts/seed.sh   (cloud mode: STAQLY_API_URL=… STAQLY_API_KEY={API_KEY} scripts/seed.sh)
set -euo pipefail
cd "$(dirname "$0")/.."
API="${STAQLY_API_URL:-http://localhost:3333}"
H=(-H "User-Agent: staqly-seed")
[ -n "${STAQLY_API_KEY:-}" ] && H+=(-H "Authorization: Bearer $STAQLY_API_KEY")
J=(-H "Content-Type: application/json")
mk() { curl -s -o /dev/null -w "%{http_code} collection $1\n" "${H[@]}" "${J[@]}" -X POST "$API/v1/collections" -d "{\"name\":\"$1\",\"description\":\"$2\",\"default_view\":\"$3\"}"; }
mk "Instagram Avatar" "AI avatar posts awaiting review" gallery
mk "AI Research" "Summaries and findings from research agents" list
mk "Automation Results" "Structured output from n8n workflows" table

captions=("Golden hour on the rooftop ☀️ #aiavatar" "Neon nights in the city 🌃" "Slow morning, good coffee ☕" "Sunset state of mind 🌅")
for i in 0 1 2 3; do
  curl -s -o /dev/null -w "%{http_code} image $i\n" "${H[@]}" -X POST "$API/v1/collections/Instagram%20Avatar/items" \
    -F "file=@integrations/examples/assets/avatar$i.png" \
    -F "item={\"title\":\"Avatar post #$((i+1))\",\"content\":\"${captions[$i]}\",\"source\":\"seed\",\"metadata\":{\"prompt\":\"photorealistic avatar, scene $i\",\"model\":\"mock-higgsfield-v1\",\"seed\":$((1000+i))}}"
done
curl -s -o /dev/null -w "%{http_code} text\n" "${H[@]}" "${J[@]}" -X POST "$API/v1/collections/AI%20Research/items" \
  -d '{"title":"SQLite WAL mode in production","content":"WAL allows concurrent readers with one writer.\nUse VACUUM INTO for online backups.","source":"seed","metadata":{"agent":"research-bot","confidence":0.9}}'
curl -s -o /dev/null -w "%{http_code} link\n" "${H[@]}" "${J[@]}" -X POST "$API/v1/collections/AI%20Research/items" \
  -d '{"title":"Model Context Protocol specification","url":"https://modelcontextprotocol.io/specification","source":"seed"}'
curl -s -o /dev/null -w "%{http_code} json\n" "${H[@]}" "${J[@]}" -X POST "$API/v1/collections/Automation%20Results/items" \
  -d '{"title":"Nightly trend scan","content":{"trends":["film grain","pastel","street style"],"posts_scanned":120},"status":"completed","source":"seed"}'
