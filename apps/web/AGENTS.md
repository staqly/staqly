<!-- BEGIN:nextjs-agent-rules -->

## This is NOT the Next.js you know

This version has breaking changes — APIs, conventions, and file structure may all differ from your training data. Read the relevant guide in `node_modules/next/dist/docs/` (resolved from this file's directory; in monorepos the `next` package may not be visible from the repo root) before writing any code. Heed deprecation notices.

This block is written and re-added by `next dev` — verify at `node_modules/next/dist/server/lib/generate-agent-files.js`. Removing it from a diff only re-creates the uncommitted change; committing it with your work keeps the tree clean.

<!-- END:nextjs-agent-rules -->

# apps/web — staq.ly dashboard

Next.js 16 (App Router, React Compiler, `output: "standalone"`), React 19,
TypeScript 7 (native `tsc`), Tailwind v4, shadcn/ui, TanStack Query (all server
state), React Hook Form + Zod, lucide-react, next-themes, Vitest + Testing
Library, oxlint + oxfmt. Node 24 LTS (Docker `node:24-alpine`, `@types/node` 24).

## Commands

```bash
npm run dev            # needs the API: API_INTERNAL_URL=http://localhost:3333 (default)
npm test               # vitest
npm run lint && npm run typecheck && npm run build
npm run format         # oxfmt --write; `npm run format:check` in CI
npm run gen:api        # regenerate lib/api-types.ts from ../api/openapi.yaml
```

* `gen:api` runs openapi-typescript in an isolated `npx` environment pinned to
  `typescript@5.9.3`: the generator builds its AST with the classic
  `ts.factory` API, which the TypeScript 7 package no longer exports. The
  project itself (tsc, `next build`) uses TypeScript 7. Never hand-edit
  `lib/api-types.ts`; fields the spec lacks are added in `lib/api.ts`.

## Lint & format

* **oxlint** (`.oxlintrc.json`): plugins typescript, unicorn, oxc, react
  (incl. hooks + React Compiler rules), jsx-a11y, nextjs, import, vitest
  (tests); categories correctness + suspicious = error, perf = warn.
  `react-perf` is not enabled — the React Compiler memoizes, so "new
  function/object as prop" findings are noise. `jsx-a11y/prefer-tag-over-role`
  is off because our ARIA widgets (radiogroups of buttons, listboxes, status
  regions, shadcn `role="group"`) are deliberate.
* Suppress only one line with a reason:
  `// oxlint-disable-next-line rule/name -- why`. Check with
  `npx oxlint --report-unused-disable-directives`.
* **oxfmt** (`.oxfmtrc.json`): printWidth 120, double quotes, semicolons,
  trailing commas; `components/ui/**` keeps the shadcn style (no semicolons,
  width 80). `lib/api-types.ts` and this file are not formatted.

## How it talks to the API

* The browser only calls same-origin `/api/*`; `next.config.ts` rewrites it to
  `API_INTERNAL_URL` (resolved at **build** time; Docker passes
  `http://api:3333`). `proxyClientMaxBodySize` must stay ≥ the upload limit.
* `lib/api.ts` is the typed fetch client: sends `X-CSRF-Token` on unsafe
  requests when the session has one, maps `{error:{code,message,fields,details}}`
  to `ApiError`. Handle `401` (→ /login or /setup), `402 plan_limit_exceeded`,
  `403 not_local` (device not connected), `409`, `429`.
* Mode comes from `GET /v1/session` / `GET /v1/setup`: **local** hides
  setup/login/API keys/recovery; **cloud** shows them. Never store secrets in
  localStorage; newly created keys live in memory only (`lib/session-key.ts`).

## UI rules

* Product name is **staq.ly** in all user-facing text.
* Must work and look good on phones (375–430 px), tablets and desktop, light
  and dark; no horizontal page overflow; touch targets ≥ 44 px.
* Use shadcn/ui primitives from `components/ui`; feature components live in
  `components/<feature>`; pages in `app/(app)/...`. Settings sections:
  General, Plan & usage, Notifications (`components/notifications`), Data &
  privacy, System.
* Notification channel secrets come back masked (`••••abcd`): show "•••• set"
  with Replace/Remove, never prefill them, and leave unchanged secrets out of
  PATCH bodies (`lib/channels.ts`).
* Downloads (event export, workspace ZIP) are plain same-origin links under
  `/api/...`; the session cookie authenticates them.
* Snippets shown to users use the real defaults: API `http://localhost:3333`,
  MCP `http://localhost:3333/mcp`, dashboard `http://localhost:8080`.

## Tests

Mock fetch (see `tests/`); cover both modes where behaviour differs. Keep
`npm test`, lint, typecheck and build green.
