# @staqly/e2e

End-to-end tests against the real Docker stack: the staqly image (API, MCP and
dashboard in one container) built from this checkout, started with `compose.yml` plus `compose.e2e.yml`, driven
over HTTP, MCP and the operator CLI. Tests are written with Vitest over raw
HTTP (`lib/clients.ts`, a thin typed wrapper), so they test the server's
contract itself: status codes, credentials (local mode's access token, API keys), cookies and CSRF,
the dashboard proxy, MCP JSON-RPC and webhook signatures by hand. The SDKs are
exercised by the cross-client suite below.

The product under test is the post scheduler: posts in collections go out to
channels; a channel on "your own automation" (provider `custom`) makes staqly
emit `post.publish_requested` and wait for a result; schedules publish the
waiting drafts; the queue runs it all as jobs.

## Running

Requires Docker with Compose v2 and the workspace installed (`pnpm install`).

```bash
pnpm test:e2e                            # from the repo root (turbo, builds the SDK first)
pnpm --filter @staqly/e2e test:e2e       # just this package
make test-e2e                            # same as pnpm test:e2e
```

The suite builds the image, starts the stack, runs the files one by one and
removes the stack with its volume at the end.

| Variable | Effect |
|---|---|
| `E2E_BUILD=0` | Do not build; use the existing `staqly-e2e:test` image (CI builds it itself) |
| `E2E_KEEP=1` | Leave the stack running afterwards for inspection (it is in cloud mode by then) |
| `E2E_REUSE=1` | Neither start nor remove the stack: run selected files against one that is already up, in the mode those files expect (`npx vitest run tests/03c-webhook-delivery.test.ts`) |

The stack is an isolated compose project (`staqly-e2e`) with its own
containers, image, volume and ports, so it runs next to your own staqly and
never replaces the released `ghcr.io/staqly/staqly` image:

| Service | Address on this machine |
|---|---|
| API (`staqly-e2e`) | `http://localhost:13333` |
| Dashboard (`staqly-e2e`) | `http://localhost:18080` |
| Webhook and ntfy receiver, schedule endpoint (`staqly-e2e-receiver`) | `http://localhost:19000` |

Settings live in [`e2e.env`](e2e.env) and are passed with `--env-file`. To run
the stack by hand:

```bash
docker compose -f compose.yml -f compose.e2e.yml --env-file e2e/e2e.env up -d --build --wait
docker compose -f compose.yml -f compose.e2e.yml --env-file e2e/e2e.env down -v
```

Leftovers of a previous run (for example after `E2E_KEEP=1`) are removed
before the next run starts.

## What each file covers

Files run sequentially in name order on one stack. Each file creates its own
data; the only shared state is the stack itself: `07-cloud-mode` switches the
same volume from local to cloud mode, and `08-signup-and-plans`, `09-cloud-authz`
and `10-cloud-plans-and-races` need that.

| File | Covers |
|---|---|
| `01-local-mode` | Workspace created automatically with its Default collection and the provider catalog (`custom` available); setup disabled (409); no request without the access token, also from this machine (401, wrong token too); the token holds every scope (no welcome wizard); `server token` prints it (fixed by `STAQLY_LOCAL_TOKEN` in `e2e.env`, so `--rotate` is refused) and the start printed the `#token=` dashboard link; dashboard API refused without a session; browser sign-in with the token and with a one-time link from `server connect-link` |
| `01b-local-guards` | No keyless way in: every Host, Origin and X-Forwarded-Host without the token is 401 (REST and MCP), wrong and malformed credentials are 401; with the token any of them work; the exposed dashboard needs a session whatever the headers, a wrong token signs nobody in, CSRF through the proxy, device links once, logout of one browser |
| `02-posts-and-dashboard` | A picture uploaded and a post with two custom targets (one with its own text) created over REST; `/posts/:id`, `/schedule`, `/posts`, `/channels`, `/alerts` render; `/inbox`, `/notifications`, `/collections` redirect (301); the post and its picture through the proxy; 12 MB upload streamed through the proxy; the browser schedules, moves and clears the time with session and CSRF (a draft cannot be cancelled, a past time or a post without a channel is 422); a stale `expected_version` is 409; the history; duplicate and delete; the collection's status counts |
| `03-webhooks` | The custom channel round trip: publish now puts the post in publishing and asks the automation once per channel (`post.publish_requested`, actor `publisher`); the receiver fails the first delivery, the retry succeeds; the signed payload names the post, the channel, the target's text and the timeout; the automation reports one published target (with URL and id) and one failed: the post is failed, `post.target_*` and `post.failed` events; retrying the failed target asks again, its success publishes the post; a published post is read-only (409); a disabled channel fails its target without asking anyone |
| `03b-queues` | Webhook outbox: unknown and review-era event patterns refused (422), a delivery that keeps failing gives up after `max_attempts` and succeeds on a manual retry, only subscribed events of subscribed collections arrive; schedules: validation (rule, time, source, pick), the next waiting post (oldest first, the schedule's channel becomes the target, a post with its own time is not waiting), every waiting post, chosen posts (a post's own target text wins), a schedule without channels, `source: agent` (`schedule.posts_requested`, `GET /v1/work` until a draft arrives), `source: endpoint` (signed request to the receiver, `{posts: [...]}` created with `source: schedule:<name>` and published; a failing endpoint is `schedule.failed`), disabled and once schedules; the job queue: list, get, stats, cancel a queued job (200), 409 for retrying a done job or cancelling twice, a re-schedule queues a fresh job under the same key |
| `03c-webhook-delivery` | Exact payload and headers, HMAC checked by hand (tampered body, wrong or rotated secret, stale timestamp); retries keep delivery and event IDs; exact and glob patterns (`post.*_requested`); `"*"` never gets audit or webhook events, naming the family does; pause/resume; a receiver slower than `timeout_ms`; a receiver that is gone (gives up, manual retry delivers once back); the test endpoint; invalid settings (422) |
| `03d-alerts` | ntfy pushes: "Published: <first line>" with the preview and a link to the post, "Publish failed: ..." for a failed one, `post.publish_requested` as "Your automation is asked to publish"; a digest window bundles posts; event and collection filters; validation (type, events, config); the test endpoint; auto-disable after `NOTIFY_MAX_FAILS=3` failures and enabling again |
| `03e-input-and-races` | Broken or trailing JSON (400), unknown fields and wrong types (400), a missing collection, unknown media and channels, a scheduled post without a time (404/422), over 2 MiB (413), content over the limit (422), other content types (415 with a JSON error), Unicode content found by search, tags trimmed and limited, NUL and invalid UTF-8, guessed and malicious IDs (404), tampered cursors (422), pagination in both orders, bad filters and FTS syntax; Idempotency-Key replay, mismatch and limits (a publish replayed, not started twice); 20 parallel publishes of one post (one 202, one job), schedule and cancel racing, parallel PATCH with and without `expected_version`, 50 parallel creates |
| `03f-workers` | The clock: a post scheduled 6 s ahead is published by the scheduler (locked once started), a once schedule runs at its time, publishes the waiting draft and disables itself, a custom target nobody reported on fails after the collection's one-minute processing timeout ("No result reported in time", a late result is 409, the timeout job is done and cancelled for a target answered in time); limits of the timeout and the publish time; the session's server default |
| `04-mcp` | Streamable HTTP with the access token: initialize, 100 tools with the posts vocabulary and nothing of the review era, `create_post`; STDIO via `docker exec -i staqly-e2e mcp-stdio` (no key; the access token passed as `STAQLY_API_KEY` is taken as none); the round trip `connect_channel` → `create_post` → `schedule_post` → `preview_post` → `publish_post` → `list_requested_work` → `report_publish_result` → `get_post_history`; a `soon` platform refuses to connect; `create_schedule` with `source: agent`, `run_schedule` asks for posts, the agent's draft is published on the next run, `list_jobs` |
| `05-search-and-devices` | Full-text search by a word prefix, within a collection; a phone on the LAN without a session is refused (401), redeems a QR link, schedules and cancels a post; the link works once (410) |
| `06-persistence` | Posts, media, channels, schedules and the schedule's queued job survive `compose restart` and `up --force-recreate`; a backup taken with `server backup` and restored like `scripts/restore.sh` (the database and `secret.key` swapped in the volume while the server is stopped) brings them back, a post written after the backup is gone, and the channel still answers a test and takes a post (its credentials unseal with the restored key) |
| `06b-export` | Workspace export ZIP: one export at a time, every file present (`posts.ndjson` with targets, `channels.json`, `schedules.json`, ...), posts, targets, events and media bytes match the API, no webhook or endpoint secret anywhere in the archive, no credentials in `channels.json`, unknown export IDs |
| `06c-outbox-restart` | SIGKILL of the API with deliveries queued behind two held by a slow receiver: after the restart every queued event is delivered exactly once, the two in flight stay pending under their 2-minute lease and are delivered again when it runs out |
| `06d-notify-outbox-restart` | The same for alerts (ntfy to the receiver): SIGKILL while one push is held open and another alert's five are queued; after the restart each queued post is pushed exactly once, and the push in flight is sent again at once (no lease), not lost |
| `07-cloud-mode` | Re-creates the container with `STAQLY_MODE=cloud`: credentials required (401); recovery key from `server recovery-key` opens a session; CSRF enforced; a writer key (`posts:write` without `posts:publish`) sees the local data, edits, but may not publish, schedule or create a scheduled post over REST or MCP; MCP without a key or with the local access token refused; the local token is no key in cloud mode and `server token` refuses |
| `08-signup-and-plans` | Signup needs `SETUP_TOKEN`; a new isolated workspace starts on the Starter trial with its Default collection; its 6th channel gets 402 `channels`; `server set-plan ... plus` lifts it |
| `09-cloud-authz` | Every one of the 16 scopes allowed and denied on representative endpoints (REST matrix with the read scopes held by every key, MCP); a lone write scope cannot create a post; bearer header variants (401); unknown and retired scopes, past expiry (422); expiring, revoked and rotated keys; no privilege escalation by non-admin keys; hardened session cookie; CSRF missing, wrong or from another session; logout and session revocation; recovery key rotation (single use, signs other browsers out); a second workspace guessing the first one's IDs (posts, collections, channels, schedules, jobs, webhooks, deliveries, media, exports, alerts, keys, sessions: all 404, nothing changed, nothing listed, MCP refused, B's post cannot target A's channel); private webhook targets and schedule endpoints never called in cloud mode |
| `10-cloud-plans-and-races` | Free limits with 402 details (API keys, webhooks, alerts, channels, upload size, event export) and parallel creates that never overshoot; 10 parallel publishes of one post (one wins, one request to the automation); parallel PATCH with `expected_version`; parallel posts stop exactly at 50 (duplicating counts too); Idempotency-Key per key; data-only export on Free, media files and redacted secrets on Pro; an ended Pro period is read-only (402 `plan_expired`) until Free is chosen |

When MCP tools are added or removed, update `TOOL_COUNT` in
`tests/04-mcp.test.ts`.

## Layout

```
compose.e2e.yml      project name, container and image names, receiver service (repo root)
e2e/e2e.env          ports and settings of the e2e stack
e2e/receiver/        webhook/ntfy receiver and a schedule's posts endpoint (fails the first POST; GET /requests, POST /reset, POST /fail?n=, POST /delay?ms=, PUT /posts, POST /posts)
e2e/fixtures/        images used by the tests and by scripts/seed.sh; clip.mp4 and report.pdf for the UI suite
e2e/lib/             global setup, compose and docker helpers, raw HTTP and cookies, the REST client, the publishing flow (wait for the automation, report), MCP, polling, cloud workspaces and keys, receiver control, ZIP reader
e2e/tests/           the test files above
e2e/perf/            dashboard performance run: seed.mjs (large workspace) and run.mjs (Chrome over CDP); see Performance
```

## CI

The `e2e` job in `.github/workflows/ci.yml` builds the staqly image once (with the
GitHub Actions layer cache) tagged `staqly-e2e:test`, checks formatting, lint and types of this package,
then runs `pnpm turbo run test:e2e --filter=@staqly/e2e` with `E2E_BUILD=0`. On pull requests,
`ci.yml` runs affected jobs only (`.github/scripts/changes.py`), comparing the checkout's merge commit
to its first parent; a missing base runs everything. Pushes to main run every job: that run is the one a
release checks. Image changes include the dashboard, shared packages and `.dockerignore`.
Workflows, the lockfile and root configuration run every job.

Nightly and manual CI runs also set `TURBO_FORCE=true` to execute tasks even with a warm Turbo cache.
`make release` may tag main while the push's CI still runs (it refuses a failed one): `release.yml` builds
the image at once, then publishes and deploys only after that CI run passed.
Only superseded PR runs are cancelled; each main push keeps its own run. Docker E2E runs alongside the
API jobs; `internal/httpapi` on PostgreSQL is split over three runners (every third test each) and a fourth
runs the other packages.
The `workflows` job runs actionlint and the change-selection tests
(`make lint-workflows`, `python3 -m unittest discover -s .github/scripts`).

The setup action optionally caches `.turbo` and `apps/*/.next/cache` for JS package jobs.
Caches are separated by OS, architecture, workflow, job, Node/lockfile hash and commit, with a
restore prefix for earlier commits. Turbo also hashes the OpenAPI spec and build-time environment
(URLs and docs base path). Go jobs keep using setup-go's cache; their Turbo results are not persisted.
CI caches the sqlc executable separately, using the version from `apps/api/package.json`'s `gen`
command; local `pnpm gen` is unchanged.

The live suites below have workflows of their own; each runs the same commands
as locally:

| Workflow | When | Runs | Secrets |
|---|---|---|---|
| `cross-client.yml` | relevant pushes/PRs (API, CLI, SDKs, shared test helpers or the suite), nightly, by hand | Go, uv and Node; builds the TS SDK, then `pnpm --filter @staqly/e2e test:cross-client` | none |
| `n8n.yml` | relevant pushes to `main` (image, compose, integration or suite), nightly, by hand (`scenarios` input) | the image built from the checkout (layer cache) as `ghcr.io/staqly/staqly:latest`, `docker compose -f compose.yml -f compose.dev.yml up -d --no-build --wait` (`make dev` without the browser), then `pnpm --filter @staqly/e2e test:n8n`; `results.json` as an artifact | none |
| `ui.yml` | nightly, by hand | Go and Node, PostgreSQL as a service; `make dashboard`, Playwright's Chromium, then `UI_SKIP_DASHBOARD=1 pnpm --filter @staqly/e2e test:ui` and `pnpm --filter @staqly/e2e test:ui-responsive`; the report and traces as an artifact on failure | none |

The real-agent suite (`e2e/agent/`) has no workflow: it costs money, so it runs only by hand (below).

## Real-agent MCP suite

`agent/run.mjs` drives a live staqly with real agent CLIs: each scenario runs
`claude -p` (Sonnet, without the user's settings, hooks or plugins), `codex exec`
(`CODEX_MODEL`, default `gpt-6.1-sol`) or `opencode run` (`OPENCODE_MODEL`, default
`openai/gpt-6.1-sol`) with only the staqly MCP tools, then checks the result over
REST. It covers a collection with a processing timeout and a channel on the
agent's own automation, the provider catalog and a platform still `soon`,
writing drafts and scheduling posts, publish now with the agent playing the
automation (`list_requested_work`, `report_publish_result`), a failed target
and its retry, a schedule asking the agent for posts, creating a weekday
schedule, a stale `expected_version`, a published post that can only be
duplicated, tags, search and taking posts off the schedule, webhooks (`"*"`
without audit events, signed), pattern validation and recovery from a
review-era event name using only the error, a schedule fed from the suite's
endpoint, an ntfy alert, media upload, exports, delete and purge, the job
queue; plus multi-agent runs (a writer publishes while another CLI plays the
publisher and a third adds a schedule; three CLIs racing to publish one post),
prompt injection in post content, STDIO vs HTTP parity and cursor pagination
over 230 posts. In cloud mode it adds scope enforcement with read-only, writer
and publisher keys, a key revoked mid-session (HTTP and STDIO), and a
workspace whose paid plan has ended (`plan-expired`, HTTP and
`plan-expired-stdio`): the suite signs up a workspace of its own, ends its Pro
plan with `set-plan ... --until 2020-01-01`, and the agent's create and publish
must come back as tool errors with the `plan_expired` message (not protocol
errors), nothing is written, reads still work, the agent explains it and does
not try a workaround (plan, billing, keys).

```bash
make dev                                                # local mode: once
pnpm --filter @staqly/e2e test:agent                    # claude, local, all scenarios (about 5 min, about $2)
node e2e/agent/run.mjs --agent=codex webhook-star       # codex, selected scenarios
node e2e/agent/run.mjs --agent=opencode                 # opencode, all scenarios (about 5 min, about $1)
node e2e/agent/run.mjs --compare a/results.json b/results.json c/results.json   # same end states for every CLI?

# local mode served elsewhere, e.g. `staqly serve --port 4333` from this checkout, with its access token
STAQLY_LOCAL_URL=http://127.0.0.1:4333 STAQLY_API_KEY=$(staqly serve token) STAQLY_STDIO='["/path/to/mcp-stdio"]' \
  STAQLY_STDIO_ENV='{"STAQLY_MODE":"local","DATA_DIR":"/path/to/data"}' node e2e/agent/run.mjs

# cloud: a cloud-mode server built from this checkout, with a signed-up workspace
eval "$(e2e/agent/cloud-server.sh start)"               # CLOUD_PORT (33333), STAQLY_CLOUD_DIR
node e2e/agent/run.mjs --env=cloud --agent=opencode
e2e/agent/cloud-server.sh stop
```

Flags: `--agent=claude|codex|opencode` (the main agent; multi-agent scenarios
use the other two, `STAQLY_PEERS=claude,codex` to leave one out),
`--env=local|cloud`, `--transport=http|stdio`. Needs the `claude`, `codex` and
`opencode` CLIs signed in. Locally it uses the dev stack (`localhost:3333`,
`docker exec -i staqly mcp-stdio`, its access token from `docker exec staqly server token`,
which the agents' HTTP MCP config sends as `Authorization: Bearer`); `STAQLY_LOCAL_URL`,
`STAQLY_API_KEY` (that server's token), `STAQLY_STDIO` and `STAQLY_STDIO_ENV` point it at another
local-mode server. In cloud it needs
`STAQLY_CLOUD_URL`, an admin key in `STAQLY_API_KEY`, the STDIO command, and
`STAQLY_OPERATOR` (the server's operator command as a JSON argv prefix, e.g.
`["staqly","serve","--data","/srv/staqly"]`) for `plan-expired`;
`cloud-server.sh start` prints all of them. The webhook receiver and the
schedule endpoint listen on a free port (`E2E_RECEIVER_PORT`), reached as
`host.docker.internal` from the dev stack and on this machine's LAN address
otherwise (webhooks never go to loopback). It cleans up what it created
(collections, channels, schedules, webhooks, alerts, its API keys, the
`plan-expired` workspace); transcripts and `results.json` (per scenario: tools
called, tool errors, retries, cost, tokens, an end-state snapshot; per tool:
calls, errors, unexpected errors and recoveries; the models used) go to
`$TMPDIR/staqly-agent-e2e/<run>/`. Codex reports tokens, not dollars
(`CODEX_USD_PER_MTOK=in,cached,out` for an estimate). `AGENT_TIMEOUT_SECONDS`
(900) kills a hung CLI run, `E2E_MAX_COST_USD` stops starting scenarios once
reached, `E2E_CONCURRENCY` (5) sets how many run at once. The dev stack runs
the released image unless built from the checkout (`make dev`). It is not part
of `pnpm test:e2e` and no CI workflow runs it: start it by hand when MCP tool names or descriptions change.

OpenCode runs isolated: the suite starts one private `opencode serve` with its
own config, state and cache directories (none of your OpenCode config, MCP
servers, plugins, instructions or skills; credentials come from `opencode auth
login` or the provider's environment variable, e.g. `OPENAI_API_KEY`). Each
agent run gets a directory whose `opencode.json` has only the staqly MCP server
(`codemode: false`) and denies the built-in tools (shell, read, edit, web,
subagents, code mode); a call of any other tool fails the scenario. The suite
waits for that directory's MCP connection before `opencode run --server`:
OpenCode 2.0.25 connects MCP servers when a directory is first used, and a
prompt sent at that moment gets a first step without the MCP tools.

## Real n8n suite

`n8n/run.mjs` is a real-integration test against a real n8n (not mocked; CI
runs it on pushes to `main` and nightly, `n8n.yml`). Next to the running dev
stack it starts n8n from
`integrations/n8n/compose.yml` (project `staqly-n8n-e2e`, pinned image, port
`N8N_PORT`, default 25678, joined to `staqly_default`), creates the n8n owner and
an API key, imports workflows over n8n's public API and checks every side effect
over the staqly REST API. Posts go to a channel on "your own automation"
(provider `custom`): staqly sends `post.publish_requested` to the n8n consumer
workflow, which publishes with a mock and reports the result.
Every call to staqly, the runner's own and those of the workflows' HTTP Request
nodes, carries the dev stack's access token (`STAQLY_API_KEY`, else
`docker exec staqly server token`).

| Scenario | What runs |
|---|---|
| `demo` | `setup-demo.sh` + the shipped `instagram-avatar-demo.json`: collection, channel and webhook created; token-protected generate; a post with a PNG published right away on the n8n channel → `post.publish_requested` → Mock Publisher → reported published with the link and the platform id; `post.published` reaches Record outcome; forged event rejected and changes nothing |
| `publish-success` | n8n creates and publishes, the consumer reports `published` with url and external_id; deliveries of `post.publish_requested` and `post.published` succeeded; a post without publish stays a draft |
| `failure-retry` | the consumer reports `failed` with an error, cURL retries the target, the second request is published (attempts 2; two `post.publish_requested` in the history) |
| `endpoint-down-auto-retry` / `-manual-retry` | inactive n8n workflow (404): the delivery stays queued with backoff and is retried after reactivation; `max_attempts: 1` + `POST /v1/webhook-deliveries/{id}/retry` |
| `signature` | wrong secret, tampered body, stale timestamp, missing signature fail the n8n execution and change nothing; a valid event is processed once, its replay is deduplicated |
| `idempotency` | same `Idempotency-Key` → same post (`Idempotent-Replayed`); n8n "retry execution" creates no duplicate |
| `poll` | n8n polls `GET /v1/work` and reports every waiting target of its collection as published; the list empties |
| `schedule-endpoint` | a schedule with `source: endpoint` asks an n8n webhook (signed request verified in n8n) and publishes the two posts it returns; a secret n8n cannot verify fails the run and creates nothing |
| `concurrency` | 8 parallel executions → 8 posts published, 5 parallel with one key → 1 post, each post published exactly once |
| `mcp-continues` | MCP `create_post` + `publish_post` → n8n publishes; n8n writes, MCP publishes, `list_requested_work` and `report_publish_result` finish it |

```bash
make dev                                      # once
pnpm --filter @staqly/e2e test:n8n            # all scenarios (about 1 min + n8n start)
node e2e/n8n/run.mjs demo signature           # selected scenarios
KEEP=1 node e2e/n8n/run.mjs demo              # leave n8n and the data for a look
```

It deletes the collections (`n8nE2E...`), channels, schedules and webhooks it
created and removes the n8n container and volume; `results.json` goes to
`$TMPDIR/staqly-n8n-e2e/<run>/`.

## Cross-client suite (live, no Docker)

`cross-client/` runs every client against real servers built from this
checkout: the TypeScript SDK, the Python SDK (sync and async, through
`cross-client/py_worker.py` in the SDK's uv environment), the `staqly` CLI,
the real `curl` binary and MCP over Streamable HTTP. Its global setup builds the
CLI (`go build`) and starts three `staqly serve` processes with temporary data
directories: local mode (every client sends the access token the server made
in its data directory, `access_token`), cloud mode, and cloud mode with a tiny per-key rate
limit (burst 3, then one request a second) for 429 / `Retry-After`. Nothing is
mocked; webhook and ntfy receivers are local HTTP servers.

The cloud servers run on PostgreSQL (cloud mode needs it), each in its own schema:
point `STAQLY_E2E_DATABASE_URL` at a server, e.g.
`docker run -d --rm -p 55432:5432 -e POSTGRES_PASSWORD=staqly postgres:17.6-alpine` and
`STAQLY_E2E_DATABASE_URL=postgres://postgres:staqly@127.0.0.1:55432/postgres?sslmode=disable`.
The Docker suite brings its own (`postgres` in `compose.e2e.yml`); `07-cloud-mode` moves the local
SQLite data into it with `server import-sqlite`.

```bash
pnpm --filter @staqly/e2e test:cross-client                  # all files, about 2 min
pnpm --filter @staqly/e2e test:cross-client 02-cross-client  # one file
XC_BIN=/path/to/staqly pnpm --filter @staqly/e2e test:cross-client   # skip the build
```

Needs Go, uv and curl. Ports (127.0.0.1): `XC_LOCAL_PORT` (23333),
`XC_CLOUD_PORT` (24333), `XC_LIMITED_PORT` (25333), each dashboard on the port
+5000, and `XC_CRASH_PORT` (25433) for a server killed mid-wait.
`XC_KEEP=1` leaves the servers running. Not part of `pnpm test:e2e`; CI runs it
nightly and on pushes to `main` or pull requests that touch its inputs (`cross-client.yml`), after building the TS SDK
(`pnpm turbo run build --filter=@staqly/sdk`), which the suite imports.

| File | Covers |
|---|---|
| `01-sdk-behaviour` | Typed errors (401/402/403/404/409/415/422/429) identical in TS, Python sync and async (publishing twice, a stale `expected_version`, a time in the past, a writer key that may not publish, Free channel and webhook limits); retries honouring `Retry-After`; `Idempotency-Key` sent on unsafe JSON requests only, a create whose response was lost retried and applied once, explicit keys replayed across clients (curl gets `Idempotent-Replayed`), mismatching bodies refused; iterators over several pages (posts, events) in the same order; 12 MB upload and download byte for byte, a post carrying the file, type from the bytes not the declared Content-Type; streamed event exports; `waitForPublished` / `wait_for_published` released by a result report from curl (published and failed), timing out, aborted (TS signal, Python task cancellation); `verifyWebhook` on real deliveries, tampering and old timestamps refused; a server that never answers (TimeoutError), connection refused and a server killed while every SDK waits (APIConnectionError, no hang) |
| `02-cross-client` | The same create → publish → report result through TS, Python sync, Python async, CLI, curl and MCP: identical posts and targets, history events and webhook payloads (which equal the stored events), told apart only by `actor.agent`; CLI connects a channel → Python writes → curl schedules (stale version 409) → TS waits → MCP reports a failure → CLI retries → Python reports success; TS duplicates a published post → MCP edits the copy → curl tags it → CLI publishes with `--wait` while Python plays the automation; 12 concurrent publishes from all clients with exactly one run, concurrent metadata patches without lost updates, one winner among equal `expected_version`s, one post for a concurrently repeated `Idempotency-Key` |
| `03-cli` | Every command group in both modes: exit codes (0, 1, 2 failed, 3 timeout, 64 usage), `--json` output, no review-era commands, name-addressed collections (rename, timeout, archive, delete), `posts create` as a draft, with `--publish-at` (needs a channel), `--now --wait` (published 0, failed 2, timeout 3), `--file`, `--stdin`, `@file`, `--idempotency-key`; posts list (`--all` paging, filters), get, update (`--expected-version`, `--clear-publish-at`, `--no-channels`), schedule (relative times), cancel, duplicate, preview, history, delete, purge (admin), tags, the `schedule` calendar; publish, `work`, `result` by channel name, `retry --wait`; channels (providers, connect your own automation, a `soon` platform refused, update, test, remove); media; events; webhooks; alerts; schedules (times, every, once, run) and jobs (list, stats, get, cancel, retry); workspace export; keys, plans, profiles (`login` / `logout`), billing without Stripe, `staqly serve` operator commands against a live data directory |
| `04-method-sweep` | The remaining SDK methods in TS and Python: server info, collections (`listPosts`, the processing timeout, archive refuses new posts, delete), posts (search, tags, preview, moving between collections, purge), providers and channels (catalog, a `soon` platform and an unknown one refused, test, disable, enable, remove, `oauthStart` on a platform on the way), alerts (masked secrets, enable, update, test against nothing, deliveries), workspace and exports, sign-in with an API key, device links (dashboard session only, single use), billing without Stripe, recovery keys (single use) |
| `05-schedules` | Schedules, jobs and the work list across clients: Python drafts → CLI creates a weekday schedule → curl runs it (the oldest draft goes to the schedule's channel, `GET /v1/work` lists it in every client) → TS `waitForPublished` released by curl's result report → the queue read by TS, Python, CLI and MCP (a done job can neither be retried nor cancelled) → CLI toggles, Python runs, MCP deletes; a schedule with `source: agent` asks for posts until MCP writes one; a once schedule from the CLI with a relative time; validation the same everywhere |

## Dashboard UI suite (Playwright, no Docker)

`ui/` drives the real dashboard in a real browser (Playwright, headless
Chromium, desktop 1440x900) against `staqly serve` built from this checkout:
its global setup builds the dashboard's static export, embeds it like `make
dashboard` (into `apps/api/web`), builds the binary and starts a local-mode
server with `DASHBOARD_EXPOSED=true` (as in the Docker image; every browser
signs in once with the access token the server made in its data directory, the
global setup with the `#token=` link it prints) and a cloud-mode server with `SETUP_TOKEN`,
each with a temporary data directory, plus a loopback receiver for webhooks,
alerts and a schedule's endpoint. Both workspaces are seeded over REST: three
channels on your own automation and a disabled one, collections (14 active and
one archived), posts in every status (drafts, scheduled in the coming days,
publishing, published with links, failed on one channel) with pictures, a
video, tags and an edited one, schedules (weekday mornings, asking the agent,
once, paused), webhooks with succeeded, failed, pending and paused deliveries,
ntfy and email alerts, and in cloud API keys (agent, read-only, revoked). The
browser sessions are saved once (`ui/.state/`) and reused by the specs.

```bash
pnpm --filter @staqly/e2e test:ui                          # everything, about 6 min
pnpm --filter @staqly/e2e test:ui 04-post                  # one file
pnpm --filter @staqly/e2e test:ui --project=cloud          # one mode
UI_KEEP=1 UI_REUSE=1 pnpm --filter @staqly/e2e test:ui 03-posts   # keep the servers and reuse them on the next run
npx playwright show-report ui/.report                       # the HTML report (traces of failures in ui/.results)
```

Needs Go, Node and a Playwright Chromium (`npx playwright install chromium`
once). Ports (127.0.0.1): `UI_LOCAL_PORT` (61333) / `UI_LOCAL_DASHBOARD_PORT`
(61080), `UI_CLOUD_PORT` (61433) / `UI_CLOUD_DASHBOARD_PORT` (61480),
`UI_RECEIVER_PORT` (61590) and `UI_DOWN_PORT` (61633, +50 for its dashboard)
for the server the error spec stops and restarts. `UI_BIN=/path/to/staqly`
skips both builds, `UI_SKIP_DASHBOARD=1` reuses the export already in
`apps/api/web`. Specs run one at a time against the shared servers; each one
that changes data makes its own collection (with a channel and drafts of its
own). Not part of `pnpm test:e2e`; runs nightly in CI (`ui.yml`).

| File | Covers |
|---|---|
| `local/01-auth` | A new browser sees the access-token sign-in with both commands (`staqly serve token`, `docker exec staqly server token`); a wrong token is refused, the right one signs in and survives a reload; the `#token=` link the server prints signs in on any page and leaves no token in the address bar; a connect link signs another browser in once (reused and malformed links refused) |
| `local/02-shell` | Every route from a deep link and after a refresh, without console errors; document titles; no Unicode ellipsis, the name "staqly" and no review-era word in all text, placeholders and labels; a post's sheet survives a refresh; `/inbox`, `/notifications`, `/collections` redirect; sidebar (Schedule is home, Posts, Channels, Alerts, the bell in the footer); back/forward; command palette (Cmd+K, sidebar search, filter, Enter, no results, Escape); theme light/dark/system across reloads; workspace menu and Connect a device with focus return; skip link |
| `local/03-posts` | The collections pane and the open collection: rows with status and the channel stack; status, search and channel filters in the URL and across a refresh; archived collections behind a switch (no new posts there); a row opens the post as a drawer with the id in the URL; the composer: validation, the collection preselected, channel avatars, the character counter and per-channel tab, Save draft, a time turns Publish now into Schedule, Publish now hands the post to the automation and the list follows the result live; a channel's own text; a disabled channel cannot be picked; a post created over REST appears live; the pane counts scheduled posts |
| `local/04-post` | A published post: content, picture, tags, channels with their links, read-only (no Edit, no Publish now); a failed post: the channel's error, Retry asks the automation again, the result publishes it; a draft: Publish now, the sheet follows the result live; schedule from the sheet, move and clear it; a post without channels cannot be published; Edit in the composer with a 409 from another writer (nothing lost, the other version shown, the next save works); Duplicate (a toast, the copy is a draft); Delete; the not-found state |
| `local/05-collections` | Create with validation and a name already in use; settings: rename (a taken name under the field), the processing limit, the drawer stays open after Save; archive and unarchive from the folded Danger zone (archived refuses new posts); schedules: a weekday schedule (times, weekdays, time zone, channels, asking the agent), Run now publishes the waiting draft on the schedule's channel, off and on, edit to every waiting post, delete; a once schedule needs a future time; delete with the exact name; the Default collection has no Danger zone; a collection that does not exist |
| `local/06-activity` | Event type, actor and date filters in the URL and across a refresh; links to the post and Data; the post filter; JSONL and CSV exports of the filtered log; live events |
| `local/07-webhooks` | Hooks in every state ("up to 1 attempt"); succeeded, failed (details with the receiver's answer) and pending deliveries; create with validation, the secret shown once, Send test, pause, edit, rotate, delete; retry a delivery |
| `local/08-channels` | The list grouped by platform with Connected and Disabled; Add channel: platforms in groups, the ones on the way greyed with Soon, connect your own automation (back to the platforms and again); rename, test the connection, disable and enable, remove |
| `local/09-alerts` | Alerts with health, the usage chip, Send test, deliveries, pause and resume; a failing email alert explains itself; create an ntfy alert with validation, it receives a published post, edit, delete |
| `local/10-settings` | Sections and aria-current; workspace rename; devices (connect dialog, a new code, focus return, signing another browser out); plan & usage meters for posts, channels and alerts; System → Queue counts, status filter, cancel a queued publish; activity downloads and the workspace ZIP; backups and system; integrations: every client tab's snippet, transport radios, Copy, Test connection, Agents & activity |
| `local/11-errors` | Skeletons on a throttled network; a 500 on a list with Try again; a 500 on publish keeps the post a draft; two tabs publishing the same post (one run, a clear conflict); the server stopped mid-session (a clear error, the sheet stays) and restarted on the same data (the same button works) |
| `local/12-a11y` | axe-core (WCAG 2.2 A/AA, serious and critical) on every page in light and dark mode and on every dialog and sheet (composer, post sheet, reschedule, new collection, collection settings, new schedule, add channel, new alert, new webhook, connect device, activity filter, slot dialog, bell) with reduced motion; a keyboard-only draft from the skip link to Save draft with a visible focus ring; focus trapped in dialogs and sheets and given back |
| `local/13-schedule` | Home: the week with the seeded posts (time, title, collection, the channel stack), the count, day/week/month and list views, the collection schedules' runs as hollow chips, the now line; an entry opens the post; drag to another hour reschedules; past slots take no drop and have no +; + on a slot picks a waiting draft or writes a new post with the time set; published and failed states after a reload; an empty week says so |
| `local/14-bell` | A published post shows up as unread, Mark all as read clears the count across a reload, the row opens the post; a failed post and a request to the automation are listed too |
| `cloud/01-auth` | Deep link to /login and back; empty, wrong, agent (no admin) and admin keys; log out; recovery (wrong key, new keys shown once behind the acknowledgement, the used key refused afterwards); /setup with a wrong and the right setup token |
| `cloud/02-api-keys` | Active, revoked and last-used keys; create with validation and the presets (Read-only agent, Publisher automation, Full access), a key that works with exactly its scopes; an expiry in the past; rotate (old key refused, new one works) and revoke; axe on API keys, the key dialog, plan & usage, login and setup |
| `cloud/03-empty-workspace` | A new workspace's empty states (schedule, channels, alerts, webhooks, the Default collection, the composer without channels) and its first post arriving live |

## Responsive layout check (live, no Docker)

`ui-responsive/` opens every dashboard route (schedule in every view, posts,
a collection and its filters, posts of every kind and status, channels,
activity, alerts, webhooks, integrations, settings), plus the command palette,
menus, the composer with channels and its counters, the date-time picker,
collection settings, the danger zone, a new schedule, the post menu,
reschedule, add channel, new alert, new webhook, the activity filter, the slot
dialog, the bell and the phone drawer, in headless Chromium (Playwright) at
320×640, 390×844, a landscape phone (844×390), 768×1024, 1024×768, 1280×800 and
1920×1080, in local and cloud mode (signed in, and the sign-in and setup pages
signed out). Below 1024 px it emulates a touch screen; phones run in dark mode,
the rest in light. Its global setup builds the CLI (`go build`, with the
dashboard export embedded), starts a local-mode and a cloud-mode `staqly serve`
with temporary data directories and seeds both through the REST API with
layout-hostile data: four channels (one disabled), 34 collections, content
with long unbroken words and URLs, unicode, 12 tags, posts with pictures (made
by the browser: wide and tall PNGs) and a video, per-channel texts, every
reachable status, 12 scheduled posts across the week, schedules of every kind,
webhooks, alerts and cloud API keys.

A page fails on: the page scrolling sideways; an element wider than its parent
(the report names where the widening starts) or unbreakable text spilling out
of its box; a dialog, sheet, menu or the sidebar whose content is wider than
it; controls drawn over each other; touch targets under 40 px on touch screens
(inline links in running text are exempt, hit areas grown with
`pointer-coarse:` padding or an `after:` inset count); controls left under the
phone tab bar at the end of the page; page errors. Ellipsised text without a
`title` is printed, not failed.

```bash
make dashboard                                         # the export the binary embeds
pnpm --filter @staqly/e2e test:ui-responsive           # about 4 min
pnpm --filter @staqly/e2e test:ui-responsive local     # one mode
UI_BIN=/path/to/staqly pnpm --filter @staqly/e2e test:ui-responsive   # skip the build
```

Needs Go and Playwright's Chromium (`pnpm --filter @staqly/e2e exec playwright
install chromium`). Ports (127.0.0.1): `UI_LOCAL_PORT` (43333), `UI_CLOUD_PORT`
(43433), each dashboard on the port +5000; `UI_KEEP=1` leaves the servers
running. Checks live in `ui-responsive/checks.ts` (plain DOM code run in the
page), routes and overlays in `ui-responsive/routes.ts`.

## Background service (`staqly service`)

`service/` tests `staqly service install / status / logs / uninstall` against
the real service managers. Not part of `pnpm test:e2e` or CI.

```bash
e2e/service/run.sh          # macOS: the real LaunchAgent on this Mac, about 1 min
e2e/service/systemd.sh      # Linux: a Linux build in a Docker container with systemd as PID 1
```

`run.sh` builds the CLI (or tests `STAQLY_BIN`), installs `ai.staqly.serve` in
`~/Library/LaunchAgents` on API `127.0.0.1:43333` and dashboard `48080`
(`API_PORT`, `DASH_PORT`, `BUSY_PORT` change them) with a temporary data
directory, so it runs next to your own staqly or the Docker container. It
refuses to start when a staqly service is already installed and always
uninstalls at the end, printing the launchd state before and after. It checks
the plist (path, mode 0600, ports, data, `PATH`, KeepAlive), launchd state,
`/health`, the dashboard, the log and the AI CLI found through the captured
`PATH`, `launchctl kickstart -k`, a restart after `kill -9`, reinstalling
(also with a request in flight), a busy port being refused with the previous
service left running, uninstall (plist and job gone, data and log kept) and
re-install. `systemd.sh` checks the same on Linux plus `systemd-analyze
verify`, lingering, and that `PATH` and a value with `$`, `%` and quotes
reach the service unchanged; its container (`staqly-service-systemd`)
publishes no ports and is removed at the end.

## Performance

`perf/` measures the dashboard against a live server built from this checkout,
with a large workspace. Not part of `pnpm test:e2e` or CI (it needs Chrome and
a few minutes); the guards that do run in `pnpm check` are the bundle budget
(`apps/platform/bundle-budget.json`, `pnpm --filter @staqly/platform size`
after a build), the render counts in `apps/platform/tests/render-perf.*.test.tsx`
and the compression/ETag tests in `apps/api/internal/web` and
`apps/api/internal/httpapi/compress_test.go`.

```bash
make dashboard && (cd apps/api && go build -o /tmp/staqly-perf ./cmd/server)
export DATA_DIR=$(mktemp -d)
ADDR=127.0.0.1:63334 DASHBOARD_ADDR=127.0.0.1:63081 /tmp/staqly-perf &
export STAQLY_API_KEY=$(cat "$DATA_DIR/access_token")   # the access token the server made on its first start
node e2e/perf/seed.mjs --api http://127.0.0.1:63334     # 200 collections, 10k posts, ~50k events, ~1k failed deliveries (~6 min)
node e2e/perf/run.mjs --url http://127.0.0.1:63081 --json /tmp/perf.json   # signs Chrome in with the token
```

`seed.mjs` goes through the public API (events, versions and webhook
deliveries as an agent would make them): one channel on your own automation,
50 posts per collection, 30 % of them scheduled across the coming three weeks
(a full calendar), 20 posts with 100 versions, every other draft updated
`--updates` times, and a webhook to a closed port on the first 10 collections.

`run.mjs` drives Chrome (`--chrome` or `CHROME`, default the installed Google
Chrome) over the DevTools protocol, no npm dependency. `load`: each route cold
(`/schedule`, `/posts`, a collection, a post, `/channels`, `/activity`,
`/webhooks`, `/settings`), desktop (1440x900, 40 ms RTT, 10 Mbit/s) and mobile
(390x844, 4x CPU slowdown, 150 ms RTT, 1.6 Mbit/s, Lighthouse's slow 4G): FCP,
LCP, CLS, TBT (long tasks after FCP), requests (all / API / duplicate API),
transferred KB, heap; the JSON has each request's timing (`waterfall`).
`interact`: open a post from the list, "Load more" to 500 rows, the command
palette and post search typing, the activity feed's "Load older" (input to
next paint from Event Timing). `memory`: 2x100 client-side navigations and 30
post sheets, heap after GC, DOM nodes, listeners, live intervals and
EventSources.

Baseline of the posts dashboard (2026-10-21, `staqly serve` local mode, the
seed above: 200 collections, 10k posts, ~49k events; LCP in ms, desktop /
mobile):

| Route | LCP | TBT mobile | Transfer mobile |
|---|---|---|---|
| `/schedule` | 600 / 3060 | 109 ms | 705 KB |
| `/posts` | 1016 / 3780 | 93 ms | 707 KB |
| `/posts/:id` | 940 / 3590 | 142 ms | 900 KB |
| `/channels` | 580 / 3040 | 90 ms | 633 KB |
| `/activity` | 590 / 3080 | 90 ms | 713 KB |
| `/settings` | 600 / 3010 | 128 ms | 828 KB |

FCP is ~300 ms desktop and ~1.2 s mobile everywhere, CLS 0. `interact`: open a
post 121 ms, post search typing 16 ms max per key, palette typing 32 ms,
5x "Load older events" 1.3 s in all. `memory`: 200 navigations and 30 post
sheets keep DOM nodes flat (3479 → 3508), heap 10.7 → 15.8 MB, one interval.
The mobile floor is bandwidth: ~390 KB gz of first-load JS on slow 4G
arrives at ~3 s; the post editor (Tiptap, ~150 KB gz) loads with the sheet,
which shows the post's text until it is there.
