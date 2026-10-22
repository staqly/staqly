# Staqly — agent guide

Local-first workspace where people and AI agents write posts, schedule them
and publish them to connected channels (the posts pivot, 2026-10-19). Monorepo:

| Path | What | Guide |
|---|---|---|
| `apps/api` | Go backend: REST, MCP (stdio + Streamable HTTP), webhook worker, SQLite | [apps/api/AGENTS.md](apps/api/AGENTS.md) |
| `apps/platform` | Next.js dashboard (static export embedded in the server and image `ghcr.io/staqly/staqly`), including the media editor at `/media/edit` (OpenGPEX, GPL-3.0, in `lib/opengpex`) | [apps/platform/AGENTS.md](apps/platform/AGENTS.md) |
| `apps/cli` | `staqly` CLI and `staqly serve` (one binary, goreleaser) | — |
| `apps/docs` | docs.staqly.ai (Fumadocs, GitHub Pages) | [apps/docs/AGENTS.md](apps/docs/AGENTS.md) |
| `apps/web` | staqly.ai landing page (Next.js static export) | [apps/web/README.md](apps/web/README.md) |
| `packages/brand` | `@staqly/brand`: logo, hero demo, brand CSS tokens and site helpers shared by dashboard, docs and web (TS source, `transpilePackages`; CSS `@source`s itself) | — |
| `packages/check-links` | `@staqly/check-links`: link check of the docs and web static exports | — |
| `sdks/typescript` | TypeScript SDK (types from OpenAPI) | [sdks/typescript/AGENTS.md](sdks/typescript/AGENTS.md) |
| `sdks/python` | Python SDK (sync + async) | [sdks/python/AGENTS.md](sdks/python/AGENTS.md) |
| `integrations/` | n8n example + demo workflow, Claude Code / Codex configs | [integrations/AGENTS.md](integrations/AGENTS.md) |
| `scripts/` | install, backup, restore, seed | — |
| `e2e/` | Docker end-to-end suite (`compose.e2e.yml`) | [e2e/README.md](e2e/README.md) |

License: AGPL-3.0-only (`LICENSE`); `sdks/*` and `integrations/` stay Apache-2.0 (their own
`LICENSE`). Outside contributions need the CLA (`ICLA.md`, `CCLA.md`, `.github/workflows/cla.yml`).
Keep `NOTICE` updated if you add
code that requires attribution.

## Commands

```bash
pnpm install       # once: the whole workspace (Turborepo + pnpm; Go and Python packages have package.json wrappers)
pnpm check         # format:check, lint, typecheck, test of every package (turbo, cached)
pnpm turbo run test --filter=@staqly/api   # one package (api, cli, dashboard, sdk, sdk-python, web: the tools' logic; docs has none)
pnpm gen           # sqlc queries, dashboard API types, TypeScript SDK schema
pnpm test:e2e      # Docker end-to-end suite (e2e/, compose.e2e.yml)
pnpm --filter @staqly/e2e test:cross-client   # live, no Docker: TS + Python SDKs, CLI, curl, MCP against `staqly serve` (e2e/cross-client)
pnpm --filter @staqly/e2e test:ui   # the dashboard in a real browser (Playwright + axe) against `staqly serve` (e2e/ui)
pnpm --filter @staqly/e2e test:ui-responsive   # after `make dashboard`: every dashboard route at phone/tablet/desktop widths, fails on overflow, overlaps, small touch targets (e2e/ui-responsive)
make up            # compose.yml alone with the released image, open a signed-in browser (users run `docker run`, not this)
make dev           # build from this checkout with the dev stack (compose.dev.yml: mailpit, Stripe CLI)
make site          # landing page :8070 + docs :8090 from this checkout
make connect       # one-time sign-in link for another browser
make release VERSION=v0.2.0  # tag origin/main; CI publishes the binaries and ghcr.io/staqly/staqly:v0.2.0
```

Run `pnpm check` before finishing a change; run `pnpm test:e2e` for anything
touching auth, Docker, the dashboard proxy, webhooks or MCP.
Run `test:ui` for dashboard changes a user sees (flows, keyboard, accessibility).
Run `test:cross-client` for changes to an SDK, the CLI, idempotency, errors or
anything clients must see the same way.
Run `test:ui-responsive` for dashboard layout changes.

## CI

`.github/workflows/` (details and costs in [e2e/README.md](e2e/README.md#ci)):

| Workflow | When | What |
|---|---|---|
| `ci.yml` | push to `main` (every job), pull requests (only the jobs whose code changed: the `changes` job) | `pnpm check` tasks per package, generated code up to date, the Docker e2e suite |
| `cross-client.yml` | push to `main`, pull requests touching the API, CLI, SDKs or the suite | `pnpm --filter @staqly/e2e test:cross-client` (Go, uv, Node; no secrets) |
| `n8n.yml` | push to `main`, nightly, by hand | the dev stack built from the checkout + a real n8n: `test:n8n` (no secrets) |
| `ui.yml` | nightly, by hand | the dashboard in a real browser: `test:ui` (flows, axe) and `test:ui-responsive` against `staqly serve` built from the checkout (no secrets) |
| `docs.yml`, `release.yml`, `codeql.yml` | docs changes on `main`; `v*` tags; pushes, pull requests and weekly | docs site; binaries + image, then (stable tags, this repository only) a `repository_dispatch` `staqly-release` to the repository in the variable `DEPLOY_DISPATCH_REPO` with the secret `DEPLOY_DISPATCH_TOKEN` (fails when either is missing); code scanning |

The landing page (`apps/web`) is built and hosted outside this repository's workflows. The real-agent
suite (`e2e/agent/`, real Claude Code, Codex and OpenCode over MCP; costs money) has no workflow: run
it by hand (e2e/README.md). Every workflow step runs a command that works the same locally.

## Product rules that code must keep

* **Two modes.** `local` (default): one workspace, no API keys, no plans,
  billing, members or sign-in methods, no welcome wizard; every request needs
  the install's access token (`stk_local_...`, made on first start in
  `DATA_DIR/access_token` 0600, printed at every start with a
  `http://localhost:8080/#token=...` link; `server token [--rotate]`,
  `STAQLY_LOCAL_TOKEN` fixes it for tests/CI): API clients send it as
  `Authorization: Bearer`, browsers trade it for a session (or use a connect
  link). Only `cmd/mcp-stdio` stays keyless (it opens the database itself). `cloud`: people sign in with Google or an email
  code (accounts, workspace members with roles owner/admin/editor/viewer,
  invitations; the verified address is the person: Google and an email code for the same address sign into one account), scoped API keys, sessions
  + CSRF, plan limits (402); without Google or mail it refuses to start unless
  `AUTH_DEV_SIGNIN=true` (dev and tests only: email codes readable at
  `/v1/auth/email/dev-code`, plus the older setup-token / API-key / recovery-key
  sign-in). See https://docs.staqly.ai/local-vs-cloud.
* **Roles are scopes.** `app.RoleScopes` maps a role onto the scopes; a person's
  role is read on every request, API keys never exceed their creator's current
  role, and people (members, invitations) are managed by people, never by keys.
* **One authorization path.** REST and MCP call the same `internal/app`
  services, which check scopes themselves. Never add a shortcut around them.
* **Every query is scoped by `workspace_id`.**
* **Secrets are stored hashed** (API keys, sessions, recovery keys, device
  links) **or sealed** with AES-GCM: channel credentials and developer app
  secrets with `DATA_DIR/secret.key` / `STAQLY_SECRET_KEY`, alert secrets with
  `DATA_DIR/notify_secret.key` / `NOTIFY_SECRET_KEY`; backups carry both. Webhook
  secrets and schedule endpoint secrets are the only plaintext secrets
  (needed to sign).
* **Writing and publishing are separate scopes**: `posts:write` creates and
  edits, `posts:publish` schedules, publishes, cancels, retries a target or
  reports a custom target's result. Scopes: `collections:read|write|delete`,
  `posts:read|write|delete|publish`, `channels:read|manage`,
  `schedules:manage`, `media:read|write`, `webhooks:manage`, `alerts:manage`,
  `events:read`, `admin`. Dashboard sessions and local mode hold all.
* **Deleting needs its own scope**: `posts:delete` (posts, media),
  `collections:delete` (collections); `:write` never implies it. Purge stays
  `admin`; webhooks, channels, schedules and alerts delete with their
  `:manage` scope.
* **Publishing is a job.** Scheduling or publishing a post queues work in
  the job queue (River, in the same database and the same transaction as the change);
  providers are called from the worker, never inside a DB
  transaction. A target on your own automation (provider `custom`) emits
  `post.publish_requested` and waits for `POST /posts/{id}/targets/{channel}/result`
  until the collection's processing timeout.
* **Webhooks are at-least-once** via the transactional outbox; never make
  HTTP calls inside a DB transaction.
* **How users run it.** Locally: the binary (`staqly serve`, `staqly service
  install`) or the single image `ghcr.io/staqly/staqly` with plain `docker run`
  (container `staqly`, volume `staqly-data`; see
  `apps/docs/content/docs/self-hosting/docker.mdx`). Image users have no
  checkout: user-facing docs and dashboard hints never rely on `make`,
  `scripts/*.sh` or `compose.yml` for them. Compose (`make dev`) is for
  developing staqly. Cloud mode is staqly's hosted service (app.staqly.ai),
  deployed outside this repository. This repository ships the code and the
  image only, no cloud deployment tooling, and never names private
  repositories (cloud mode stays tested here: `compose.e2e.yml`, e2e and CI
  on PostgreSQL).
* Ports: API 3333 (127.0.0.1 only), dashboard 8080 (published on the
  network). Every browser signs in once, with the access token or a one-time
  connect link; the API stays on 127.0.0.1 as defense in depth, and local agents
  send the access token like any client.

## Keeping things in sync

* API change → update `apps/api/openapi.yaml` → `make lint-openapi` →
  `pnpm gen` in `apps/platform` and `pnpm gen` in the SDK.
* New env var → `internal/config`, `compose.yml` (only if Docker needs it),
  commented line in `.env.example`, docs.
* TS/JS style (oxfmt everywhere): single quotes in TS (double quotes in JSX attributes), no semicolons, imports
  grouped with a blank line between groups; always braces (`curly`) and
  blank lines around blocks, `return` and declarations
  (`@stylistic/padding-line-between-statements` via oxlint `jsPlugins`).
  `pnpm format` = `oxlint --fix` + `oxfmt`.
* User-facing name is **staqly**; code identifiers use `staqly`
  (`X-Staqly-*`, `STAQLY_*`, `staqly-api`).
* **Migrations are append-only once released.** Before the first release the
  history was squashed into one file, `db/migrations/00001_init.sql`, shared by
  SQLite and PostgreSQL (databases made before the squash, at version 47, get it
  recorded as applied by `store.baselineSquash`). From the first release on, never
  edit a released migration — add `0000N_<name>.sql` (goose) instead, or existing
  installs will diverge.
* Node: the current LTS, pinned once in `.nvmrc` (CI) and the `node:<major>`
  base image in the root `Dockerfile` (dashboard build stage); bump them
  together when a new LTS lands.
