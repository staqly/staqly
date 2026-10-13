# staq.ly — agent guide

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

`reference/` holds a clone of Impri for comparison only. It is git-ignored —
never commit it or copy code from it.

## Commands

```bash
make up            # API 127.0.0.1:3333 + dashboard :8080 (Docker), opens a signed-in browser
make connect       # one-time sign-in link for another browser
make test          # Go (race) + SDK + dashboard tests
make test-e2e      # full Docker end-to-end run in an isolated compose project
make lint-openapi  # validate apps/api/openapi.yaml
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
* Ports: API 3333, dashboard 8080; both bind to 127.0.0.1 in `compose.yml`.
  The dashboard is published on the network, so every browser signs in once
  with a one-time link (`DASHBOARD_EXPOSED=true`); the API stays on 127.0.0.1
  and is keyless for local agents.

## Keeping things in sync

* API change → update `apps/api/openapi.yaml` → `make lint-openapi` →
  `npm run gen:api` in `apps/web` and `npm run gen` in the SDK.
* New env var → `internal/config`, `compose.yml` (only if Docker needs it),
  commented line in `.env.example`, docs.
* User-facing name is **staq.ly**; code identifiers use `staqly`
  (`X-Staqly-*`, `STAQLY_*`, `staqly-api`).
* Update `docs/PROGRESS.md` when a feature lands or a limitation changes.
