# Contributing to staqly

Thanks for helping! Issues and pull requests are welcome. Please follow our
[Code of Conduct](CODE_OF_CONDUCT.md).

* **Bugs / ideas:** open an issue using the templates (search for duplicates first).
* **Security issues:** never in public issues — see [SECURITY.md](SECURITY.md).
* **Pull requests:** keep them focused, describe what and why, and follow the
  checklist in the PR template. CI must be green.

## Development setup

The repo is a Turborepo with pnpm workspaces (`pnpm-workspace.yaml`,
`turbo.json`). Packages share the same script names (`format`, `format:check`,
`lint`, `typecheck`, `test`, `build`, `gen`) where they apply; the Go API, the CLI and the
Python SDK wrap their toolchains (go, uv) in a small `package.json`, so one
command covers everything and turbo caches unchanged packages. Requires Node
(see `.nvmrc`), pnpm (`corepack enable`), Go and uv.

```bash
pnpm install
pnpm check                                   # every package: format:check, lint, typecheck, test
pnpm turbo run test --filter=@staqly/platform
pnpm gen                                     # after editing queries or openapi.yaml
pnpm test:e2e                                # Docker end-to-end suite, see e2e/README.md
```

CI (`.github/workflows/ci.yml`) runs the same turbo tasks per package, checks
generated code is up to date, builds the Docker image and runs the e2e suite
against it. `openapi.yaml` is the hand-written contract; a
Go contract test fails if a route is undocumented or a real response doesn't
match its schema.

### Dev stack (`compose.dev.yml`)

`make dev` runs `compose.yml` plus `compose.dev.yml`: the image built from this
checkout, debug text logs, private destinations allowed (so webhooks,
alerts, schedule endpoints and your own automation can reach containers), and
**mailpit**, which catches every email alert at http://localhost:8025 (alert
config: host `mailpit`, port `1025`, `tls` `none`). `make dev-down` stops it.

`make site` runs the landing page (http://localhost:8070) and the docs
(http://localhost:8090) from this checkout, linked to each other and to the
dev dashboard.

To work on billing, run the Stripe CLI in the stack. In `.env`:

```bash
STAQLY_MODE=cloud
COMPOSE_PROFILES=stripe
STRIPE_SECRET_KEY=sk_test_51ExampleKey          # Stripe dashboard, test mode
STRIPE_PRICE_PRO=price_1Pro30UsdMonthly         # a recurring test price
STRIPE_WEBHOOK_SECRET=whsec_printed_by_make_stripe_secret
```

`make stripe-secret` prints the signing secret the Stripe CLI uses (it stays
the same for your account); `make dev` then forwards test-mode events to
`/v1/billing/webhook`. Pay with the test card `4242 4242 4242 4242`.

Run without Docker: `cd apps/api && go run ./cmd/server` (data in `./data`;
add `WEBHOOK_ALLOW_LOOPBACK=true` for receivers on localhost) and
`cd apps/platform && pnpm dev` (http://localhost:3000, hot reload, `/api/*`
rewritten to the API). To try `staqly serve` from the checkout, run
`make dashboard` first (it embeds the dashboard's static export), then
`cd apps/cli && go run ./cmd/staqly serve`.

```
apps/api            Go backend: chi REST, MCP (stdio + HTTP), webhook worker
  cmd/server          REST + /mcp + workers; operator commands (backup, workspaces, recovery-key, set-plan, stats, connect-link)
  cmd/mcp-stdio       MCP over STDIO (keyless locally; STAQLY_API_KEY in cloud mode)
  cmd/dashboard       serves the dashboard's static export + /api proxy (standalone; the staqly image embeds the dashboard in server)
  internal/app        domain services + authorization (shared by REST and MCP)
  internal/httpapi    HTTP handlers, auth/CSRF/idempotency middleware, tests
  internal/mcpserver  MCP tool definitions
  internal/store      SQLite open/migrate/backup
  internal/db         sqlc-generated queries
  db/migrations       goose migrations (embedded; append-only)
  openapi.yaml        OpenAPI 3.2 contract
apps/platform      Next.js dashboard (static export)
apps/cli            `staqly` CLI and `staqly serve` (Go)
apps/docs           documentation site (Fumadocs), docs.staqly.ai
apps/web            landing page, staqly.ai
sdks/typescript     TypeScript SDK (types generated from OpenAPI)
sdks/python         Python SDK
integrations/       n8n example + workflow, Claude Code, Codex, OpenCode configs
scripts/            install, backup, restore, seed
e2e/                Docker end-to-end suite; fixtures/ for seed and e2e
docs/               architecture, cloud, MCP, n8n, notifications, export, observability, CLI, backup, troubleshooting
```

More: [architecture](docs/architecture.md) · [progress & limitations](docs/PROGRESS.md) ·
[security policy](SECURITY.md).

## Conventions

* Read [AGENTS.md](AGENTS.md) and the `AGENTS.md` of the project you touch —
  they list commands, conventions and the product rules the code must keep.
* API changes start in `apps/api/openapi.yaml` (the contract test fails if the
  code and the spec disagree); regenerate client types afterwards.
* Schema changes are new migrations in `apps/api/db/migrations` + `sqlc generate`.
* TypeScript/JavaScript: oxfmt formatting (single quotes, no semicolons, grouped
  imports) and oxlint — run `pnpm format` and `pnpm lint`. Go: `gofmt`.
  Python: `ruff format` + `ruff check` + `mypy --strict`.
* By contributing you agree to the Contributor License Agreement below.

## Contributor License Agreement

Staqly is licensed under the GNU AGPL-3.0 (the SDKs and `integrations/` under
Apache-2.0). Before we can merge your first pull request, you accept a
Contributor License Agreement, based on the FSFE's Fiduciary License Agreement:

* contributing as an individual: the [Individual agreement (ICLA)](ICLA.md);
* contributing for a company or other legal entity: the [Entity agreement (CCLA)](CCLA.md),
  accepted by someone allowed to sign for it, with the people who contribute for
  it listed in a GitHub issue titled `CCLA: <company name>`.

The CLA check on your pull request asks you to post one comment:

```
I have read the CLA Document and I hereby sign the CLA
```

You do it once; it covers your later contributions too. The agreement keeps the
project's rights in one place, and in return promises that staqly and your
contribution stay available under a free and open source license. Your name stays
in the history, and you keep the right to use your own contribution as you like.
