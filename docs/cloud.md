# Local vs cloud

| Setting | Local mode (default) | Cloud mode |
|---|---|---|
| `STAQLY_MODE` | `local` | `cloud` |
| Workspaces | one, created automatically | many — anyone can create one ("no card, no email"), or only `SETUP_TOKEN` holders |
| Plan | **Unlocked** (no limits) | 7-day **Pro** trial, then Pro (paid) or **Free** |
| Agents / n8n / scripts | no credentials (this machine only) | API key `stk_...` — the key decides which workspace you work in |
| Dashboard | opens directly on this computer | session cookie + CSRF; recovery key for lost sessions |
| Phones / tablets | **Connect a device** QR (needs LAN access, see below) | **Connect a device** QR |
| MCP | `http://localhost:3333/mcp` or `docker exec -i staqly mcp-stdio` | `https://<host>/mcp` + `Authorization: Bearer {API_KEY}` |

### Defaults that depend on the mode

| Env | Local default | Cloud default | Notes |
|---|---|---|---|
| `ADDR` | `127.0.0.1:3333` | `:3333` | The Docker image sets `ADDR=:3333`; compose publishes it on `127.0.0.1` |
| `COOKIE_SECURE` | `false` | `true` | Set `false` only for plain-HTTP testing |
| `WEBHOOK_ALLOW_PRIVATE` | `true` | `false` | Tenants must not reach the operator's network |
| `LOCAL_TRUSTED_PEERS` | empty (loopback only) | — | CIDRs whose peers get keyless access in local mode; compose sets the Docker/private ranges |
| `LOCAL_ALLOWED_ORIGINS` | empty | — | Extra browser origins accepted in local mode besides the dashboard |
| `CORS_ALLOWED_ORIGINS` | ignored | `https://docs.staqly.ai` | Browser origins that may call the API with an API key (the docs' API playground); `none` turns it off. Never with cookies |

`STAQLY_MODE` is trimmed and case-insensitive; any value other than `local`
or `cloud` refuses to start. `NOTIFY_ALLOW_PRIVATE` and `WATCHER_ALLOW_PRIVATE`
default to `false` in both modes. Local mode warns at startup when it listens
beyond loopback without `LOCAL_TRUSTED_PEERS`.

## Plans (cloud)

| Limit | Free | Pro — $30/mo |
|---|---|---|
| Approvals / month (approve + reject) | 50 | unlimited |
| Items and collections | unlimited | unlimited |
| Watchers (active) | 3 | unlimited |
| Fastest watcher interval | every 15 min | every minute |
| Notification channels | 2 | unlimited |
| Webhooks | 3 | unlimited |
| Active API keys | 1 | unlimited |
| Storage | 1 GB | 100 GB |
| Max file size | 5 MB | 100 MB |

New workspaces start on a **7-day Pro trial** (`plan_expires_at`). When a Pro trial or paid
period ends unpaid, the workspace is **suspended**: reads and exports work, every other change
answers `402 plan_expired` (REST, MCP write tools, decision links), watchers pause. Switching to
Free (`POST /v1/workspace/plan`, admin) or `set-plan pro` (operator, no end date) lifts it.

File sizes are also capped by the operator's `MAX_UPLOAD_MB` (default 100).
The limits live in `apps/api/internal/app/plans.go` and are enforced by the API.

Signed webhooks, MCP, full history and backups are on every plan.

* Limits apply **per workspace**: every API key of a workspace shares its
  plan. "Approvals / month" counts approvals and rejections (by people, chat
  buttons or bulk actions) per calendar month (UTC); deleting items
  does not give quota back.
* A request over a limit fails with **HTTP 402**:
  ```json
  {"error":{"code":"plan_limit_exceeded","message":"The Free plan allows 50 approvals per month; ...",
            "details":{"limit":"decisions_per_month","max":50,"used":50,"plan":"free"}}}
  ```
* `GET /v1/plans` lists the plans; `GET /v1/workspace/usage` shows the current
  plan and usage (the dashboard's *Plan & usage* page).
* **Payments:** with `STRIPE_SECRET_KEY`, `STRIPE_WEBHOOK_SECRET` and
  `STRIPE_PRICE_PRO` set, workspace admins pay for Pro with Stripe Checkout
  (*Settings → Plan & usage → Upgrade to Pro*) and manage it in the Stripe
  customer portal; the plan follows the subscription (paid period + 3 days of
  grace). See the docs site, *Self-hosting → Billing with Stripe*. Without
  them, or to comp a workspace, the operator changes plans on the server:
  ```bash
  docker exec staqly server workspaces                     # id, plan, plan end, created, name
  docker exec staqly server set-plan {WORKSPACE_ID} pro      # free | pro
  docker exec staqly server set-plan {WORKSPACE_ID} pro --until 2026-12-31   # comped until then
  ```
  The plan table lives in `apps/api/internal/app/plans.go` (single source of
  truth for limits and the pricing shown in the dashboard).
* Local installs are always unlocked; usage is still shown for information.

## Deploying a cloud instance

```bash
cat > .env <<'ENV'
STAQLY_DOMAIN=staqly.example.com   # DNS points at this server, ports 80 and 443 open
# SETUP_TOKEN=...                  # set to make signup private
ENV
make cloud     # compose.yml + compose.cloud.yml: the pre-built image behind Caddy
```

`compose.cloud.yml` sets `STAQLY_MODE=cloud`, `PUBLIC_API_URL` and
`PUBLIC_DASHBOARD_URL` (`https://` + the domain), `TRUST_PROXY=true` and
`COOKIE_SECURE=true`. Caddy (`deploy/Caddyfile`) gets the certificate, routes
`/v1`, `/mcp`, `/openapi.yaml`, `/health` and `/readyz` to the API and
everything else to the dashboard; only Caddy publishes ports.

With your own reverse proxy instead, run `compose.yml` alone with those
variables in `.env`: route the domain to `:8080` (dashboard; it proxies
`/api/*` to the API; firewall it so only the proxy reaches it) and `/v1`,
`/mcp`, `/openapi.yaml`, `/health` to `127.0.0.1:3333`.

Signup is rate-limited to 5 workspaces per hour per client IP (`TRUST_PROXY`
makes it use the right-most `X-Forwarded-For` entry, the one your proxy
appends; IPv6 clients are counted per /64).

Cloud-mode hardening worth knowing: secret-bearing responses (new API keys,
recovery keys, device links, webhook secrets) are never cached for
`Idempotency-Key` replay; credential audit events (`api_key.*`, `session.*`,
`recovery_key.*`, `device*.*`) are listed only for admin principals; the
webhook signing secret is shown only on create/rotate; using or rotating the
recovery key signs out every other session; `mcp-stdio` re-checks
`STAQLY_API_KEY` at least once a minute, so revoking it takes effect.

## Using a cloud workspace from your tools

```bash
export STAQLY_API_KEY={API_KEY}
claude mcp add --transport http staqly https://staqly.example.com/mcp \
  --header "Authorization: Bearer $STAQLY_API_KEY"
codex mcp add staqly --url https://staqly.example.com/mcp --bearer-token-env-var STAQLY_API_KEY
curl https://staqly.example.com/v1/items?status=pending -H "Authorization: Bearer $STAQLY_API_KEY"
```

```ts
import { Staqly } from "@staqly/sdk";
const local = new Staqly();                                              // local mode
const cloud = new Staqly({ baseUrl: "https://staqly.example.com", apiKey: process.env.STAQLY_API_KEY });
```

n8n: set *Authentication → Generic → Header Auth* (`Authorization: Bearer {API_KEY}`)
on the HTTP Request nodes and point `apiBaseUrl` at the cloud URL.

Several workspaces = several keys: each key only sees its own workspace.

## Connect a device (QR)

In the dashboard choose **Connect a device**. It shows a QR code with a
one-time link: scanning it on a phone opens staqly already signed in — no key
to type.

* The token is in the URL fragment (`/connect#...`), so it never reaches server
  logs, proxies or `Referer`; it is stored hashed, expires after **5 minutes**
  and works **once**.
* The phone gets its own browser session; it appears under *Settings →
  Devices* and can be revoked there.
* Only the dashboard can create links — API keys cannot mint browser sessions.
* The QR grants full access to the workspace: scan it only with your own
  device and don't share screenshots.

**Signing in (local mode):** the dashboard is published on your network so
phones and tablets can use it, which means **every browser signs in once**,
including the one on this computer: Docker Desktop hides the real client
address and a `Host` header can be forged, so the dashboard cannot tell "this
computer" from "someone on the Wi-Fi". `docker exec staqly server connect-link`
(or `staqly serve connect-link`) prints a one-time link (valid 5 minutes);
phones scan the QR from *Connect a device*. For the QR to work on a phone, set
the address it can reach: recreate the container with
`-e PUBLIC_DASHBOARD_URL=http://192.168.1.23:8080` (your computer's LAN IP), or
start `PUBLIC_DASHBOARD_URL=http://192.168.1.23:8080 staqly serve --expose`.

Sessions last 30 days and can be revoked under *Settings → Devices*. The API
stays on `127.0.0.1`, so agents, MCP and scripts on this computer keep working
without keys.

## Moving local data to the cloud

1. Locally: back up the database and uploads ([backup.md](backup.md)).
2. On the server (a checkout running `make cloud`), put them in
   `backups/<ts>/workspace.db` and `backups/<ts>/uploads.tar.gz`, start the
   stack in cloud mode, then `make restore BACKUP=backups/<ts>`.
3. Get a recovery key for the restored workspace:
   `docker exec staqly server workspaces`, then
   `docker exec staqly server recovery-key {WORKSPACE_ID}`, and sign in at `/login`.

`server recovery-key` is also the operator's escape hatch when a user loses all
sessions and the recovery key — it needs shell access to the server. Like a
rotation from the dashboard, it signs out every browser session of the
workspace (the count is printed on stderr; stdout is only the key).

## Why local mode is safe without keys

* Published ports bind to `127.0.0.1`.
* Run outside Docker, the server listens on `127.0.0.1:3333` by default.
* Credential-less requests must come from a loopback peer or one inside
  `LOCAL_TRUSTED_PEERS` (compose sets
  `10.0.0.0/8,172.16.0.0/12,192.168.0.0/16,fc00::/7` because Docker forwards
  published ports from its bridge), name a local `Host` (`localhost`,
  `127.0.0.1`, `api`, `host.docker.internal`, the container name,
  `LOCAL_ALLOWED_HOSTS`) — defeating DNS rebinding — carry no `Origin` other
  than the dashboard's own (`localhost`/`127.0.0.1`/`[::1]` on `DASHBOARD_PORT`,
  `PUBLIC_DASHBOARD_URL`) or one in `LOCAL_ALLOWED_ORIGINS` — blocking websites,
  including pages on other localhost ports — and, when they come through the
  dashboard proxy, a local `X-Forwarded-Host` — so other devices on the LAN
  need a QR-issued session.
* Every local process (agents included) has full access. For per-agent limits
  run in cloud mode on localhost.
