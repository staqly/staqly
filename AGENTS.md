# Staqly — agent guide

Local-first workspace where AI agents store generated content (items in
collections) and humans review it. Monorepo:

| Path | What | Guide |
|---|---|---|
| `apps/api` | Go backend: REST, MCP (stdio + Streamable HTTP), webhook worker, SQLite | [apps/api/AGENTS.md](apps/api/AGENTS.md) |
| `apps/dashboard` | Next.js dashboard (static export embedded in the server and image `ghcr.io/staqly/staqly`) | [apps/dashboard/AGENTS.md](apps/dashboard/AGENTS.md) |
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
| `docs/` | architecture, cloud & plans, MCP, n8n, backup, troubleshooting, progress | — |

License: Apache-2.0 (`LICENSE`, `NOTICE`). Keep `NOTICE` updated if you add
code that requires attribution.

## Commands

```bash
pnpm install       # once: the whole workspace (Turborepo + pnpm; Go and Python packages have package.json wrappers)
pnpm check         # format:check, lint, typecheck, test of every package (turbo, cached)
pnpm turbo run test --filter=@staqly/api   # one package (api, cli, dashboard, docs, web, sdk, sdk-python, e2e)
pnpm gen           # sqlc queries, dashboard API types, TypeScript SDK schema
pnpm test:e2e      # Docker end-to-end suite (e2e/, compose.e2e.yml)
make up            # compose.yml alone with the released image, open a signed-in browser (users run `docker run`, not this)
make dev           # build from this checkout with the dev stack (compose.dev.yml: mailpit, Stripe CLI)
make site          # landing page :8070 + docs :8090 from this checkout
make cloud         # production cloud mode: the image behind Caddy (compose.cloud.yml, STAQLY_DOMAIN)
make connect       # one-time sign-in link for another browser
make release VERSION=v0.2.0  # tag origin/main; CI publishes the binaries and ghcr.io/staqly/staqly:v0.2.0
```

Run `pnpm check` before finishing a change; run `pnpm test:e2e` for anything
touching auth, Docker, the dashboard proxy, webhooks or MCP.

## Product rules that code must keep

* **Two modes.** `local` (default): one workspace, no keys, no plan limits,
  credential-less requests only from this machine (peer, `Host`, `Origin`,
  `X-Forwarded-Host` checks). `cloud`: many workspaces, scoped API keys,
  sessions + CSRF, recovery keys, plan limits (402). See `docs/cloud.md`.
* **One authorization path.** REST and MCP call the same `internal/app`
  services, which check scopes themselves. Never add a shortcut around them.
* **Every query is scoped by `workspace_id`.**
* **Secrets are stored hashed** (API keys, sessions, recovery keys, device
  links). Webhook secrets are the only plaintext secrets (needed to sign).
* **Approve/reject need `items:review`**; creating content needs
  `items:write`. Custom actions may not set `approved`/`rejected`.
* **Webhooks are at-least-once** via the transactional outbox; never make
  HTTP calls inside a DB transaction.
* **How users run it.** Locally: the binary (`staqly serve`, `staqly service
  install`) or the single image `ghcr.io/staqly/staqly` with plain `docker run`
  (container `staqly`, volume `staqly-data`; see
  `apps/docs/content/docs/self-hosting/docker.mdx`). Image users have no
  checkout: user-facing docs and dashboard hints never rely on `make`,
  `scripts/*.sh` or `compose.yml` for them. Compose (`make dev`) is for
  developing staqly; cloud servers use `make cloud`.
* Ports: API 3333 (127.0.0.1 only), dashboard 8080 (published on the
  network). The dashboard is reachable from other devices, so every browser signs in once
  with a one-time link (`DASHBOARD_EXPOSED=true`); the API stays on 127.0.0.1
  and is keyless for local agents.

## Keeping things in sync

* API change → update `apps/api/openapi.yaml` → `make lint-openapi` →
  `pnpm gen` in `apps/dashboard` and `pnpm gen` in the SDK.
* New env var → `internal/config`, `compose.yml` (only if Docker needs it),
  commented line in `.env.example`, docs.
* TS/JS style (oxfmt everywhere): single quotes in TS (double quotes in JSX attributes), no semicolons, imports
  grouped with a blank line between groups; always braces (`curly`) and
  blank lines around blocks, `return` and declarations
  (`@stylistic/padding-line-between-statements` via oxlint `jsPlugins`).
  `pnpm format` = `oxlint --fix` + `oxfmt`.
* User-facing name is **staqly**; code identifiers use `staqly`
  (`X-Staqly-*`, `STAQLY_*`, `staqly-api`).
* **Migrations are append-only once released.** `db/migrations/00001_init.sql`
  shipped in the first release; never edit a released migration — add
  `0000N_<name>.sql` (goose) instead, or existing installs will diverge.
* Node: the current LTS, pinned once in `.nvmrc` (CI) and the `node:<major>`
  base image in the root `Dockerfile` (dashboard build stage); bump them
  together when a new LTS lands.
* Update `docs/PROGRESS.md` when a feature lands or a limitation changes.
