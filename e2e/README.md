# @staqly/e2e

End-to-end tests against the real Docker stack: the staqly image (API, MCP and
dashboard in one container) built from this checkout, started with `compose.yml` plus `compose.e2e.yml`, driven
over HTTP, MCP and the operator CLI. Tests are written with Vitest and use the
TypeScript SDK (`@staqly/sdk`) wherever an agent would, so they test the SDK
too. Raw HTTP is used for what the SDK hides on purpose: status codes, guard
headers (Origin, Host), cookies and CSRF, the dashboard proxy and MCP JSON-RPC.

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

The stack is an isolated compose project (`staqly-e2e`) with its own
containers, image, volume and ports, so it runs next to your own staqly and
never replaces the released `ghcr.io/staqly/staqly` image:

| Service | Address on this machine |
|---|---|
| API (`staqly-e2e`) | `http://localhost:13333` |
| Dashboard (`staqly-e2e`) | `http://localhost:18080` |
| Webhook, ntfy and RSS receiver (`staqly-e2e-receiver`) | `http://localhost:19000` |

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
same volume from local to cloud mode, and `08-signup-and-plans` needs that.

| File | Covers |
|---|---|
| `01-local-mode` | Workspace created automatically; setup disabled (409); keyless access from this machine; foreign Origin and Host refused (403); dashboard API refused without a session; browser sign-in with a one-time link from `server connect-link` |
| `02-items-and-dashboard` | Image upload over REST (SDK); item and inbox pages render; item and image served through the dashboard proxy; 12 MB upload streamed through the proxy; approval with session and CSRF; second decision conflicts (409); item history |
| `03-webhooks` | Webhook on `item.approved`; the receiver fails the first delivery, the retry succeeds (2 attempts); HMAC signature checked with the SDK's `verifyWebhook` |
| `03b-queues` | Webhook outbox: unknown event patterns refused (422), a delivery that keeps failing gives up after `max_attempts` and succeeds on a manual retry, only subscribed events of subscribed collections arrive; ntfy notification channel retried after a failure, its one-tap approve link decides the item once (then 410); RSS watcher with keywords (baseline run, new entries become items once); REST long-poll wait (decided and timed out); image item without media refused (422) |
| `04-mcp` | Streamable HTTP: initialize, tool count, `create_item`; STDIO via `docker exec -i staqly-e2e mcp-stdio`; `await_decision` returns when a human approves concurrently |
| `05-search-and-devices` | Prefix full-text search; a phone on the LAN (non-local Host) is refused, redeems a QR link, reads and acts; the link works once (410) |
| `06-persistence` | Items and media survive `compose restart` and `up --force-recreate` |
| `07-cloud-mode` | Re-creates the container with `STAQLY_MODE=cloud`: credentials required (401); recovery key from `server recovery-key` opens a session; CSRF enforced; scoped API key sees the local data; a creator key cannot review over REST or MCP; keyless MCP refused |
| `08-signup-and-plans` | Signup needs `SETUP_TOKEN`; a new isolated workspace starts on a Pro trial; on Free, items are unlimited but the 51st approval gets 402 `decisions_per_month`; `server set-plan ... pro` lifts it |

When MCP tools are added or removed, update `TOOL_COUNT` in
`tests/04-mcp.test.ts`.

## Layout

```
compose.e2e.yml      project name, container and image names, receiver service (repo root)
e2e/e2e.env          ports and settings of the e2e stack
e2e/receiver/        webhook/ntfy receiver and RSS feed (fails the first POST; GET /requests, POST /reset, POST /fail?n=, PUT /feed, GET /feed.xml)
e2e/fixtures/        images used by the tests and by scripts/seed.sh
e2e/lib/             global setup, compose and docker helpers, raw HTTP and cookies, MCP, polling
e2e/tests/           the test files above
```

## CI

The `e2e` job in `.github/workflows/ci.yml` builds the staqly image once (with the
GitHub Actions layer cache) tagged `staqly-e2e:test`, checks formatting, lint and types of this package,
then runs `pnpm turbo run test:e2e --filter=@staqly/e2e` with `E2E_BUILD=0`.

## Real-agent MCP suite

`agent/run.mjs` drives the dev stack (`make dev`: local mode, mailpit) with a
real Claude Code agent: each scenario runs `claude -p` with only the staqly MCP
tools allowed, then checks the result over REST. It covers collection flows and
the action catalog, every item type, review, auto-run publish with
`report_result`, regenerate/fail/retry, stale `expected_version`, archive and
history, search and bulk actions, webhooks (`"*"` without audit events, signed),
pattern validation, an RSS watcher, an email channel approved through its
decision link, `await_decision` with reviewer feedback, media upload, exports,
delete and purge.

```bash
make dev                                      # once
pnpm --filter @staqly/e2e test:agent          # all scenarios (about 2 min, about $2.5)
node e2e/agent/run.mjs webhook-star media-upload   # selected scenarios
```

Needs the `claude` CLI signed in, and port 8799 free (webhook receiver and RSS
feed, reached from the container as `host.docker.internal`). It cleans up what
it created; agent transcripts and `results.json` go to `$TMPDIR/staqly-agent-e2e/<run>/`.
It is not part of `pnpm test:e2e` or CI.
