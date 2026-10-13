# Contributing to Staqly

Thanks for helping! Issues and pull requests are welcome. Please follow our
[Code of Conduct](CODE_OF_CONDUCT.md).

* **Bugs / ideas:** open an issue using the templates (search for duplicates first).
* **Security issues:** never in public issues — see [SECURITY.md](SECURITY.md).
* **Pull requests:** keep them focused, describe what and why, and follow the
  checklist in the PR template. CI must be green.

## Development setup

```bash
make test        # Go API + CLI (race detector), both SDKs, dashboard
make test-e2e    # full Docker end-to-end run in an isolated compose project
make lint-openapi
make sqlc        # regenerate typed queries after editing apps/api/db/queries
```

CI (`.github/workflows/ci.yml`) runs formatting, lint, types, tests and builds
for every part, checks generated code is up to date, builds the Docker images
and runs the end-to-end suite. `openapi.yaml` is the hand-written contract; a
Go contract test fails if a route is undocumented or a real response doesn't
match its schema.

Run without Docker: `cd apps/api && go run ./cmd/server` (data in `./data`;
add `WEBHOOK_ALLOW_LOOPBACK=true` for receivers on localhost) and
`cd apps/web && npm install && npm run dev`.

```
apps/api            Go backend: chi REST, MCP (stdio + HTTP), webhook worker
  cmd/server          REST + /mcp + workers; operator commands (backup, workspaces, recovery-key, set-plan, connect-link)
  cmd/mcp-stdio       MCP over STDIO (keyless locally; STAQLY_API_KEY in cloud mode)
  internal/app        domain services + authorization (shared by REST and MCP)
  internal/httpapi    HTTP handlers, auth/CSRF/idempotency middleware, tests
  internal/mcpserver  MCP tool definitions
  internal/store      SQLite open/migrate/backup
  internal/db         sqlc-generated queries
  db/migrations       goose migrations (embedded; one init migration so far)
  openapi.yaml        OpenAPI 3.2 contract
apps/web            Next.js dashboard
apps/cli            `staqly` CLI (Go)
apps/docs           documentation site (Fumadocs)
packages/sdk-typescript  TypeScript SDK (types generated from OpenAPI)
packages/sdk-python      Python SDK
integrations/       n8n example + workflow, Claude Code, Codex, OpenCode configs
scripts/            backup, restore, seed, e2e
docs/               architecture, cloud, MCP, n8n, notifications, export, observability, CLI, backup, troubleshooting
```

More: [architecture](architecture.md) · [progress & limitations](PROGRESS.md) ·
[security policy](SECURITY.md).

## Conventions

* Read [AGENTS.md](AGENTS.md) and the `AGENTS.md` of the project you touch —
  they list commands, conventions and the product rules the code must keep.
* API changes start in `apps/api/openapi.yaml` (the contract test fails if the
  code and the spec disagree); regenerate client types afterwards.
* Schema changes are new migrations in `apps/api/db/migrations` + `sqlc generate`.
* TypeScript/JavaScript: oxfmt formatting (single quotes, no semicolons, grouped
  imports) and oxlint — run `npm run format` and `npm run lint`. Go: `gofmt`.
  Python: `ruff format` + `ruff check` + `mypy --strict`.
* By contributing you agree that your contributions are licensed under the
  Apache License 2.0.
