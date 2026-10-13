# Troubleshooting

**`403 not_local`.** Local mode refused a credential-less request because its
peer address, `Host` or `Origin` is not local. Use `localhost`/`127.0.0.1`, a
compose service name, or add your host name to `LOCAL_ALLOWED_HOSTS`. A
browser page from another site is refused on purpose.

**`402 plan_limit_exceeded` (cloud).** The workspace hit its plan limit
(`error.details` says which). See *Plan & usage* in the dashboard; the operator
can upgrade with `make set-plan WS={WORKSPACE_ID} PLAN=indie`.

**`429 rate_limited` when creating a workspace (cloud).** At most 5 signups
per hour per IP. Behind a reverse proxy set `TRUST_PROXY=true`, otherwise all
users share the proxy's IP.

**Phone: "This device isn't connected".** Use *Connect a device* on your
computer and scan the QR (set `PUBLIC_WEB_URL=http://<LAN IP>:8080` in
`.env` so the QR points at an address the phone can reach). A browser on this
computer signs in with the link from `make up` / `make connect`. The QR link expires after 5 minutes and
works once.

**Dashboard shows the login page but I never set up (cloud mode).** Another setup already
happened on this volume (e.g. a previous run). Use the recovery key at /login,
or start fresh with `docker compose down -v` (deletes all data).

**Lost every session and the recovery key (cloud mode).** On the host run
`docker exec staqly-api server recovery-key` and log in at /login with the
printed key. This needs shell access to the server — there is no remote
backdoor.

**`403 csrf_failed` from scripts (cloud mode).** You are sending a session cookie. Scripts
should use `Authorization: Bearer {API_KEY}` (no CSRF needed).

**`403 forbidden: missing required scope …` (cloud mode).** The API key lacks a scope;
create a key with the right scopes under Integrations → API keys. The
setup key intentionally lacks `admin`.

**Webhook deliveries fail with `private destination … is not allowed` /
`loopback destination …`.** Adjust `WEBHOOK_ALLOW_PRIVATE` /
`WEBHOOK_ALLOW_LOOPBACK` in `.env`. Inside Docker, `localhost` is the API
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
STAQLY_API_KEY …`). Run `docker exec -i staqly-api mcp-stdio` manually —
errors go to stderr.

**Port already in use.** Set `API_PORT` / `WEB_PORT` in `.env`. If you change
`API_PORT`, also update `PUBLIC_API_URL` and your MCP client URLs.

**Upload rejected with 415.** Only common image/video/audio/PDF/text/CSV/JSON/
ZIP files are accepted; SVG and HTML are refused because they can carry script.
Raise the size limit with `MAX_UPLOAD_MB`.

**`database is locked`.** Shouldn't happen with the bundled configuration
(immediate transactions + 10 s busy timeout). Make sure only one `server`
process uses a data directory.

**Logs.** `docker compose logs -f api` — JSON lines; request paths are logged,
headers and query strings are not.
