#!/usr/bin/env bash
#
# End-to-end test against the real Docker stack.
#
# Runs in an isolated compose project (own containers, ports and volume), so
# your own staqly data is never touched. It first exercises local (keyless)
# mode, then switches the same data to cloud mode.
#
# Usage:
#   make test-e2e            (or: scripts/e2e.sh)
#   KEEP=1 scripts/e2e.sh    leave the test stack running afterwards
#   E2E_BUILD= scripts/e2e.sh  use prebuilt staqly-e2e-{api,web}:test images (CI)
#
# Requires: docker, curl, python3.

set -euo pipefail
cd "$(dirname "$0")/.."

# ══ Configuration ════════════════════════════════════════════════════════════

export COMPOSE_PROJECT_NAME=staqly-e2e
export API_PORT=13333
export WEB_PORT=18080
export WEBHOOK_RETRY_BASE=1s                  # retry a failed webhook after 1 s, not minutes
export PUBLIC_API_URL=http://localhost:13333
BUILD=${E2E_BUILD---build}                    # empty = images are prebuilt

API=http://localhost:$API_PORT                # the API, as a local agent sees it
WEB=http://localhost:$WEB_PORT                # the dashboard and its /api proxy
API_CONTAINER=staqly-e2e-api
RECEIVER=staqly-e2e-receiver                  # fake webhook endpoint

TMP=$(mktemp -d)
COOKIES=$TMP/cookies

# Own container and image names, so the test stack runs next to yours and its
# local builds never replace the released ghcr.io images on this machine.
cat > "$TMP/override.yml" <<'YAML'
services:
  api: { container_name: staqly-e2e-api, image: "staqly-e2e-api:test" }
  web: { container_name: staqly-e2e-web, image: "staqly-e2e-web:test" }
YAML
export COMPOSE_FILE="compose.yml:$TMP/override.yml"

# ══ Helpers ══════════════════════════════════════════════════════════════════

JSON=(-H "Content-Type: application/json")
MCP=(-H "Content-Type: application/json" -H "Accept: application/json, text/event-stream")
MCP_INIT='{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-06-18","capabilities":{},"clientInfo":{"name":"e2e","version":"1"}}}'

step() { printf '\n%s\n' "$*"; }
pass() { printf '  \033[32m✔\033[0m %s\n' "$*"; }
fail() { printf '  \033[31m✘\033[0m %s\n' "$*"; exit 1; }

# json <python expression>: read JSON from stdin as `d`, print the expression.
#   echo '{"a":[1,2]}' | json 'len(d["a"])'   → 2
json() { python3 -c 'import sys, json; d = json.load(sys.stdin); print(eval(sys.argv[1]))' "$1"; }

# status <curl args…>: print only the HTTP status code.
status() { curl -s -o /dev/null -w '%{http_code}' "$@"; }

# expect <wanted status> <message> <curl args…>
expect() {
  local want=$1 msg=$2
  shift 2

  local got
  got=$(status "$@")

  if [ "$got" != "$want" ]; then
    fail "$msg (expected $want, got $got)"
  fi
}

# wait_http <url>: wait up to 90 s for the URL to answer 2xx.
wait_http() {
  for _ in $(seq 90); do
    if curl -sf -o /dev/null "$1"; then
      return 0
    fi

    sleep 1
  done

  fail "timeout waiting for $1"
}

# mcp <json-rpc body> [extra curl args…]: call the Streamable HTTP endpoint.
mcp() {
  local body=$1
  shift

  curl -s "${MCP[@]}" "$@" -X POST "$API/mcp" -d "$body"
}

# mcp_tool <id> <tool> <arguments json>: JSON-RPC body for tools/call.
mcp_tool() {
  printf '{"jsonrpc":"2.0","id":%s,"method":"tools/call","params":{"name":"%s","arguments":%s}}' "$1" "$2" "$3"
}

cleanup() {
  if [ "${KEEP:-}" != "1" ]; then
    docker rm -f "$RECEIVER" >/dev/null 2>&1 || true
    docker compose down -v >/dev/null 2>&1 || true
  fi

  rm -rf "$TMP"
}
trap cleanup EXIT

# ══ 1. Start ═════════════════════════════════════════════════════════════════

step "1. Start the stack with Docker Compose (local mode)"

# Leftovers of a previous KEEP=1 run.
docker rm -f "$RECEIVER" >/dev/null 2>&1 || true
docker compose down -v >/dev/null 2>&1 || true

if ! docker compose up -d $BUILD api web >/dev/null 2>&1; then
  docker compose up -d $BUILD api web # run again, visibly, to show the error
  fail "compose up"
fi

wait_http "$API/health"
wait_http "$WEB/"
pass "api and web are up"

# ══ 2. Local mode guards ═════════════════════════════════════════════════════
# Local mode has one workspace and no keys, but only for this machine: a
# foreign Origin (CSRF) or Host (DNS rebinding) must be refused.

step "2. Workspace is created automatically; no keys needed from this machine"

setup=$(curl -s "$WEB/api/v1/setup" | json 'd["mode"] + "/" + str(d["initialized"])')
[ "$setup" = "local/True" ] || fail "expected an initialized local workspace, got $setup"

expect 409 "setup must be disabled in local mode" -X POST "$API/v1/workspaces" "${JSON[@]}" -d '{}'
expect 200 "keyless access" "$API/v1/collections"
expect 403 "cross-site request accepted" -X POST "$API/v1/collections" "${JSON[@]}" -H 'Origin: https://evil.example' -d '{"name":"x"}'
expect 403 "DNS-rebinding Host accepted" -H 'Host: evil.example' "$API/v1/collections"
pass "keyless locally; foreign Origin and Host refused"

# The dashboard is published on the network, so a browser needs a session,
# created from a one-time link (`server connect-link`).

step "   Sign this browser in"

expect 403 "dashboard API without a session must be refused" "$WEB/api/v1/collections"

link=$(docker exec "$API_CONTAINER" server connect-link 2>/dev/null)
csrf=$(curl -s -c "$COOKIES" -X POST "$WEB/api/v1/device-links/redeem" "${JSON[@]}" -d "{\"token\":\"${link#*#}\"}" | json 'd["csrf_token"]')
BROWSER=(-b "$COOKIES" -H "X-CSRF-Token: $csrf" -H "Origin: $WEB")

expect 200 "signed-in browser" "${BROWSER[@]}" "$WEB/api/v1/collections"
pass "browser signed in with a one-time link from 'server connect-link'"

# ══ 3. Collection + webhook receiver ═════════════════════════════════════════

step "3. Create a collection"

collection=$(curl -sf "${JSON[@]}" -X POST "$API/v1/collections" -d '{"name":"Instagram Avatar"}' | json 'd["id"]')
pass "collection $collection"

# The receiver answers 500 to the first delivery and 200 afterwards, so the
# retry path is tested too. It logs every request as one JSON line.

step "   Start a webhook receiver (fails the first delivery, then accepts)"

docker run -d --name "$RECEIVER" --network staqly-e2e_default python:3.13-alpine python -u -c '
import http.server, json

calls = 0

class Handler(http.server.BaseHTTPRequestHandler):
    def do_POST(self):
        global calls
        calls += 1
        body = self.rfile.read(int(self.headers["Content-Length"])).decode()
        print(json.dumps({"n": calls, "headers": dict(self.headers), "body": body}), flush=True)
        self.send_response(500 if calls == 1 else 200)
        self.end_headers()

    def log_message(self, *args):
        pass

server = http.server.HTTPServer(("0.0.0.0", 9000), Handler)
print("ready", flush=True)
server.serve_forever()
' >/dev/null

# Register the webhook only once the receiver listens: a refused first
# delivery would count as an extra attempt.
for _ in $(seq 60); do
  docker logs "$RECEIVER" 2>/dev/null | grep -q '^ready$' && break
  sleep 1
done
docker logs "$RECEIVER" 2>/dev/null | grep -q '^ready$' || fail "webhook receiver did not start"

webhook=$(curl -sf "${JSON[@]}" -X POST "$API/v1/webhooks" -d "{\"url\":\"http://$RECEIVER:9000/hook\",\"events\":[\"item.approved\"]}")
webhook_id=$(echo "$webhook" | json 'd["id"]')
webhook_secret=$(echo "$webhook" | json 'd["secret"]')

# ══ 4. Item via REST ═════════════════════════════════════════════════════════

step "4. Submit an item through the REST API"

item=$(curl -sf -X POST "$API/v1/collections/$collection/items" \
  -F "file=@integrations/examples/assets/avatar0.png" \
  -F 'item={"title":"E2E avatar","content":"caption","metadata":{"prompt":"e2e"},"source":"e2e"}' | json 'd["id"]')
media=$(curl -s "$API/v1/items/$item" | json 'd["media"]["id"]')
pass "item $item"

# ══ 5. Dashboard ═════════════════════════════════════════════════════════════

step "5. View the item in the dashboard"

expect 200 "item page" "$WEB/items/$item"
expect 200 "inbox page" "$WEB/inbox"

title=$(curl -s "${BROWSER[@]}" "$WEB/api/v1/items/$item" | json 'd["title"]')
[ "$title" = "E2E avatar" ] || fail "item via proxy: $title"

type=$(curl -s "${BROWSER[@]}" -o /dev/null -w '%{content_type}' "$WEB/api/v1/media/$media")
[ "$type" = image/png ] || fail "media preview via proxy: $type"

# A 12 MB "PNG" (valid header, zero padding) checks that the proxy streams
# large uploads instead of truncating them.
python3 -c 'import sys; sys.stdout.buffer.write(b"\x89PNG\r\n\x1a\n" + b"\0" * (12 << 20))' > "$TMP/big.png"
expect 201 "12 MB upload through the dashboard proxy" "${BROWSER[@]}" -X POST "$WEB/api/v1/media" -F "file=@$TMP/big.png"

pass "pages render; item, image and a 12 MB upload go through the dashboard proxy"

# ══ 6. Approve ═══════════════════════════════════════════════════════════════

step "6. Approve it (as the browser does: session + CSRF, via the proxy)"

approved=$(curl -s "${BROWSER[@]}" "${JSON[@]}" -X POST "$WEB/api/v1/items/$item/actions/approve" -d '{"feedback":"ship it"}' | json 'd["status"]')
[ "$approved" = approved ] || fail "approve: $approved"

expect 409 "double approval must conflict" -X POST "$API/v1/items/$item/actions/approve" "${JSON[@]}" -d '{}'
pass "approved; second decision conflicts"

# ══ 7. Webhook delivery ══════════════════════════════════════════════════════

step "7. Receive the webhook (with one retry)"

deliveries="$API/v1/webhook-deliveries?webhook_id=$webhook_id"
delivery=""

for _ in $(seq 30); do
  delivery=$(curl -s "$deliveries" | json 'd["data"][0]["status"] if d["data"] else ""')

  if [ "$delivery" = succeeded ]; then
    break
  fi

  sleep 1
done

[ "$delivery" = succeeded ] || fail "delivery status: $delivery"

attempts=$(curl -s "$deliveries" | json 'd["data"][0]["attempts"]')
[ "$attempts" = 2 ] || fail "expected 2 attempts, got $attempts"

# Verify the HMAC signature of the last request, exactly as a receiver should:
# sha256=hex(HMAC(secret, timestamp + "." + body)).
docker logs "$RECEIVER" | tail -1 | python3 -c '
import hashlib, hmac, json, sys

request = json.loads(sys.stdin.read())
headers = {k.lower(): v for k, v in request["headers"].items()}
signed = (headers["x-staqly-timestamp"] + "." + request["body"]).encode()
expected = "sha256=" + hmac.new(sys.argv[1].encode(), signed, hashlib.sha256).hexdigest()
assert hmac.compare_digest(expected, headers["x-staqly-signature"]), "bad signature"

event = json.loads(request["body"])
assert event["type"] == "item.approved", event
assert event["data"]["item"]["status"] == "approved", event
' "$webhook_secret" || fail "webhook payload/signature"

pass "delivered after retry, HMAC signature valid"

# ══ 8. History ═══════════════════════════════════════════════════════════════

step "8. Inspect history"

types=$(curl -s "$API/v1/items/$item/events" | json '",".join(e["type"] for e in d["data"])')
[ "$types" = "item.approved,item.created" ] || fail "history: $types"
pass "history: $types"

# ══ 9–10. MCP ════════════════════════════════════════════════════════════════

step "9. Connect through MCP (Streamable HTTP and STDIO, keyless)"

init=$(mcp "$MCP_INIT")
[ "$(echo "$init" | json 'd["result"]["serverInfo"]["name"]')" = staqly ] || fail "mcp initialize: $init"

tools=$(mcp '{"jsonrpc":"2.0","id":2,"method":"tools/list"}' | json 'len(d["result"]["tools"])')
[ "$tools" = 21 ] || fail "expected 21 tools, got $tools"
pass "HTTP: initialize ok, $tools tools"

step "10. Create more items through MCP"

created=$(mcp "$(mcp_tool 3 create_item '{"collection":"Instagram Avatar","title":"via MCP HTTP","content":"hello"}')")
[ "$(echo "$created" | json 'd["result"]["structuredContent"]["status"]')" = pending ] || fail "mcp http create: $created"

# STDIO: pipe three JSON-RPC messages into the mcp-stdio binary inside the
# container; `sleep` keeps stdin open until the answer arrives.
stdio=$(
  {
    printf '%s\n' "$MCP_INIT" \
      '{"jsonrpc":"2.0","method":"notifications/initialized"}' \
      "$(mcp_tool 2 create_item '{"collection":"Instagram Avatar","title":"via MCP stdio"}')"
    sleep 2
  } | docker exec -i "$API_CONTAINER" mcp-stdio
)
echo "$stdio" | grep -q '"id":2,"result"' || fail "mcp stdio create: $stdio"

# await_decision blocks until a human decides; approve from the side after 1 s.
gate=$(mcp "$(mcp_tool 5 create_item '{"collection":"Instagram Avatar","title":"gate me"}')" | json 'd["result"]["structuredContent"]["id"]')

(
  sleep 1
  curl -s -o /dev/null "${JSON[@]}" -X POST "$API/v1/items/$gate/actions/approve" -d '{}'
) &

wait_args="{\"item_id\":\"$gate\",\"timeout_seconds\":20}"
decision=$(mcp "$(mcp_tool 6 await_decision "$wait_args")" \
  | json 'str(d["result"]["structuredContent"]["decided"]) + "/" + d["result"]["structuredContent"]["item"]["status"]')
wait

[ "$decision" = "True/approved" ] || fail "await_decision: $decision"
pass "created via HTTP and STDIO; await_decision returned when a human approved"

# ── Search ───────────────────────────────────────────────────────────────────

step "    Full-text search"

found=$(curl -s "$API/v1/items?q=avat" | json 'd["data"][0]["title"]')
[ "$found" = "E2E avatar" ] || fail "prefix search: $found"
pass "search 'avat' finds 'E2E avatar'"

# ── Connect a device ─────────────────────────────────────────────────────────
# A phone on the LAN reaches the dashboard with a non-local Host. It is
# refused until it redeems a QR link, which works exactly once.

step "    Connect a device (phone on the LAN, through the dashboard proxy)"

PHONE=(-H "Host: 192.168.1.20:$WEB_PORT")
expect 403 "unconnected phone must be refused" "${PHONE[@]}" "$WEB/api/v1/collections"

token=$(curl -s "${BROWSER[@]}" "${JSON[@]}" -X POST "$WEB/api/v1/device-links" -d '{}' | json 'd["url"].split("#", 1)[1]')
phone_csrf=$(curl -s -c "$TMP/phone" "${PHONE[@]}" "${JSON[@]}" -X POST "$WEB/api/v1/device-links/redeem" -d "{\"token\":\"$token\"}" | json 'd["csrf_token"]')
PHONE_SESSION=(-b "$TMP/phone" "${PHONE[@]}" -H "X-CSRF-Token: $phone_csrf")

expect 200 "connected phone read" "${PHONE_SESSION[@]}" "$WEB/api/v1/collections"
expect 200 "connected phone action" "${PHONE_SESSION[@]}" "${JSON[@]}" -X POST "$WEB/api/v1/items/$item/actions/archive" -d '{}'
expect 410 "QR link must be single-use" "${PHONE[@]}" "${JSON[@]}" -X POST "$WEB/api/v1/device-links/redeem" -d "{\"token\":\"$token\"}"
pass "phone refused → QR link → session → reads and acts; link single-use"

# ══ 11. Persistence ══════════════════════════════════════════════════════════

step "11. Persistence across restart and container recreation"

before=$(curl -s "$API/v1/items" | json 'len(d["data"])')

docker compose restart api web >/dev/null 2>&1
wait_http "$API/health"

docker compose up -d $BUILD --force-recreate api web >/dev/null 2>&1
wait_http "$API/health"
wait_http "$WEB/"

after=$(curl -s "$API/v1/items" | json 'len(d["data"])')

if [ "$before" != "$after" ] || [ "$after" != 4 ]; then
  fail "items before=$before after=$after (expected 4)"
fi

expect 200 "media lost" "$API/v1/media/$media"
pass "$after items and media survive restart + recreation"

# ══ 12. Cloud mode ═══════════════════════════════════════════════════════════
# The same volume, served in cloud mode: every request needs a session or a
# scoped API key, and scopes apply to REST and MCP alike.

step "12. Serve the same data in cloud mode (keys required)"

STAQLY_MODE=cloud SETUP_TOKEN=e2e-token docker compose up -d --force-recreate api >/dev/null 2>&1
wait_http "$API/health"

expect 401 "cloud mode must require credentials" "$API/v1/collections"

# Recovery key (operator CLI) → browser session → scoped API key.
recovery=$(docker exec "$API_CONTAINER" server recovery-key)
csrf=$(curl -s -c "$COOKIES" "${JSON[@]}" -X POST "$API/v1/sessions/recover" -d "{\"recovery_key\":\"$recovery\"}" | json 'd["csrf_token"]')

expect 403 "CSRF must be enforced" -b "$COOKIES" "${JSON[@]}" -X POST "$API/v1/api-keys" -d '{"name":"x","scopes":["items:read"]}'

key=$(curl -s -b "$COOKIES" -H "X-CSRF-Token: $csrf" "${JSON[@]}" -X POST "$API/v1/api-keys" \
  -d '{"name":"agent","scopes":["collections:read","items:read","items:write"]}' | json 'd["key"]')
AGENT=(-H "Authorization: Bearer $key")

visible=$(curl -s "${AGENT[@]}" "$API/v1/items" | json 'len(d["data"])')
[ "$visible" = 4 ] || fail "data not visible with key ($visible items)"

# The key can create content but not review it, over REST or MCP.
expect 403 "creator key must not review" "${AGENT[@]}" "${JSON[@]}" -X POST "$API/v1/items/$item/actions/reject" -d '{}'

approve_args="{\"item_id\":\"$item\"}"
denied=$(mcp "$(mcp_tool 4 approve_item "$approve_args")" "${AGENT[@]}" | json 'd["result"].get("isError")')
[ "$denied" = True ] || fail "MCP scope not enforced"

expect 401 "keyless MCP in cloud mode" "${MCP[@]}" -X POST "$API/mcp" -d '{"jsonrpc":"2.0","id":5,"method":"tools/list"}'
pass "recovery key via CLI → session → scoped key; scopes enforced over REST and MCP"

# ══ 13. Signup and plan limits ═══════════════════════════════════════════════

step "13. Cloud signup: new isolated workspace on the Free plan, limits enforced"

expect 403 "signup without SETUP_TOKEN must be refused" -X POST "$API/v1/workspaces" "${JSON[@]}" -d '{"name":"Nope"}'

signup=$(curl -s "${JSON[@]}" -X POST "$API/v1/workspaces" -d '{"name":"Second","setup_token":"e2e-token"}')
[ "$(echo "$signup" | json 'd["workspace"]["plan"]')" = free ] || fail "signup: $signup"

workspace_id=$(echo "$signup" | json 'd["workspace"]["id"]')
OWNER=(-H "Authorization: Bearer $(echo "$signup" | json 'd["api_key"]["key"]')")

others=$(curl -s "${OWNER[@]}" "$API/v1/items" | json 'len(d["data"])')
[ "$others" = 0 ] || fail "new workspace sees other data ($others items)"

# Free plan: 100 items per month, the 101st gets 402 plan_limit_exceeded.
curl -sf "${OWNER[@]}" "${JSON[@]}" -X POST "$API/v1/collections" -d '{"name":"Q"}' >/dev/null

for i in $(seq 100); do
  curl -sf -o /dev/null "${OWNER[@]}" "${JSON[@]}" -X POST "$API/v1/collections/Q/items" -d "{\"title\":\"$i\"}" || fail "item $i"
done

over=$(curl -s "${OWNER[@]}" "${JSON[@]}" -X POST "$API/v1/collections/Q/items" -d '{"title":"101"}')
[ "$(echo "$over" | json 'd["error"]["code"]')" = plan_limit_exceeded ] || fail "limit: $over"

# The operator upgrades the workspace; the limit goes away.
docker exec "$API_CONTAINER" server set-plan "$workspace_id" indie >/dev/null
expect 201 "item after upgrade" "${OWNER[@]}" "${JSON[@]}" -X POST "$API/v1/collections/Q/items" -d '{"title":"101"}'
pass "Free plan stops at 100 items/month (402); operator upgrade to Indie lifts it"

printf '\nE2E passed.\n'
