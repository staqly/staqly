#!/usr/bin/env bash
# Prepares the dev stack's listmonk (compose.channels.yml) for a staqly channel and prints how to connect it: an
# API user `staqly` (made again on every run: listmonk shows a token only when the user is created) and SMTP to
# mailpit. Dev only: the admin password is the compose file's.
set -euo pipefail

BASE="http://localhost:${LISTMONK_PORT:-9010}"
ADMIN_USER="admin"
ADMIN_PASSWORD=staqly-dev
jar=$(mktemp)
trap 'rm -f "$jar"' EXIT

for _ in $(seq 1 60); do
  curl -fsS -o /dev/null "$BASE/health" && break
  sleep 2
done
curl -fsS -o /dev/null "$BASE/health" || { echo "listmonk does not answer on $BASE (make channels starts it)" >&2; exit 1; }

# A browser session for the admin: listmonk 4+ takes API calls from API users only.
nonce=$(curl -fsS -c "$jar" "$BASE/admin/login" | sed -n 's/.*name="nonce" value="\([^"]*\)".*/\1/p' | head -1)
curl -fsS -o /dev/null -c "$jar" -b "$jar" -X POST --data-urlencode "username=$ADMIN_USER" \
  --data-urlencode "password=$ADMIN_PASSWORD" --data-urlencode "nonce=$nonce" --data-urlencode "next=/admin" "$BASE/admin/login"
api() { curl -fsS -b "$jar" -H 'Content-Type: application/json' "$@"; }

# SMTP: every campaign lands in mailpit.
api "$BASE/api/settings" | python3 -c '
import json, sys
s = json.load(sys.stdin)["data"]
# The first server, pointed at mailpit; its other fields (and any listmonk adds) stay as they are.
smtp = s["smtp"][0]
smtp.update({"enabled": True, "host": "mailpit", "port": 1025, "auth_protocol": "none", "username": "",
  "tls_type": "none", "tls_skip_verify": True})
smtp["msg_retry_delay"] = smtp.get("msg_retry_delay") or "10s"
s["smtp"] = [smtp] + s["smtp"][1:]
print(json.dumps(s))' | api -X PUT "$BASE/api/settings" -d @- >/dev/null

# The API user, fresh: an existing one is removed first.
id=$(api "$BASE/api/users" | python3 -c '
import json, sys
print(next((str(u["id"]) for u in json.load(sys.stdin)["data"] if u["username"] == "staqly"), ""))')
if [ -n "$id" ]; then
  api -X DELETE "$BASE/api/users/$id" >/dev/null
fi
token=$(api -X POST "$BASE/api/users" -d '{"username":"staqly","name":"staqly","type":"api","status":"enabled","user_role_id":1}' |
  python3 -c 'import json, sys; print(json.load(sys.stdin)["data"]["password"])')

cat <<OUT
listmonk is ready: $BASE (admin / $ADMIN_PASSWORD), email in mailpit (http://localhost:8025).
In staqly, Add channel -> ListMonk:
  listmonk address  http://listmonk:9000
  API user          staqly
  API token         $token
Lists to send to: Default list, Opt-in list.
OUT
