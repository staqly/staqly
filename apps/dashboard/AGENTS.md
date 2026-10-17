<!-- BEGIN:nextjs-agent-rules -->

## This is NOT the Next.js you know

This version has breaking changes — APIs, conventions, and file structure may all differ from your training data. Read the relevant guide in `node_modules/next/dist/docs/` (resolved from this file's directory; in monorepos the `next` package may not be visible from the repo root) before writing any code. Heed deprecation notices.

This block is written and re-added by `next dev` — verify at `node_modules/next/dist/server/lib/generate-agent-files.js`. Removing it from a diff only re-creates the uncommitted change; committing it with your work keeps the tree clean.

<!-- END:nextjs-agent-rules -->

# apps/dashboard — staqly dashboard

Next.js 16 (App Router, React Compiler, `output: "export"`), React 19,
TypeScript 7 (native `tsc`), Tailwind v4, shadcn/ui, TanStack Query (all server
state), React Hook Form + Zod, lucide-react, next-themes, Vitest + Testing
Library, oxlint + oxfmt. Node 24 LTS (build only, Docker `node:24-alpine`, `@types/node` 24).

## Commands

```bash
pnpm dev               # needs the API: API_INTERNAL_URL=http://localhost:3333 (default)
pnpm test              # vitest
pnpm lint && pnpm typecheck && pnpm build
pnpm format            # oxfmt --write; `pnpm format:check` in CI
pnpm gen               # regenerate lib/api-types.ts from ../api/openapi.yaml
```

* `gen` runs openapi-typescript in an isolated `npx` environment pinned to
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
  (`internal/web`, in `server` / `staqly serve` and the `ghcr.io/staqly/staqly` image) embeds `out/`, sends
  the security headers and streams same-origin `/api/*` to the API in the same
  process (the standalone `cmd/dashboard` uses `API_INTERNAL_URL`, default
  `http://api:3333`). `make dashboard` builds the export into `apps/api/web/`.
  * So: no route handlers, server actions, `cookies()`/`headers()`, middleware
    or `redirect()` that depends on the request. Everything is client-side.
  * `[id]` pages are exported once as `_` (`generateStaticParams`) and read the
    real id in the browser with `useRouteId()`; the Go server maps
    `/items/*` and `/collections/*` onto that shell. A new `[id]` route needs
    an entry in `idRoutes` in `apps/api/internal/web/web.go`.
  * `pnpm dev` rewrites `/api/*` to the API (`API_INTERNAL_URL`, default
    `http://localhost:3333`); dev only.
  * The proxy contract (client `X-Forwarded-*`, `Forwarded`, `X-Real-IP`
    dropped; `X-Forwarded-Host` = the browser's Host, never empty; no
    `X-Forwarded-For`; bodies streamed, 110 MB cap → 413) lives in and is
    tested by `internal/web`.
* `lib/env.ts` is the only module that reads `process.env` (`NODE_ENV`,
  `SITE_URL`, `API_INTERNAL_URL`); import `env` from it everywhere else.
* `lib/api.ts` is the typed fetch client: sends `X-CSRF-Token` on unsafe
  requests when the session has one, maps `{error:{code,message,fields,details}}`
  to `ApiError`. Handle `401` (→ /login or /setup), `402 plan_limit_exceeded`,
  `403 not_local` (device not connected), `409`, `429`.
* Mode comes from `GET /v1/session` / `GET /v1/setup`: **local** hides
  setup/login/API keys/recovery; **cloud** shows them. Never store secrets in
  localStorage; newly created keys live in memory only (`lib/session-key.ts`).

## Security headers & untrusted data

* The Go web server (`securityHeaders` in `apps/api/internal/web/web.go`)
  sends CSP, `X-Frame-Options: DENY`, `Referrer-Policy: no-referrer`,
  `nosniff` and a `Permissions-Policy` on every non-`/api` response; a static
  export can't set headers itself, and `pnpm dev` sends none. The CSP allows
  inline scripts/styles (Next bootstrap, next-themes; no nonces so pages stay
  static); images/media may come from any http(s) host (external item URLs).
  If a feature needs another source, extend the CSP there and check the
  browser console against `staqly serve` or the image.
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
* Forms (one pattern everywhere, see `lib/form-errors.ts`): required fields get
  `required` on the Label/FieldLabel (red `*`) and `aria-required` on the
  control; an invalid field gets `data-invalid` on its Field, `aria-invalid`
  on the control and a FieldError right under it. Errors appear on submit,
  then follow every change. Submit buttons are never disabled for invalid
  input (only while pending or when nothing changed); a 422's `fields` land
  under their fields (`setFormApiError` / `formApiError`), only the rest is
  toasted. Type-to-confirm and "I have saved..." checkboxes use
  `ConfirmNameField` / `AckCheckbox`.
* Colours: statuses, badges, timeline icons, inbox tabs and action buttons
  take a tone from `lib/tones.ts` (neutral slate, info blue, warning amber,
  success emerald, attention orange, danger red, accent violet; rejected is a
  soft red pill, failed the solid red badge); item
  statuses map to tones and icons in `lib/status.ts`. Don't pick a Tailwind
  hue in a component. Approve/Reject buttons match staqly.ai (neutral border,
  green/red text, Reject first); Failed is the one solid badge.
* Must work and look good on phones (375–430 px), tablets and desktop, light
  and dark; no horizontal page overflow; touch targets ≥ 44 px.
* Focus and motion live in the primitives and `app/globals.css`: `--ring` is
  solid in light mode (a 1px `border-ring`/outline is ≥ 3:1), so a custom
  focus style needs `border-ring` or `outline-ring`, not only the soft
  `ring-ring/50` halo. Under `prefers-reduced-motion` overlays only fade
  (no slide/zoom), transitions jump and skeletons stop pulsing. Sheets pad
  their header, footer and close button for safe areas; floating content
  (menus, selects, popovers, tooltips) keeps 8 px from the screen edge.
* Use shadcn/ui primitives from `components/ui`; feature components live in
  `components/<feature>`; pages in `app/(app)/...`. Settings sections:
  General, Plan & usage, Notifications (`components/notifications`), Data &
  privacy, Backups, System.
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
* Patterns that silently un-memoise a whole page (no CompileError, the
  compiler just drops the scopes): a fresh `?? []` or a bare `.filter()`
  result that lives until the JSX (the compiler can't tell it's an array, so
  it stays "mutable" across hook calls; use a module constant / `useMemo`);
  and passing a whole TanStack Query/Mutation result around (`list`, `run`):
  it is a new object on every render, so take out `refetch`,
  `fetchNextPage`, `mutate`, `isPending` instead. JSX inside `?:`/`&&` is
  memoised as one block, so give big page sections (toolbar, filters, list,
  preview, dialogs) their own `const x = (<Toolbar />)` statements.
* Vitest compiles app code with the React Compiler too (`vitest.config.ts`),
  and `tests/render-perf.test.tsx` counts renders per component
  (`tests/render-count.ts`, a DevTools hook) for inbox j/k/x/approve, the
  collection item sheet, the activity feed and shell navigation, with upper
  bounds. If it fails, a change made some component render that shouldn't.
* The compiler does not memoise per item inside `.map()`. List rows and cards
  are `memo()` components (`InboxRow`, `QueueRow`, `TableItemRow`, `TableCard`,
  `AuditEventRow`, `WatcherCard`, sidebar `NavCollectionItem`). Pass them primitives, the item object and
  **stable callbacks** (setState, or a callback that reads the latest URL
  params from a ref; see `CollectionPage.open`), never inline arrows or
  per-render closures over `useSearchParams()`.
* Item lists use `useItemsInfinite` (the activity feed `useEventsInfinite`),
  whose `structuralSharing` matches entries by id (`shareItemPages`). Unchanged items keep their identity across
  refetches and local removals, so memoised rows skip rendering.
* After a mutation, write the server response into the cache
  (`syncItemInCache`, `removeItemFromLists`, `setQueryData`). Don't
  invalidate or refetch item lists: a refetch reloads every loaded page one by
  one. After creating an item use `refetchItemListsFromStart` (one request
  per list).
* Don't poll paged lists with `refetchInterval`. The inbox polls
  `useRecentEvents` (one small request) and refetches the list only when an
  event appears that this tab didn't cause itself.
* Very long lists use `[content-visibility:auto]` with a
  `[contain-intrinsic-size:auto_<row height>]`, so off-screen rows skip
  layout and paint. This made virtualisation unnecessary at 3000 cards. It
  doesn't apply to `<tr>`.
* Heavy per-row children (dialogs, popovers) mount on first use, not per row
  (see `JsonBlock`, `useItemOptions` and `hooks/use-lazy-dialog.ts`).
* Forms behind a button (add/edit sheets and dialogs, the item Edit tab) are
  `next/dynamic` + `useLazyDialog`, so zod and react-hook-form stay out of
  the page chunks. Never import `z` from a module every route loads
  (`components/providers.tsx` sets zod's `jitless` through `zod/v4/core`).
* The activity feed gets live events by `refreshEventLists` (fetch the first
  page, prepend newer events), never by invalidating `['events']`, which
  would refetch every loaded page.

## Tests

Mock fetch (see `tests/`); cover both modes where behaviour differs. Keep
`pnpm test`, lint, typecheck and build green.
