## What & why

<!-- What does this change and why? Link the issue: "Closes #123". -->

## How it was tested

<!-- Commands you ran and what you checked, e.g. `make test`, `make test-e2e`, screenshots for UI changes. -->

## Checklist

- [ ] Tests added/updated for the change (Go: `internal/httpapi/*_test.go`; dashboard: `apps/dashboard/tests`; SDKs; CLI)
- [ ] API changes are reflected in `apps/api/openapi.yaml` (the contract test fails otherwise) and generated types are refreshed (`pnpm gen`, SDK `pnpm gen`)
- [ ] Schema changes are a new migration in `apps/api/db/migrations` + `sqlc generate`
- [ ] Docs updated (`README.md`, `docs/`, `apps/docs`) for user-visible changes
- [ ] UI changes checked on phone, tablet and desktop, light and dark
- [ ] No secrets, keys or personal data in code, logs, fixtures or screenshots
