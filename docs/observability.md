# Observability and hardening

The API ships request ids, health probes, Prometheus metrics, rate limits and
optional Sentry error reporting. None of it needs extra services or
dependencies. Everything is configured with environment variables.

## Request ids

Every response carries `X-Request-Id`. If the caller sends its own
`X-Request-Id`, the API echoes it back. Otherwise it generates one. The same id
shows up as `request_id` in the access log and in Sentry events, so you can
grep for it. Quote it when you report a problem.

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
at most 100k tracked keys per limiter, and buckets that have fully refilled
are evicted first.

| Limiter | Applies to | Key | Default |
|---|---|---|---|
| Sensitive (`ip`) | `POST /v1/sessions/recover`, `POST /v1/device-links/redeem`, `POST /v1/workspaces`, `/v1/decide/*` | client IP + endpoint | 10/min, burst 10 |
| API (`principal`) | everything else under `/v1` and `/mcp`, **cloud mode only** | API key or session (hashed); client IP when there are no credentials | 600/min, burst 100 |

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
| `TRUST_PROXY` | `false` | Take the client IP from the first `X-Forwarded-For` entry. Turn this on only behind your own proxy that overwrites the header. Otherwise every client can pick its own IP |

Behind a proxy with `TRUST_PROXY=false`, all clients share the proxy's IP and
therefore one sensitive-endpoint bucket.

## Sentry (optional)

Set `SENTRY_DSN` to report panics and 5xx responses. The client is small and
hand-rolled, and talks to the Sentry envelope API. It has no SDK dependency.

| Env var | Default | Meaning |
|---|---|---|
| `SENTRY_DSN` | unset (disabled) | `https://<public_key>@<host>/<project_id>` |
| `SENTRY_ENVIRONMENT` | the mode (`local`/`cloud`) | Sentry environment |

The release is set to the server version.

* **Panics** are sent as exceptions with a stack trace. The response is still
  a `500` from the recoverer.
* **5xx responses** are sent as messages such as `HTTP 500 GET /v1/items/{id}`.
  Probe endpoints are not reported, because they report outages themselves.
* **Privacy.** An event carries only these tags: `method`, `route` (the
  pattern), `status` and `request_id`. It never includes headers, query
  strings, bodies, IP addresses or user data. Look up the full error in the
  server log by `request_id`.
* **Load.** Events go into a 64-slot queue and are sent from one goroutine
  with a 5 s timeout. When the queue is full, new events are dropped, so an
  error storm cannot slow down the API.

An invalid DSN logs `sentry disabled` at startup without echoing the DSN.
