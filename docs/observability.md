# Observability and hardening

The API ships request ids, health probes, Prometheus metrics, and rate limits. None of it needs extra services or
dependencies. Everything is configured with environment variables.

## Request ids

Every response carries `X-Request-Id`. If the caller sends its own
`X-Request-Id`, the API echoes it back. Otherwise it generates one. The same id
shows up as `request_id` in the access log, so you can
grep for it. Quote it when you report a problem.

## Logs

The server writes one JSON object per line to **stdout** (`log/slog`, no
OpenTelemetry dependency), ready for Cloudflare Logpush, Loki, Datadog or
`docker logs`. `mcp-stdio` logs text to **stderr** (stdout is its
protocol channel) at `warn` unless `LOG_LEVEL` / `LOG_FORMAT` say otherwise.

| Env | Default | Meaning |
|---|---|---|
| `LOG_LEVEL` | `info` | `debug`, `info`, `warn` or `error` (case-insensitive). Anything else refuses to start |
| `LOG_FORMAT` | `json` | `json` or `text` (for reading in a terminal) |

Never logged: query strings, request/response headers and bodies, API keys,
session tokens, decision-link tokens (the path shows `/v1/decide/{token}`),
channel secrets.

### Fields

Every line:

| Field | Example | Notes |
|---|---|---|
| `time`, `level`, `msg` | `INFO`, `http_request` | slog standard fields |
| `service` | `staqly-api` | (`staqly-mcp-stdio` for the stdio server) |
| `version` | `1.4.0` | build version (`-ldflags -X main.version`, Docker `ARG VERSION`; `dev` otherwise) |
| `mode` | `local` / `cloud` | |

Lines logged while handling a request add:

| Field | Notes |
|---|---|
| `request_id` | same as the `X-Request-Id` header and `request_id` in error bodies |
| `trace_id`, `span_id` | from a valid W3C `traceparent` request header (invalid ones are ignored); `trace_id` is also returned in error bodies |
| `cf_ray` | the `CF-Ray` header when it is a plausible ray id (letters, digits, `-`, ≤ 64 chars) |
| `method`, `path` | path only, never the query string |
| `workspace_id`, `principal` | after authentication; `principal` is `api_key:key_...`, `session`, `local` or `system:<id>` — never a secret |

The access line (`msg` = `http_request`, one per request) adds `status`,
`duration_ms`, `bytes` (response body size), `ip` (the client IP as used for
rate limiting; see `TRUST_PROXY`), `user_agent` (first 200 characters) and
`route` (the route pattern). Its level follows the status: `error` for 5xx,
`warn` for 429, `info` otherwise. `/health`, `/livez` and `/readyz` are not
logged at `info`.

An unexpected error (HTTP 500) also logs `msg` = `internal error` with `err`
plus the request fields above, so it can be found by `request_id` or
`trace_id`.

Background workers add `worker` (`webhooks`, `watchers`, `notifications`,
`telegram-poller`, `exports`, `retention`, `expiry`, `obs-heartbeat`) and,
where known, `workspace_id` plus the entity: `delivery_id`/`webhook_id`,
`watcher_id`, `channel_id`, `export_id`, `item_id`.

Example:

```json
{"time":"2026-10-13T12:00:00.123Z","level":"INFO","msg":"http_request","service":"staqly-api","version":"1.4.0","mode":"cloud","request_id":"api-1/abc-000042","trace_id":"4bf92f3577b34da6a3ce929d0e0e4736","span_id":"00f067aa0ba902b7","cf_ray":"8f2c1a2b3c4d5e6f-AMS","method":"POST","path":"/v1/collections/col_x/items","workspace_id":"ws_...","principal":"api_key:key_...","status":201,"duration_ms":12,"bytes":845,"ip":"203.0.113.9","user_agent":"n8n/1.80","route":"/v1/collections/{id}/items"}
```

### Cloudflare

Behind Cloudflare (proxy or Containers), keep `LOG_FORMAT=json` and ship
stdout: with Cloudflare Containers / Workers Logs the container's stdout is
collected as is; with Logpush, push the HTTP request logs and the container
logs to the same destination (R2, S3, Datadog, Loki...). Correlate an edge
request with the API line by `cf_ray` (Cloudflare adds the `CF-Ray` header to
every proxied request) and, if your Worker or client sends a W3C
`traceparent`, by `trace_id`. Set `TRUST_PROXY=true` so `ip` is the client
address (Cloudflare appends it to `X-Forwarded-For`) rather than the edge's.

```bash
# e.g. Loki / Grafana: everything about one failed request
{service="staqly-api"} | json | trace_id="4bf92f3577b34da6a3ce929d0e0e4736"
{service="staqly-api"} | json | level="ERROR" | workspace_id="ws_..."
```

## Health endpoints

| Endpoint | Use for | Checks |
|---|---|---|
| `GET /health` | Simple uptime checks (existing) | DB ping, version |
| `GET /livez` | Kubernetes/Docker liveness | Only that the process answers HTTP. Never touches the DB, so a slow disk does not cause restarts |
| `GET /readyz` | Readiness, load balancers | DB ping, every embedded migration applied, background workers alive |

`/readyz` returns `200 {"status":"ok","checks":{...}}` or `503
{"status":"unavailable","checks":{...}}`. Possible check values:

* `database`: `ok` or `unavailable`.
* `migrations`: `ok`, `unknown`, or `pending (N < M)`.
* `workers`: `ok`, `stale`, `stopped` or `not_started`. A heartbeat worker runs
  alongside the webhook and watcher workers and beats every 5 s. The check
  fails when the last beat is older than 30 s, or once the workers have shut
  down. `not_started` does not count as a failure: it covers processes that
  serve HTTP without background workers, such as tests.

Docker Compose healthcheck example:

```yaml
healthcheck:
  test: ["CMD", "wget", "-qO-", "http://127.0.0.1:3333/readyz"]
  interval: 15s
  timeout: 3s
  retries: 3
```

## Prometheus metrics

`GET /metrics` serves the Prometheus text exposition format (0.0.4).

**Access.** A request is allowed when either:

* it comes directly from this machine: a loopback TCP peer, a local `Host`
  name, and no `X-Forwarded-For`, `X-Forwarded-Host`, `Forwarded`,
  `X-Real-Ip` or `Origin` header. Requests that come through a reverse proxy,
  the dashboard proxy or a browser are never treated as local; or
* it sends `Authorization: Bearer <METRICS_TOKEN>`. The comparison is
  constant-time.

Every other request gets a plain `404`, so the endpoint does not show up in
scans. With Docker, Prometheus reaches the API over the bridge network, which
is not loopback, so set `METRICS_TOKEN`.

### Metric names

| Metric | Type | Labels | Meaning |
|---|---|---|---|
| `http_requests_total` | counter | `method`, `route`, `status` | Requests by chi route pattern (e.g. `/v1/items/{id}`), never the raw path. Unmatched paths use `unmatched` or the parent mount pattern (`/v1/*`) |
| `http_request_duration_seconds` | histogram | `method`, `route` | Latency. Buckets 5 ms to 10 s. Long-polls and MCP streams count their full duration |
| `staqly_rate_limited_total` | counter | `scope` (`ip`, `principal`) | Requests rejected with 429 |
| `staqly_webhook_deliveries` | gauge | `status` (`pending`, `succeeded`, `failed`) | Outbox rows, all workspaces |
| `staqly_webhook_outbox_oldest_pending_seconds` | gauge | none | Age of the oldest pending delivery (outbox lag); 0 when there is none |
| `staqly_webhook_outbox_due` | gauge | none | Pending deliveries whose next attempt is due now (worker backlog) |
| `staqly_watchers` | gauge | `status` (`active`, `degraded`, `paused`) | Watchers |
| `staqly_items` | gauge | `status` | Items by status. Statuses are user-defined, so only the 30 largest get their own label and the rest are summed into `other` |
| `staqly_metrics_db_up` | gauge | none | 1 if the DB gauges were collected on this scrape |
| `staqly_build_info` | gauge | `version`, `mode` | Always 1 |
| `process_start_time_seconds` | gauge | none | Process start time (unix seconds) |
| `go_info`, `go_goroutines`, `go_memstats_alloc_bytes`, `go_memstats_heap_inuse_bytes`, `go_memstats_sys_bytes`, `go_memstats_mallocs_total`, `go_gc_cycles_total`, `go_gc_pause_last_seconds` | various | none | Go runtime basics |

Labels never contain IDs, URLs, keys or workspace names. The DB gauges are
computed on each scrape from aggregate `COUNT(*)` queries, so a scrape
interval of 15–60 s is plenty.

### Scrape config

```yaml
scrape_configs:
  - job_name: staqly
    scrape_interval: 30s
    metrics_path: /metrics
    authorization:
      type: Bearer
      credentials_file: /etc/prometheus/staqly_metrics_token   # contains METRICS_TOKEN
    static_configs:
      - targets: ["api:3333"]
```

Without a token, run Prometheus or an agent (Grafana Alloy, the OTel collector,
`vmagent`) on the same host and scrape `127.0.0.1:3333`.

### Grafana tips

* Request rate by route: `sum by (route) (rate(http_requests_total[5m]))`
* Error ratio: `sum(rate(http_requests_total{status=~"5.."}[5m])) / sum(rate(http_requests_total[5m]))`
* p95 latency: `histogram_quantile(0.95, sum by (le, route) (rate(http_request_duration_seconds_bucket[5m])))`
* Webhook backlog: `staqly_webhook_deliveries{status="pending"}` and `staqly_webhook_outbox_oldest_pending_seconds`
* Abuse: `sum by (scope) (rate(staqly_rate_limited_total[5m]))`
* Restarts: `changes(process_start_time_seconds[1h])`

Suggested alerts:

* `staqly_webhook_outbox_oldest_pending_seconds > 900` for 10 minutes. Webhook
  retries back off up to `WEBHOOK_RETRY_MAX`, so set the threshold above that.
* `staqly_watchers{status="degraded"} > 0` for 1 hour.
* A 5xx ratio above 1% for 5 minutes.
* `up == 0`, or `/readyz` failing.

## Rate limits

The API uses in-memory token buckets in a single process. Memory is bounded:
at most 100k tracked keys per limiter. Buckets that have fully refilled are
evicted first, then the fullest / least recently used ones — never a reset of
the whole map, so spraying random keys can't clear an offender's bucket.

| Limiter | Applies to | Key | Default |
|---|---|---|---|
| Sensitive (`ip`) | `POST /v1/sessions/recover`, `POST /v1/device-links/redeem`, `POST /v1/workspaces`, `/v1/decide/*` | client IP + endpoint | 10/min, burst 10 |
| API (`principal`) | everything else under `/v1` and `/mcp`, **cloud mode only** | API key or session (hashed); client IP when there are no credentials | 600/min, burst 100 |
| Test sends | `POST /v1/webhooks/{id}/test`, `POST /v1/notification-channels/{id}/test` (both modes) | principal | 10/min, burst 5 |

Local mode has no per-principal limit. The sensitive endpoints are limited in
both modes. Probes and `/metrics` are never limited. Workspace signups keep
their existing 5/hour per IP cap in cloud mode as well.

When a request is over the limit, the response is `429` with a `Retry-After`
header (seconds) and this body:

```json
{"error":{"code":"rate_limited","message":"too many requests; retry after 6s"}}
```

| Env var | Default | Meaning |
|---|---|---|
| `RATE_LIMIT_DISABLED` | `false` | Turn off all WP-5 limits (the signup cap stays) |
| `RATE_LIMIT_PER_MINUTE` | `10` | Sensitive endpoints, per IP |
| `RATE_LIMIT_BURST` | = per-minute | Sensitive endpoints burst |
| `RATE_LIMIT_AUTH_PER_MINUTE` | `600` | Authenticated API, per principal |
| `RATE_LIMIT_AUTH_BURST` | `100` | Authenticated API burst |
| `TRUST_PROXY` | `false` | Take the client IP from the **right-most** `X-Forwarded-For` entry (the one your proxy appends; entries to its left are client-controlled). Turn this on only behind your own proxy. IPv6 clients are rate-limited per /64 |

Behind a proxy with `TRUST_PROXY=false`, all clients share the proxy's IP and
therefore one sensitive-endpoint bucket. The dashboard built into the same process (`staqly serve`, the
image) is not such a proxy: it passes each browser's address to the API with a per-process
secret, so browsers keep their own buckets without `TRUST_PROXY`.

