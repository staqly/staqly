# apps/api — Go backend

Go (module `github.com/staqly/staqly/apps/api`), chi, `modernc.org/sqlite`
(pure Go, CGO off), sqlc, goose, official MCP Go SDK
(`github.com/modelcontextprotocol/go-sdk`), `log/slog`.

## Layout

| Path | Role |
|---|---|
| `cmd/server` | REST + `/mcp` + background workers (webhooks, the jobs queue: publish runs, target timeouts, schedule runs, token refreshes; alerts), plus the dashboard with `DASHBOARD_ADDR`. Subcommands: `backup <file>`, `workspaces`, `recovery-key [ws]`, `set-plan <ws> <plan>`, `stats`, `connect-link [ws]` |
| `server` | `server.Main`: what `cmd/server`, the image and `staqly serve` (apps/cli) run |
| `internal/web` | Dashboard web server: embedded static export (`web/`, filled by `make dashboard`; text files gzipped once in memory, every file with an ETag), security headers, `/api/*` proxy. `/v1` JSON is gzipped by `middleware.Compress` in `internal/httpapi` |
| `cmd/dashboard` | The dashboard alone in front of a remote API (`API_INTERNAL_URL`); the image and `staqly serve` embed it in `server` instead |
| `cmd/mcp-stdio` | MCP over STDIO against the same DB (keyless locally, `STAQLY_API_KEY` in cloud) |
| `internal/app` | **Domain services + authorization.** Every exported method takes a `Principal` and checks scopes. `posts.go` (posts, targets, publishing), `channels.go` (connected accounts, sealed credentials, OAuth), `schedules.go`, `jobs.go` (the SQLite job queue), `secrets.go` (AES-GCM sealing with `DATA_DIR/secret.key` or `STAQLY_SECRET_KEY`), `notify_*.go` (alerts) |
| `internal/providers` | The platform catalog: the `Provider` interface (auth kind oauth2/token/none, fields, limits, publish), `Base`, `custom` (your own automation: publishes asynchronously through `POST /posts/{id}/targets/{channel}/result`), every other platform registered as `soon` until its wave lands |
| `internal/httpapi` | Router, auth/CSRF/local-mode guard, idempotency, handlers, rate limit. Integration tests in `*_test.go`, harness (`newEnv`, `newLocal`) in `api_test.go` |
| `internal/mcpserver` | MCP tool definitions (thin wrappers over `internal/app`). One server per process for HTTP: handlers take the caller from `t.principal(ctx)`, never from server state. Tool list pinned in `testdata/tools.golden.json` (`go test ./internal/mcpserver -update` after an intended change) |
| `internal/db` | sqlc output + `posts_search.go` (hand-written filter/keyset query for posts) |
| `internal/store` | Open the database: SQLite in local mode (pragmas in DSN, `VACUUM INTO` backup), PostgreSQL in cloud mode (`postgres.go`: a database/sql connector that rewrites each SQLite statement once — placeholders and a few SQLite-only constructs, pinned by `TestPostgresRewrite`), goose migrations for both, `import.go` (`server import-sqlite`). `storetest` opens SQLite or, with `STAQLY_TEST_DATABASE_URL`, a fresh PostgreSQL schema per test |
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
  migration) **and** `db/migrations-postgres/0000N_*.sql` with the same number
  (PostgreSQL types: TEXT, BIGINT, BYTEA; `lower(col)` for NOCASE; columns in the
  same order, SELECT * is read by position), then `sqlc generate` (SQLite engine).
  `TestSchemasMatch` (runs with `STAQLY_TEST_DATABASE_URL`, in CI's
  `api-postgres` job) fails on any drift. Use `-- +goose StatementBegin/End`
  around triggers and functions.
* **Queries** are written for SQLite (sqlc); the PostgreSQL connector rewrites
  them. Prefer SQL both accept (qualify a column the upsert also has in
  `excluded`, `table.col + 1`); a new SQLite-only construct needs a rule in
  `store.dialect` and a case in `TestPostgresRewrite`. On PostgreSQL any failed
  statement aborts the transaction: don't swallow an error inside `s.tx` and
  carry on.
* **New endpoint**: service method in `internal/app` (takes `Principal`,
  calls `p.require(scope)` first, returns `*app.Error` for client errors) →
  handler in `internal/httpapi/handlers.go` → route in `server.go` (or a
  `routes_*.go`) → OpenAPI → test in `internal/httpapi/*_test.go`. If agents
  need it, add an MCP tool in `internal/mcpserver` calling the same method.
* **Writes go through `s.tx`**: SQLite BEGIN IMMEDIATE behind the process's
  write gate; PostgreSQL SERIALIZABLE with no gate, rerun on serialization
  failures (so `fn` may run more than once: database work only). Domain changes that
  matter to integrations call `s.emit` inside the same transaction (event +
  webhook outbox rows). `tx` wakes the worker after commit.
* **Post mutations** happen inside `s.tx` with an optimistic version check
  (`UpdatePost ... AND version = ?`, `expected_version` from clients) and an
  event; a published or publishing post is not editable (`editable`).
* **Background work is a job** (`enqueue` in `jobs.go`): one live job per key
  (`publish_post:<id>`, `publish_target:<post>:<channel>`, `target_timeout:...`,
  `run_schedule:<id>`, `refresh_channel:<id>`); queueing the same key again
  moves the run time. Workers run up to `JOB_CONCURRENCY` jobs at once (default
  4, at most half per workspace while others wait). A claim takes a 60 s lease
  with a fresh `lease_token`, renewed every 20 s while the run goes on; renewing
  and `FinishJob` need the token, so a worker that lost its lease stops and
  writes nothing. Each kind has a run budget (`jobRunTimeout`: publishing 10 min)
  and the outcome is written with its own context. Queueing a running key only
  sets `requeued` (no second run at once; it goes back to the queue when the run
  ends). Retried with backoff up to `max_attempts`; `CancelJob` cancels queued
  ones and drops a running job's requeue. Never call a platform inside `s.tx`;
  providers run from the job. Delivery is at least once: a crash after the
  platform accepted a post but before the result was written publishes again on
  the retry (the target is checked first, but nothing deduplicates on the
  platform's side).
* **Credentials are sealed** (`s.seal` / `s.unseal`), never logged or
  returned; `Channel` JSON has no credentials field.
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
