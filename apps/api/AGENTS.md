# apps/api — Go backend

Go (module `github.com/staqly/staqly/apps/api`), chi, `modernc.org/sqlite`
(pure Go, CGO off), sqlc, goose, official MCP Go SDK
(`github.com/modelcontextprotocol/go-sdk`), `log/slog`.

## Layout

| Path | Role |
|---|---|
| `cmd/server` | REST + `/mcp` + background workers (webhooks, watchers, notifications, summaries / auto-tags), plus the dashboard with `DASHBOARD_ADDR`. Subcommands: `backup <file>`, `workspaces`, `recovery-key [ws]`, `set-plan <ws> <plan>`, `stats`, `connect-link [ws]` |
| `server` | `server.Main`: what `cmd/server`, the image and `staqly serve` (apps/cli) run |
| `internal/web` | Dashboard web server: embedded static export (`web/`, filled by `make dashboard`), security headers, `/api/*` proxy |
| `cmd/dashboard` | The dashboard alone in front of a remote API (`API_INTERNAL_URL`); the image and `staqly serve` embed it in `server` instead |
| `cmd/mcp-stdio` | MCP over STDIO against the same DB (keyless locally, `STAQLY_API_KEY` in cloud) |
| `internal/app` | **Domain services + authorization.** Every exported method takes a `Principal` and checks scopes |
| `internal/httpapi` | Router, auth/CSRF/local-mode guard, idempotency, handlers, rate limit. **All integration tests live in `api_test.go`** |
| `internal/mcpserver` | MCP tool definitions (thin wrappers over `internal/app`) |
| `internal/db` | sqlc output + `items_search.go` (hand-written FTS5/keyset query) |
| `internal/store` | Open SQLite (pragmas in DSN), goose migrations, `VACUUM INTO` backup |
| `internal/config` | Env vars |
| `db/migrations` | goose SQL migrations, embedded |
| `db/queries` | sqlc query files |
| `openapi.yaml` | OpenAPI 3.2 contract, embedded and served at `/openapi.yaml` |

## Commands

```bash
go test -race ./...                 # all tests (from apps/api)
go run ./cmd/server                 # local mode, data in ./data, :3333
STAQLY_MODE=cloud go run ./cmd/server
sqlc generate                       # after editing db/queries or migrations (~/go/bin/sqlc)
```

## Conventions

* **Schema change** = new `db/migrations/0000N_*.sql` (never edit an applied
  migration), then `sqlc generate`. Use `-- +goose StatementBegin/End` around
  triggers.
* **New endpoint**: service method in `internal/app` (takes `Principal`,
  calls `p.require(scope)` first, returns `*app.Error` for client errors) →
  handler in `internal/httpapi/handlers.go` → route in `server.go` → OpenAPI →
  test in `api_test.go`. If agents need it, add an MCP tool in
  `internal/mcpserver` calling the same method.
* **Writes go through `s.tx`** (BEGIN IMMEDIATE). Domain changes that
  matter to integrations call `s.emit` inside the same transaction (event +
  webhook outbox rows). `tx` wakes the worker after commit.
* **Item mutations** use `mutateItem` (version snapshot + optimistic version
  check + event).
* Timestamps: fixed-width UTC text (`timeFormat`), compare as strings; parse
  user input with `ParseTimeFilter`.
* Errors: `invalid(field,msg)` 422, `conflict` 409, `notFound` 404,
  `limitErr` 402 with `details`. Unknown errors become 500 and are logged.
* Plan limits live in `internal/app/plans.go` (single source of truth); check
  them inside the write transaction.
* Never log headers, query strings or secrets.
* Keep `ponytail:` comments on deliberate shortcuts with their ceiling.

## Tests

Integration-style tests through the real router (`httptest`), cloud mode via
`newEnv`, local mode via `newLocal`. Cover authz (wrong scope / other
workspace), the happy path and concurrency where relevant. `-race` must pass.
