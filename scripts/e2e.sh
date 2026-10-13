#!/usr/bin/env bash
# End-to-end test against the real Docker stack, in an isolated compose
# project (own containers, ports and volume; your data is untouched).
# Runs the local (keyless) flow, then switches the same data to cloud mode.
# KEEP=1 leaves the stack running afterwards.
set -euo pipefail
cd "$(dirname "$0")/.."

export COMPOSE_PROJECT_NAME=staqly-e2e
export API_PORT=13333 WEB_PORT=18080 WEBHOOK_RETRY_BASE=1s PUBLIC_API_URL=http://localhost:13333
API=http://localhost:$API_PORT
WEB=http://localhost:$WEB_PORT
RECV=staqly-e2e-receiver
TMP=$(mktemp -d)
# Own container names so the test stack can run next to yours.
cat > "$TMP/override.yml" <<'YAML'
services:
  api: { container_name: staqly-e2e-api }
  web: { container_name: staqly-e2e-web }
YAML
export COMPOSE_FILE="compose.yml:$TMP/override.yml"
JAR=$TMP/cookies
J=(-H "Content-Type: application/json")

pass() { printf '  \033[32m✔\033[0m %s\n' "$*"; }
fail() { printf '  \033[31m✘\033[0m %s\n' "$*"; exit 1; }
json() { python3 -c "import sys,json; d=json.load(sys.stdin); print(eval(sys.argv[1]))" "$1"; }
code() { curl -s -o /dev/null -w '%{http_code}' "$@"; }
cleanup() {
  if [ "${KEEP:-}" != "1" ]; then
    docker rm -f "$RECV" >/dev/null 2>&1 || true
    docker compose down -v >/dev/null 2>&1 || true
  fi
  rm -rf "$TMP"
}
trap cleanup EXIT
wait_http() { for _ in $(seq 90); do curl -sf -o /dev/null "$1" && return 0; sleep 1; done; fail "timeout waiting for $1"; }

echo "1. Start the stack with Docker Compose (local mode)"
docker compose down -v >/dev/null 2>&1 || true
docker compose up -d --build api web >/dev/null 2>&1 || { docker compose up -d --build api web; fail "compose up"; }
wait_http "$API/health"; wait_http "$WEB/"
pass "api and web are up"

echo "2. Workspace is created automatically; no keys needed from this machine"
[ "$(curl -s "$WEB/api/v1/setup" | json 'd["mode"]+"/"+str(d["initialized"])')" = "local/True" ] || fail "expected an initialized local workspace"
[ "$(code -X POST "$API/v1/workspaces" "${J[@]}" -d '{}')" = 409 ] || fail "setup must be disabled in local mode"
[ "$(code "$API/v1/collections")" = 200 ] || fail "keyless access"
[ "$(code -X POST "$API/v1/collections" "${J[@]}" -H 'Origin: https://evil.example' -d '{"name":"x"}')" = 403 ] || fail "cross-site request accepted"
[ "$(code -H 'Host: evil.example' "$API/v1/collections")" = 403 ] || fail "DNS-rebinding Host accepted"
pass "keyless locally; foreign Origin and Host refused"

echo "   Sign this browser in (the dashboard is exposed, so browsers need a session)"
[ "$(code "$WEB/api/v1/collections")" = 403 ] || fail "dashboard API without a session must be refused"
LINK=$(docker exec staqly-e2e-api server connect-link 2>/dev/null)
CSRF=$(curl -s -c "$JAR" -X POST "$WEB/api/v1/device-links/redeem" "${J[@]}" -d "{\"token\":\"${LINK#*#}\"}" | json 'd["csrf_token"]')
B=(-b "$JAR" -H "X-CSRF-Token: $CSRF")
[ "$(code "${B[@]}" "$WEB/api/v1/collections")" = 200 ] || fail "signed-in browser"
pass "browser signed in with a one-time link from 'server connect-link'"

echo "3. Create a collection"
COL=$(curl -sf "${J[@]}" -X POST "$API/v1/collections" -d '{"name":"Instagram Avatar"}' | json 'd["id"]')
pass "collection $COL"

echo "   (webhook receiver: fails the first delivery, then accepts)"
docker run -d --name "$RECV" --network staqly-e2e_default python:3.13-alpine python -u -c '
import http.server, json
n = 0
class H(http.server.BaseHTTPRequestHandler):
    def do_POST(self):
        global n; n += 1
        body = self.rfile.read(int(self.headers["Content-Length"])).decode()
        print(json.dumps({"n": n, "headers": dict(self.headers), "body": body}), flush=True)
        self.send_response(500 if n == 1 else 200); self.end_headers()
    def log_message(self, *a): pass
http.server.HTTPServer(("0.0.0.0", 9000), H).serve_forever()' >/dev/null
WH=$(curl -sf "${J[@]}" -X POST "$API/v1/webhooks" -d "{\"url\":\"http://$RECV:9000/hook\",\"events\":[\"item.approved\"]}")
WHID=$(echo "$WH" | json 'd["id"]'); SECRET=$(echo "$WH" | json 'd["secret"]')

echo "4. Submit an item through the REST API"
ITEM=$(curl -sf -X POST "$API/v1/collections/$COL/items" \
  -F "file=@integrations/examples/assets/avatar0.png" \
  -F 'item={"title":"E2E avatar","content":"caption","metadata":{"prompt":"e2e"},"source":"e2e"}' | json 'd["id"]')
MEDIA=$(curl -s "$API/v1/items/$ITEM" | json 'd["media"]["id"]')
pass "item $ITEM"

echo "5. View the item in the dashboard"
[ "$(code "$WEB/items/$ITEM")" = 200 ] || fail "item page"
[ "$(code "$WEB/inbox")" = 200 ] || fail "inbox page"
[ "$(curl -s "${B[@]}" "$WEB/api/v1/items/$ITEM" | json 'd["title"]')" = "E2E avatar" ] || fail "item via proxy"
[ "$(curl -s "${B[@]}" -o /dev/null -w '%{content_type}' "$WEB/api/v1/media/$MEDIA")" = image/png ] || fail "media preview via proxy"
python3 -c 'import sys; sys.stdout.buffer.write(b"\x89PNG\r\n\x1a\n" + b"\0" * (12 << 20))' > "$TMP/big.png"
[ "$(code "${B[@]}" -H "Origin: $WEB" -X POST "$WEB/api/v1/media" -F "file=@$TMP/big.png")" = 201 ] || fail "12 MB upload through the dashboard proxy"
pass "pages render; item, image and a 12 MB upload go through the dashboard proxy"

echo "6. Approve it (as the browser does: session + CSRF, via the proxy)"
STATUS=$(curl -s "${B[@]}" -X POST "$WEB/api/v1/items/$ITEM/actions/approve" "${J[@]}" -H "Origin: $WEB" -d '{"feedback":"ship it"}' | json 'd["status"]')
[ "$STATUS" = approved ] || fail "approve: $STATUS"
[ "$(code -X POST "$API/v1/items/$ITEM/actions/approve" "${J[@]}" -d '{}')" = 409 ] || fail "double approval must conflict"
pass "approved; second decision conflicts"

echo "7. Receive the webhook (with one retry)"
S=""
for _ in $(seq 30); do
  S=$(curl -s "$API/v1/webhook-deliveries?webhook_id=$WHID" | json 'd["data"][0]["status"] if d["data"] else ""')
  [ "$S" = succeeded ] && break; sleep 1
done
[ "$S" = succeeded ] || fail "delivery status: $S"
ATTEMPTS=$(curl -s "$API/v1/webhook-deliveries?webhook_id=$WHID" | json 'd["data"][0]["attempts"]')
[ "$ATTEMPTS" = 2 ] || fail "expected 2 attempts, got $ATTEMPTS"
docker logs "$RECV" | tail -1 | python3 -c '
import sys, json, hmac, hashlib
r = json.loads(sys.stdin.read()); h = {k.lower(): v for k, v in r["headers"].items()}
exp = "sha256=" + hmac.new(sys.argv[1].encode(), (h["x-staqly-timestamp"] + "." + r["body"]).encode(), hashlib.sha256).hexdigest()
assert hmac.compare_digest(exp, h["x-staqly-signature"]), "bad signature"
b = json.loads(r["body"]); assert b["type"] == "item.approved" and b["data"]["item"]["status"] == "approved", b
' "$SECRET" || fail "webhook payload/signature"
pass "delivered after retry, HMAC signature valid"

echo "8. Inspect history"
TYPES=$(curl -s "$API/v1/items/$ITEM/events" | json '",".join(e["type"] for e in d["data"])')
[ "$TYPES" = "item.approved,item.created" ] || fail "history: $TYPES"
pass "history: $TYPES"

echo "9. Connect through MCP (Streamable HTTP and STDIO, keyless)"
MCPH=(-H 'Content-Type: application/json' -H 'Accept: application/json, text/event-stream')
INIT=$(curl -s "${MCPH[@]}" -X POST "$API/mcp" -d '{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-06-18","capabilities":{},"clientInfo":{"name":"e2e","version":"1"}}}')
[ "$(echo "$INIT" | json 'd["result"]["serverInfo"]["name"]')" = staqly ] || fail "mcp initialize: $INIT"
NTOOLS=$(curl -s "${MCPH[@]}" -X POST "$API/mcp" -d '{"jsonrpc":"2.0","id":2,"method":"tools/list"}' | json 'len(d["result"]["tools"])')
[ "$NTOOLS" = 21 ] || fail "tools: $NTOOLS"
pass "HTTP: initialize ok, $NTOOLS tools"

echo "10. Create more items through MCP"
R=$(curl -s "${MCPH[@]}" -X POST "$API/mcp" -d '{"jsonrpc":"2.0","id":3,"method":"tools/call","params":{"name":"create_item","arguments":{"collection":"Instagram Avatar","title":"via MCP HTTP","content":"hello"}}}')
[ "$(echo "$R" | json 'd["result"]["structuredContent"]["status"]')" = pending ] || fail "mcp http create: $R"
OUT=$( (printf '%s\n' \
  '{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-06-18","capabilities":{},"clientInfo":{"name":"e2e","version":"1"}}}' \
  '{"jsonrpc":"2.0","method":"notifications/initialized"}' \
  '{"jsonrpc":"2.0","id":2,"method":"tools/call","params":{"name":"create_item","arguments":{"collection":"Instagram Avatar","title":"via MCP stdio"}}}'; sleep 2) \
  | docker exec -i staqly-e2e-api mcp-stdio)
echo "$OUT" | grep -q '"id":2,"result"' || fail "mcp stdio create: $OUT"
GATE=$(curl -s "${MCPH[@]}" -X POST "$API/mcp" -d '{"jsonrpc":"2.0","id":5,"method":"tools/call","params":{"name":"create_item","arguments":{"collection":"Instagram Avatar","title":"gate me"}}}' | json 'd["result"]["structuredContent"]["id"]')
( sleep 1; curl -s -o /dev/null "${J[@]}" -X POST "$API/v1/items/$GATE/actions/approve" -d '{}' ) &
DEC=$(curl -s "${MCPH[@]}" -X POST "$API/mcp" -d "{\"jsonrpc\":\"2.0\",\"id\":6,\"method\":\"tools/call\",\"params\":{\"name\":\"await_decision\",\"arguments\":{\"item_id\":\"$GATE\",\"timeout_seconds\":20}}}" | json 'str(d["result"]["structuredContent"]["decided"])+"/"+d["result"]["structuredContent"]["item"]["status"]')
wait
[ "$DEC" = "True/approved" ] || fail "await_decision: $DEC"
pass "created via HTTP and STDIO; await_decision returned when a human approved"

echo "    Full-text search"
[ "$(curl -s "$API/v1/items?q=avat" | json 'd["data"][0]["title"]')" = "E2E avatar" ] || fail "prefix search"
pass "search 'avat' finds 'E2E avatar'"

echo "    Connect a device (phone on the LAN, through the dashboard proxy)"
PHONE=(-H "Host: 192.168.1.20:$WEB_PORT")
[ "$(code "${PHONE[@]}" "$WEB/api/v1/collections")" = 403 ] || fail "unconnected phone must be refused"
LINK=$(curl -s "${B[@]}" -X POST "$WEB/api/v1/device-links" "${J[@]}" -H "Origin: $WEB" -d '{}')
TOKEN=$(echo "$LINK" | json 'd["url"].split("#",1)[1]')
PCSRF=$(curl -s -c "$TMP/phone" "${PHONE[@]}" -X POST "$WEB/api/v1/device-links/redeem" "${J[@]}" -d "{\"token\":\"$TOKEN\"}" | json 'd["csrf_token"]')
[ "$(code -b "$TMP/phone" "${PHONE[@]}" "$WEB/api/v1/collections")" = 200 ] || fail "connected phone read"
[ "$(code -b "$TMP/phone" "${PHONE[@]}" -H "X-CSRF-Token: $PCSRF" -X POST "$WEB/api/v1/items/$ITEM/actions/archive" "${J[@]}" -d '{}')" = 200 ] || fail "connected phone action"
[ "$(code "${PHONE[@]}" -X POST "$WEB/api/v1/device-links/redeem" "${J[@]}" -d "{\"token\":\"$TOKEN\"}")" = 410 ] || fail "QR link must be single-use"
pass "phone refused → QR link → session → reads and acts; link single-use"

echo "11. Persistence across restart and container recreation"
BEFORE=$(curl -s "$API/v1/items" | json 'len(d["data"])')
docker compose restart api web >/dev/null 2>&1
wait_http "$API/health"
docker compose up -d --build --force-recreate api web >/dev/null 2>&1
wait_http "$API/health"; wait_http "$WEB/"
AFTER=$(curl -s "$API/v1/items" | json 'len(d["data"])')
[ "$BEFORE" = "$AFTER" ] && [ "$AFTER" = 4 ] || fail "items before=$BEFORE after=$AFTER"
[ "$(code "$API/v1/media/$MEDIA")" = 200 ] || fail "media lost"
pass "$AFTER items and media survive restart + recreation"

echo "12. Serve the same data in cloud mode (keys required)"
STAQLY_MODE=cloud SETUP_TOKEN=e2e-token docker compose up -d --force-recreate api >/dev/null 2>&1
wait_http "$API/health"
[ "$(code "$API/v1/collections")" = 401 ] || fail "cloud mode must require credentials"
RECOVERY=$(docker exec staqly-e2e-api server recovery-key)
CSRF=$(curl -s -c "$JAR" -X POST "$API/v1/sessions/recover" "${J[@]}" -d "{\"recovery_key\":\"$RECOVERY\"}" | json 'd["csrf_token"]')
[ "$(code -b "$JAR" -X POST "$API/v1/api-keys" "${J[@]}" -d '{"name":"x","scopes":["items:read"]}')" = 403 ] || fail "CSRF must be enforced"
KEY=$(curl -s -b "$JAR" -H "X-CSRF-Token: $CSRF" -X POST "$API/v1/api-keys" "${J[@]}" -d '{"name":"agent","scopes":["collections:read","items:read","items:write"]}' | json 'd["key"]')
A=(-H "Authorization: Bearer $KEY")
[ "$(curl -s "${A[@]}" "$API/v1/items" | json 'len(d["data"])')" = 4 ] || fail "data not visible with key"
[ "$(code "${A[@]}" -X POST "$API/v1/items/$ITEM/actions/reject" "${J[@]}" -d '{}')" = 403 ] || fail "creator key must not review"
DENIED=$(curl -s "${A[@]}" "${MCPH[@]}" -X POST "$API/mcp" -d "{\"jsonrpc\":\"2.0\",\"id\":4,\"method\":\"tools/call\",\"params\":{\"name\":\"approve_item\",\"arguments\":{\"item_id\":\"$ITEM\"}}}" | json 'd["result"].get("isError")')
[ "$DENIED" = True ] || fail "MCP scope not enforced"
[ "$(code "${MCPH[@]}" -X POST "$API/mcp" -d '{"jsonrpc":"2.0","id":5,"method":"tools/list"}')" = 401 ] || fail "keyless MCP in cloud mode"
pass "recovery key via CLI → session → scoped key; scopes enforced over REST and MCP"

echo "13. Cloud signup: new isolated workspace on the Free plan, limits enforced"
[ "$(code -X POST "$API/v1/workspaces" "${J[@]}" -d '{"name":"Nope"}')" = 403 ] || fail "signup without SETUP_TOKEN must be refused"
NEW=$(curl -s -X POST "$API/v1/workspaces" "${J[@]}" -d '{"name":"Second","setup_token":"e2e-token"}')
[ "$(echo "$NEW" | json 'd["workspace"]["plan"]')" = free ] || fail "signup: $NEW"
NK=(-H "Authorization: Bearer $(echo "$NEW" | json 'd["api_key"]["key"]')")
[ "$(curl -s "${NK[@]}" "$API/v1/items" | json 'len(d["data"])')" = 0 ] || fail "new workspace sees other data"
curl -sf "${NK[@]}" "${J[@]}" -X POST "$API/v1/collections" -d '{"name":"Q"}' >/dev/null
for i in $(seq 100); do curl -sf -o /dev/null "${NK[@]}" "${J[@]}" -X POST "$API/v1/collections/Q/items" -d "{\"title\":\"$i\"}" || fail "item $i"; done
OVER=$(curl -s "${NK[@]}" "${J[@]}" -X POST "$API/v1/collections/Q/items" -d '{"title":"101"}')
[ "$(echo "$OVER" | json 'd["error"]["code"]')" = plan_limit_exceeded ] || fail "limit: $OVER"
WSID=$(echo "$NEW" | json 'd["workspace"]["id"]')
docker exec staqly-e2e-api server set-plan "$WSID" indie >/dev/null
[ "$(code "${NK[@]}" "${J[@]}" -X POST "$API/v1/collections/Q/items" -d '{"title":"101"}')" = 201 ] || fail "after upgrade"
pass "Free plan stops at 100 items/month (402); operator upgrade to Indie lifts it"

echo
echo "E2E passed."
