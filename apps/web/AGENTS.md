<!-- BEGIN:nextjs-agent-rules -->

## This is NOT the Next.js you know

This version has breaking changes — APIs, conventions, and file structure may all differ from your training data. Read the relevant guide in `node_modules/next/dist/docs/` (resolved from this file's directory; in monorepos the `next` package may not be visible from the repo root) before writing any code. Heed deprecation notices.

This block is written and re-added by `next dev` — verify at `node_modules/next/dist/server/lib/generate-agent-files.js`. Removing it from a diff only re-creates the uncommitted change; committing it with your work keeps the tree clean.

<!-- END:nextjs-agent-rules -->

# apps/web — staqly dashboard

Next.js 16 (App Router, React Compiler, `output: "export"`), React 19,
TypeScript 7 (native `tsc`), Tailwind v4, shadcn/ui, TanStack Query (all server
state), React Hook Form + Zod, lucide-react, next-themes, Vitest + Testing
Library, oxlint + oxfmt. Node 24 LTS (build only, Docker `node:24-alpine`, `@types/node` 24).

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
* **oxfmt** (`.oxfmtrc.json`): printWidth 120, single quotes, double quotes
  in JSX attributes, no semicolons, trailing commas, grouped/sorted imports
  (builtin, external, internal `@/`, relative, styles). One style for the
  whole app, `components/ui/**` included. `lib/api-types.ts` and this file are
  not formatted.

## How it talks to the API

* The dashboard is a **static export** (`output: "export"` → `out/`). No
  Next.js server runs in production: the Go web server in `apps/api`
  (`internal/web`, binary `cmd/web`, image `staqly-web`) embeds `out/`, sends
  the security headers and streams same-origin `/api/*` to `API_INTERNAL_URL`
  (default `http://api:3333`). `make web` builds the export into `apps/api/web/`.
  * So: no route handlers, server actions, `cookies()`/`headers()`, middleware
    or `redirect()` that depends on the request. Everything is client-side.
  * `[id]` pages are exported once as `_` (`generateStaticParams`) and read the
    real id in the browser with `useRouteId()`; the Go server maps
    `/items/*` and `/collections/*` onto that shell. A new `[id]` route needs
    an entry in `idRoutes` in `apps/api/internal/web/web.go`.
  * `npm run dev` rewrites `/api/*` to the API (`API_INTERNAL_URL`, default
    `http://localhost:3333`); dev only.
  * The proxy contract (client `X-Forwarded-*`, `Forwarded`, `X-Real-IP`
    dropped; `X-Forwarded-Host` = the browser's Host, never empty; no
    `X-Forwarded-For`; bodies streamed, 110 MB cap → 413) lives in and is
    tested by `internal/web`.
* `lib/api.ts` is the typed fetch client: sends `X-CSRF-Token` on unsafe
  requests when the session has one, maps `{error:{code,message,fields,details}}`
  to `ApiError`. Handle `401` (→ /login or /setup), `402 plan_limit_exceeded`,
  `403 not_local` (device not connected), `409`, `429`.
* Mode comes from `GET /v1/session` / `GET /v1/setup`: **local** hides
  setup/login/API keys/recovery; **cloud** shows them. Never store secrets in
  localStorage; newly created keys live in memory only (`lib/session-key.ts`).

## Security headers & untrusted data

* `next.config.ts` `headers()` sends CSP, `X-Frame-Options: DENY`,
  `Referrer-Policy: no-referrer`, `nosniff` and a `Permissions-Policy` on every
  non-`/api` route. The CSP allows inline scripts/styles (Next bootstrap,
  next-themes; no nonces so pages stay static) and `'unsafe-eval'` in dev only;
  images/media may come from any http(s) host (external item URLs). If a
  feature needs another source, extend the CSP there and check the browser
  console in dev and prod.
* `/api` responses carry the API's own headers.
* Values from the API or the URL go through `lib/safe.ts`: `safeNextPath` for
  `?next=` redirects (same origin only), `safeHttpUrl` before any `href`/`src`
  built from `item.url`/`content`/watcher URLs (otherwise render text),
  `safeCssColor` before a collection color reaches `style`. External media
  (item.url, not `/api/v1/media`) is click-to-load in previews and never
  loaded in list thumbnails; videos never autoplay.
* The remembered API key (`lib/session-key.ts`) is cleared on sign-out,
  recovery sign-in, device redeem, setup and when that key is revoked/rotated.

## UI rules

* Product name is **staqly** in all user-facing text.
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

## Rendering performance

Lists can hold thousands of items (inbox, collection views). An update must
re-render only the component it concerns.

* **React Compiler is on** (`reactCompiler: true`). It bails out of a whole
  component *silently* on some patterns, e.g. calling a function declared
  further down the component (hoisting), `?.`/`&&`/ternaries inside
  `try/catch`, or reading a variable before its declaration. The component
  then runs unmemoised. Declare helpers before use. When touching a hot
  component, check that it compiles: run babel with
  `babel-plugin-react-compiler` and a `logger` that prints `CompileError`
  events.
* The compiler does not memoise per item inside `.map()`. List rows and cards
  are `memo()` components (`InboxRow`, `QueueRow`, `GalleryCard`,
  `TableItemRow`, `ListRow`). Pass them primitives, the item object and
  **stable callbacks** (setState, or a callback that reads the latest URL
  params from a ref; see `CollectionPage.open`), never inline arrows or
  per-render closures over `useSearchParams()`.
* Item lists use `useItemsInfinite`, whose `structuralSharing` matches items
  by id (`shareItemPages`). Unchanged items keep their identity across
  refetches and local removals, so memoised rows skip rendering.
* After a mutation, write the server response into the cache
  (`syncItemInCache`, `removeItemFromLists`, `setQueryData`). Don't
  invalidate or refetch item lists: a refetch reloads every loaded page one by
  one.
* Don't poll paged lists with `refetchInterval`. The inbox polls
  `useRecentEvents` (one small request) and refetches the list only when an
  event appears that this tab didn't cause itself.
* Very long lists use `[content-visibility:auto]` with a
  `[contain-intrinsic-size:auto_<row height>]`, so off-screen rows skip
  layout and paint. This made virtualisation unnecessary at 3000 cards. It
  doesn't apply to `<tr>`.
* Heavy per-row children (dialogs, popovers) mount on first use, not per row
  (see `QuickReview`).

## Tests

Mock fetch (see `tests/`); cover both modes where behaviour differs. Keep
`npm test`, lint, typecheck and build green.
