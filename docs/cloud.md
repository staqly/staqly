# Local vs cloud

| | Local mode (default) | Cloud mode |
|---|---|---|
| `STAQLY_MODE` | `local` | `cloud` |
| Workspaces | one, created automatically | many — anyone can create one ("no card, no email"), or only `SETUP_TOKEN` holders |
| Plan | **Unlocked** (no limits) | **Free** by default; Indie / Team set by the operator |
| Agents / n8n / scripts | no credentials (this machine only) | API key `stk_…` — the key decides which workspace you work in |
| Dashboard | opens directly on this computer | session cookie + CSRF; recovery key for lost sessions |
| Phones / tablets | **Connect a device** QR (needs LAN access, see below) | **Connect a device** QR |
| MCP | `http://localhost:3333/mcp` or `docker exec -i staqly-api mcp-stdio` | `https://<host>/mcp` + `Authorization: Bearer {API_KEY}` |

### Defaults that depend on the mode

| Env | Local default | Cloud default | Notes |
|---|---|---|---|
| `ADDR` | `127.0.0.1:3333` | `:3333` | The Docker image sets `ADDR=:3333`; compose publishes it on `127.0.0.1` |
| `COOKIE_SECURE` | `false` | `true` | Set `false` only for plain-HTTP testing |
| `WEBHOOK_ALLOW_PRIVATE` | `true` | `false` | Tenants must not reach the operator's network |
| `LOCAL_TRUSTED_PEERS` | empty (loopback only) | — | CIDRs whose peers get keyless access in local mode; compose sets the Docker/private ranges |
| `LOCAL_ALLOWED_ORIGINS` | empty | — | Extra browser origins accepted in local mode besides the dashboard |

`STAQLY_MODE` is trimmed and case-insensitive; any value other than `local`
or `cloud` refuses to start. `NOTIFY_ALLOW_PRIVATE` and `WATCHER_ALLOW_PRIVATE`
default to `false` in both modes. Local mode warns at startup when it listens
beyond loopback without `LOCAL_TRUSTED_PEERS`.

## Plans (cloud)

| | Free | Indie — $9/mo | Team — $29/mo |
|---|---|---|---|
| Items created / month | 100 | 2,000 | unlimited |
| Webhooks | 3 | 20 | unlimited |
| Active API keys | 3 | 20 | unlimited |
| Storage | 1 GB | 20 GB | 200 GB |
| Max file size | 25 MB | 100 MB | 500 MB (capped by `MAX_UPLOAD_MB`) |

Signed webhooks, MCP, full history and backups are on every plan.

* Limits apply **per workspace** — every API key of a workspace shares its
  plan. "Items / month" is metered when an item is created (calendar month,
  UTC); deleting items does not refund quota.
* A request over a limit fails with **HTTP 402**:
  ```json
  {"error":{"code":"plan_limit_exceeded","message":"The Free plan allows 100 items per month; …",
            "details":{"limit":"items_per_month","max":100,"used":100,"plan":"free"}}}
  ```
* `GET /v1/plans` lists the plans; `GET /v1/workspace/usage` shows the current
  plan and usage (the dashboard's *Plan & usage* page).
* **Payments are not integrated yet.** The operator changes plans on the
  server:
  ```bash
  make workspaces                          # id, plan, created, name
  make set-plan WS={WORKSPACE_ID} PLAN=indie         # free | indie | team
  ```
  The plan table lives in `apps/api/internal/app/plans.go` (single source of
  truth for limits and the pricing shown in the dashboard).
* Local installs are always unlocked; usage is still shown for information.

## Deploying a cloud instance

```bash
cat > .env <<'ENV'
STAQLY_MODE=cloud
PUBLIC_API_URL=https://staqly.example.com
PUBLIC_WEB_URL=https://staqly.example.com
TRUST_PROXY=true            # behind your reverse proxy
# SETUP_TOKEN=…             # set to make signup private
ENV
docker compose up -d --build
```

Put a TLS reverse proxy in front: route the domain to `127.0.0.1:8080`
(dashboard; it proxies `/api/*` to the API) and, if agents should use the API
directly, `/v1`, `/mcp`, `/openapi.yaml` to `127.0.0.1:3333`. Keep published
ports on `127.0.0.1`.

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

* The token is in the URL fragment (`/connect#…`), so it never reaches server
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
computer" from "someone on the Wi-Fi". `make up` opens a signed-in browser for
you (one-time link, valid 5 minutes); `make connect` prints another link;
phones scan the QR from *Connect a device*. For the QR to work on a phone, set
the address it can reach:

```bash
echo "PUBLIC_WEB_URL=http://192.168.1.23:8080" >> .env   # your computer's LAN IP
docker compose up -d
```

Sessions last 30 days and can be revoked under *Settings → Devices*. The API
stays on `127.0.0.1`, so agents, MCP and scripts on this computer keep working
without keys.

## Moving local data to the cloud

1. Locally: `make backup`.
2. On the server, start the stack in cloud mode, then
   `make restore BACKUP=backups/<ts>`.
3. Get a recovery key for the restored workspace:
   `make workspaces` then `make recovery-key WS={WORKSPACE_ID}`, and sign in at `/login`.

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
  than the dashboard's own (`localhost`/`127.0.0.1`/`[::1]` on `WEB_PORT`,
  `PUBLIC_WEB_URL`) or one in `LOCAL_ALLOWED_ORIGINS` — blocking websites,
  including pages on other localhost ports — and, when they come through the
  dashboard proxy, a local `X-Forwarded-Host` — so other devices on the LAN
  need a QR-issued session.
* Every local process (agents included) has full access. For per-agent limits
  run in cloud mode on localhost.
