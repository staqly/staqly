# Staqly — agent guide

Local-first workspace where AI agents store generated content (items in
collections) and humans review it. Monorepo:

| Path | What | Guide |
|---|---|---|
| `apps/api` | Go backend: REST, MCP (stdio + Streamable HTTP), webhook worker, SQLite | [apps/api/AGENTS.md](apps/api/AGENTS.md) |
| `apps/web` | Next.js dashboard | [apps/web/AGENTS.md](apps/web/AGENTS.md) |
| `packages/sdk-typescript` | TypeScript SDK (types from OpenAPI) | [packages/sdk-typescript/AGENTS.md](packages/sdk-typescript/AGENTS.md) |
| `integrations/` | n8n example + demo workflow, Claude Code / Codex configs | [integrations/AGENTS.md](integrations/AGENTS.md) |
| `scripts/` | backup, restore, seed, e2e | — |
| `docs/` | architecture, cloud & plans, MCP, n8n, backup, troubleshooting, progress | — |

License: Apache-2.0 (`LICENSE`, `NOTICE`). Keep `NOTICE` updated if you add
code that requires attribution.

## Commands

```bash
make up            # pull released images, start API 127.0.0.1:3333 + dashboard :8080, open a signed-in browser
make dev           # same, but build the images from this checkout
make connect       # one-time sign-in link for another browser
make test          # Go (race) + SDK + dashboard tests
make test-e2e      # full Docker end-to-end run in an isolated compose project
make lint-openapi  # validate apps/api/openapi.yaml
make deploy        # tag origin/main as prod-<timestamp>; CI publishes ghcr.io/staqly/staqly-{api,web}
```

Run `make test` before finishing a change; run `make test-e2e` for anything
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
* Ports: API 3333 (127.0.0.1 only), dashboard 8080 (published on the
  network). The dashboard is reachable from other devices, so every browser signs in once
  with a one-time link (`DASHBOARD_EXPOSED=true`); the API stays on 127.0.0.1
  and is keyless for local agents.

## Keeping things in sync

* API change → update `apps/api/openapi.yaml` → `make lint-openapi` →
  `npm run gen:api` in `apps/web` and `npm run gen` in the SDK.
* New env var → `internal/config`, `compose.yml` (only if Docker needs it),
  commented line in `.env.example`, docs.
* TS/JS style (oxfmt everywhere): single quotes in TS (double quotes in JSX attributes), no semicolons, imports
  grouped with a blank line between groups; always braces (`curly`) and
  blank lines around blocks, `return` and declarations
  (`@stylistic/padding-line-between-statements` via oxlint `jsPlugins`).
  `npm run format` = `oxlint --fix` + `oxfmt`.
* User-facing name is **staqly**; code identifiers use `staqly`
  (`X-Staqly-*`, `STAQLY_*`, `staqly-api`).
* **Migrations are append-only once released.** `db/migrations/00001_init.sql`
  shipped in the first release; never edit a released migration — add
  `0000N_<name>.sql` (goose) instead, or existing installs will diverge.
* Node: the current LTS, pinned once in `.nvmrc` (CI) and the `node:<major>`
  base image in `apps/web/Dockerfile` (build stage); bump them
  together when a new LTS lands.
* Update `docs/PROGRESS.md` when a feature lands or a limitation changes.
