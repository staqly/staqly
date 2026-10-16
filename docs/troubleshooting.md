# Troubleshooting

**`403 not_local`.** Local mode refused a credential-less request because its
peer address, `Host` or `Origin` is not local.

* *Peer:* only loopback and the networks in `LOCAL_TRUSTED_PEERS` get keyless
  access. The documented `docker run` command and `compose.yml` set the
  Docker/private ranges; `staqly serve` (or `go run ./cmd/server`) listens on `127.0.0.1:3333` and
  trusts loopback only. Another machine on your LAN should use a QR session
  (or cloud mode with API keys) rather than widening `LOCAL_TRUSTED_PEERS`.
* *Host:* use `localhost`/`127.0.0.1`, `api`, `host.docker.internal`, or add your
  host name to `LOCAL_ALLOWED_HOSTS`.
* *Origin:* browsers are accepted only from the dashboard
  (`http(s)://localhost|127.0.0.1|[::1]:DASHBOARD_PORT`, the origin of
  `PUBLIC_DASHBOARD_URL`). A browser tool on another port (e.g. the MCP Inspector)
  needs its origin in `LOCAL_ALLOWED_ORIGINS`. Pages from other sites are
  refused on purpose.

**Server refuses to start: `config: ...`.** `STAQLY_MODE` must be `local` or
`cloud`; `LOCAL_TRUSTED_PEERS` entries must be CIDRs or IP addresses;
`NOTIFY_SECRET_KEY` must be at least 32 bytes.

**`422 search_too_broad` / `503 timeout` on item lists.** A listing or search
ran longer than 5 seconds. Add more specific words or filters (collection,
status, dates). Searches use at most 16 distinct words.

**`403 ... requires items:review` when updating an item.** With `items:write`
alone an approved/rejected item's content is frozen (`metadata_patch` still
works) and only the agent lifecycle status changes are allowed (approved →
processing → completed/failed, retries, resubmitting, archiving).

**"Item changed since this message".** The decision link or chat button was
for an older version of the item; open it in the dashboard to review the
current version.

**`422 ... must be re-entered when ... changes` on a notification channel.**
Changing a channel's destination requires re-entering its secret in the same
request (see [notifications.md](notifications.md)).

**`402 plan_limit_exceeded` (cloud).** The workspace hit its plan limit
(`error.details` says which). See *Plan & usage* in the dashboard; the operator
can upgrade with `docker exec staqly server set-plan {WORKSPACE_ID} pro`.

**`429 rate_limited` when creating a workspace (cloud).** At most 5 signups
per hour per IP. Behind a reverse proxy set `TRUST_PROXY=true`, otherwise all
users share the proxy's IP.

**Phone: "This device isn't connected".** Use *Connect a device* on your
computer and scan the QR (set `PUBLIC_DASHBOARD_URL=http://<LAN IP>:8080`, with
`-e` on `docker run` or in the environment of `staqly serve --expose`, so the QR
points at an address the phone can reach). A browser on this computer signs in
with the link from `docker exec staqly server connect-link` (or
`staqly serve connect-link`). The link expires after 5 minutes and works once.

**Dashboard shows the login page but I never set up (cloud mode).** Another setup already
happened on this volume (e.g. a previous run). Use the recovery key at /login,
or start fresh by deleting the data volume (`docker volume rm`; deletes all data).

**Lost every session and the recovery key (cloud mode).** On the host run
`docker exec staqly server recovery-key` and log in at /login with the
printed key. This needs shell access to the server — there is no remote
backdoor. It signs out every other browser of that workspace.

**`403 csrf_failed` from scripts (cloud mode).** You are sending a session cookie. Scripts
should use `Authorization: Bearer {API_KEY}` (no CSRF needed).

**`403 forbidden: missing required scope ...` (cloud mode).** The API key lacks a scope;
create a key with the right scopes under API keys in the sidebar. The
setup key intentionally lacks `admin`.

**Webhook deliveries fail with `private destination ... is not allowed` /
`loopback destination ...`.** Adjust `WEBHOOK_ALLOW_PRIVATE` /
`WEBHOOK_ALLOW_LOOPBACK` in `.env`. `WEBHOOK_ALLOW_PRIVATE` defaults to `true`
in local mode and `false` in cloud mode. In cloud mode the delivery log keeps
only the status code and a short reason, never the response body. Inside Docker, `localhost` is the API
container itself — use `host.docker.internal` or a compose service name (see
[n8n.md](n8n.md)).

**Webhook deliveries `connection refused` from n8n on the host (Linux).** Make
n8n listen on all interfaces (or the docker bridge), and use
`http://host.docker.internal:5678/...`; the compose file maps it to the host
gateway.

**n8n Code node: `Cannot find module 'crypto'`.** Set
`NODE_FUNCTION_ALLOW_BUILTIN=crypto` on n8n, or leave the demo's
`webhookSecret` empty (no verification).

**MCP STDIO prints nothing / exits.** Do not pass `-t` to `docker exec`. In
cloud mode `STAQLY_API_KEY` must be forwarded (`docker exec -i -e
STAQLY_API_KEY ...`). Run `docker exec -i staqly mcp-stdio` manually —
errors go to stderr.

**Port already in use.** Set `API_PORT` / `DASHBOARD_PORT` in `.env`. If you change
`API_PORT`, also update `PUBLIC_API_URL` and your MCP client URLs.

**Upload rejected with 415.** Only common image/video/audio/PDF/text/CSV/JSON/
ZIP files are accepted; SVG and HTML are refused because they can carry script.
Raise the size limit with `MAX_UPLOAD_MB`.

**`database is locked`.** Shouldn't happen with the bundled configuration
(immediate transactions + 10 s busy timeout). Make sure only one `server`
process uses a data directory.

**`429` from a webhook/channel "test" button.** Test sends are limited per
API key or session (burst 5, then 10 per minute).

**Logs.** `docker logs -f staqly` (`staqly service logs -f` for the binary) — JSON lines on stdout (`LOG_FORMAT=text`
for a terminal, `LOG_LEVEL=debug` for more); request paths are logged,
headers and query strings are not. Find everything about one failed request by
the `request_id` (or `trace_id`) from the error body — see
[observability.md](observability.md#logs).
