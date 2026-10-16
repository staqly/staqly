# sdks/typescript — @staqly/sdk

TypeScript SDK over the REST API. Runtime-dependency-free (global `fetch` + Web
Crypto only) so it runs on Node ≥20.19, Bun, Deno, browsers and edge runtimes.
ESM-only, built by plain `tsc` (TypeScript 7) into `dist/` — no bundler.

| Path | Role |
|---|---|
| `src/schema.ts` | **Generated** from `apps/api/openapi.yaml` (`pnpm gen`). Never edit by hand. `gen` runs openapi-typescript through `npx` with TypeScript 5.9, because TS 7 has no JS compiler API (needs network on first run). |
| `src/types.ts` | Public model types derived from `schema.ts` + SDK param types |
| `src/core.ts` | `HTTPClient`: auth, timeouts, retries/backoff, Idempotency-Key, error mapping |
| `src/errors.ts` | Error hierarchy (`StaqlyError` → `APIError` → status classes) |
| `src/resources/*.ts` | One class per namespace (`items`, `collections`, `webhooks`, ...) |
| `src/client.ts` | `Staqly` client, `requestApproval`, `requiresApproval` |
| `src/webhooks.ts` | `verifyWebhook` (also the `@staqly/sdk/webhooks` entry) |
| `src/node.ts` | Node-only helpers (`fileFromPath`, `saveResponse`) — `@staqly/sdk/node` entry |
| `test/*.test.ts` | vitest unit tests with a fake fetch (`test/helpers.ts`) |
| `test/live.test.ts` | Live suite, skipped unless `STAQLY_LIVE=1` |

```bash
pnpm gen               # after an OpenAPI change
pnpm check             # typecheck (tsc 7) + lint (oxlint, publint) + unit tests + build
pnpm test              # unit tests only
STAQLY_LIVE=1 STAQLY_API_URL=http://127.0.0.1:3333 pnpm test     # + live, local mode
STAQLY_LIVE=1 STAQLY_API_URL=http://127.0.0.1:3334 pnpm test     # + live, cloud mode (creates a workspace)
```

Rules:

* No runtime dependencies and no `node:` imports outside `src/node.ts`.
* Tooling: oxlint (`.oxlintrc.json`); no formatter yet (oxfmt is pre-1.0). vitest
  stays on 4.x while `engines` says Node 20.19 (vitest 5 needs Node ≥22.12).
* Request bodies / query params keep the API's snake_case names and schema types;
  SDK options are camelCase. Every method accepts `RequestOptions` last.
* Keep the surface aligned with `sdks/python` (same namespaces and method
  names, camelCase vs snake_case). New endpoint → method in both SDKs, a row in
  `test/resources.test.ts`, a live assertion if it matters, README section.
* Bump `version` in `package.json` **and** `src/version.ts` together (a test checks it);
  add a CHANGELOG entry.
