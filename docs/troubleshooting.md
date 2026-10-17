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

**`422 unknown event pattern ...` on a webhook or notification channel.**
`events` takes 1-50 patterns: `*`, a glob inside a known family such as
`item.*`, or a known event type. Fix the typo (e.g. `item.aproved`); custom
events such as `item.shipped` no longer exist (actions come from a fixed catalog).

**`422 ... items need media` when creating an image or video item.** Upload a
file (`media_id`) or pass a `url`; an update may not clear both.

**`402 plan_limit_exceeded` (cloud).** The workspace hit its plan limit
(`error.details` says which). See *Settings → Plan & usage* in the dashboard; the operator
can upgrade with `docker exec staqly server set-plan {WORKSPACE_ID} pro`.

**`429 rate_limited` when creating a workspace (cloud).** At most 5 signups
per hour per IP. Behind a reverse proxy set `TRUST_PROXY=true`, otherwise all
users share the proxy's IP.

**Phone: "This device isn't connected".** Use *Connect a device* on your
computer and scan the QR. With `staqly serve --expose` the QR uses this
computer's Wi-Fi address on its own; with Docker the dialog asks for that
address once; plain `staqly serve` keeps the dashboard on this computer, so
restart it with `--expose`. `PUBLIC_DASHBOARD_URL=http://<LAN IP>:8080` (`-e` on
`docker run`, or the environment of `staqly serve --expose`) fixes the address
for every link. A browser on this computer signs in
with the link from `docker exec staqly server connect-link` (or
`staqly serve connect-link`). The link expires after 5 minutes and works once.

**Dashboard shows the login page but I never set up (cloud mode).** Another setup already
happened on this volume (e.g. a previous run). Sign in with the admin API key at
/login (or "Lost your key?" with the recovery key), or start fresh by deleting the data volume (`docker volume rm`; deletes all data).

**Sign-in with an API key fails (cloud mode).** `403`: only keys with the
`admin` scope open the dashboard; use the admin key from signup or recovery,
not an agent key. `401`: the key is wrong, revoked or expired; use "Lost your
key?" with the recovery key. `409`: the server is in local mode; use a connect
link.

**`401 invalid recovery key` (cloud mode).** Each recovery key works once; a
recovery replaces it with the new one the dashboard showed. Use the newest one.

**Lost the admin key and the recovery key (cloud mode).** On the host run
`docker exec staqly server recovery-key` and use the printed key at /login →
"Lost your key?"; it shows a new admin key and recovery key once. This needs
shell access to the server — there is no remote backdoor. It signs out every
other browser of that workspace.

**`403 csrf_failed` from scripts (cloud mode).** You are sending a session cookie. Scripts
should use `Authorization: Bearer {API_KEY}` (no CSRF needed).

**`403 forbidden: missing required scope ...` (cloud mode).** The API key lacks a scope;
create a key with the right scopes under API keys in the sidebar. Agent keys
should lack `admin` (the signup key has it and signs in to the dashboard).

**Webhook deliveries fail with `private destination ... is not allowed` /
`loopback destination ...`.** Adjust `WEBHOOK_ALLOW_PRIVATE` /
`WEBHOOK_ALLOW_LOOPBACK` (`-e` on `docker run`, the environment of
`staqly serve`, or `.env` for Compose). `WEBHOOK_ALLOW_PRIVATE` defaults to `true`
in local mode and `false` in cloud mode. In cloud mode the delivery log keeps
only the status code and a short reason, never the response body. Inside Docker, `localhost` is the API
container itself — use `host.docker.internal` or a compose service name (see
[n8n.md](n8n.md)).

**Webhook deliveries `connection refused` from n8n on the host (Linux).** Make
n8n listen on all interfaces (or the docker bridge), and use
`http://host.docker.internal:5678/...`; the compose file maps it to the host
gateway (with `docker run`, add `--add-host host.docker.internal:host-gateway`).

**n8n Code node: `Cannot find module 'crypto'`.** Set
`NODE_FUNCTION_ALLOW_BUILTIN=crypto` on n8n, or leave the demo's
`webhookSecret` empty (no verification).

**MCP STDIO prints nothing / exits.** Do not pass `-t` to `docker exec`. In
cloud mode `STAQLY_API_KEY` must be forwarded (`docker exec -i -e
STAQLY_API_KEY ...`). Run `docker exec -i staqly mcp-stdio` manually —
errors go to stderr.

**Port already in use.** `staqly serve --port 4333 --dashboard-port 9090`;
with `docker run`, change the host side of `-p` (e.g. `-p 127.0.0.1:4333:3333`)
and set `-e PUBLIC_API_URL=http://localhost:4333`; with Compose, `API_PORT` /
`DASHBOARD_PORT` in `.env`. After moving the API, also update your MCP client
URLs.

**Items get no summary or tags.** `staqly serve` uses `claude` (else `codex`)
from its `PATH`; install one, or set `TAGGER_COMMAND`. The Docker image has no
AI CLI: set `TAGGER_URL` (OpenAI-compatible, e.g.
`https://api.openai.com/v1`), `TAGGER_API_KEY` and optionally `TAGGER_MODEL`
(default `gpt-4o-mini`). Failures are logged as `summary failed`; each item is tried up to 5
times. `TAGGER_COMMAND=off` turns summaries off.

**Upload rejected with 415.** Only common image/video/audio/PDF/text/CSV/JSON/
ZIP files are accepted; SVG and HTML are refused because they can carry script.
Raise the size limit with `MAX_UPLOAD_MB`.

**`database is locked`.** Shouldn't happen with the bundled configuration
(immediate transactions + 10 s busy timeout). Make sure only one `server`
process uses a data directory.

**`429` from a webhook/channel "test" button.** Test sends are limited per
API key or session (burst 5, then 10 per minute).

**Logs.** `docker logs -f staqly` — JSON lines on stdout (`LOG_FORMAT=text`
for a terminal); `staqly serve` prints text to the terminal, and
`staqly service logs -f` shows the background service's. `LOG_LEVEL=debug`
gives more; request paths are logged,
headers and query strings are not. Find everything about one failed request by
the `request_id` (or `trace_id`) from the error body — see
[observability.md](observability.md#logs).
