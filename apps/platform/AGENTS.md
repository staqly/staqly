<!-- BEGIN:nextjs-agent-rules -->

## This is NOT the Next.js you know

This version has breaking changes — APIs, conventions, and file structure may all differ from your training data. Read the relevant guide in `node_modules/next/dist/docs/` (resolved from this file's directory; in monorepos the `next` package may not be visible from the repo root) before writing any code. Heed deprecation notices.

This block is written and re-added by `next dev` — verify at `node_modules/next/dist/server/lib/generate-agent-files.js`. Removing it from a diff only re-creates the uncommitted change; committing it with your work keeps the tree clean.

<!-- END:nextjs-agent-rules -->

# apps/platform — staqly dashboard

Next.js 16 (App Router, React Compiler, `output: "export"`), React 19,
TypeScript 7 (native `tsc`), Tailwind v4, shadcn/ui, TanStack Query (all server
state), React Hook Form + Zod, lucide-react, next-themes, Vitest + Testing
Library, oxlint + oxfmt. Node 24 LTS (build only, Docker matches root `.nvmrc`, `@types/node` 24).

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
  function/object as prop" findings are noise. `no-await-in-loop` is off: our loops
  await on purpose (pages that need the previous cursor, one upload or
  invitation at a time). `jsx-a11y/no-noninteractive-tabindex` is off too: a
  scrolling region takes `tabIndex={0}` so keyboards can reach it (axe's
  scrollable-region-focusable, WCAG 2.1.1). `jsx-a11y/prefer-tag-over-role`
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
    `/posts/*` and `/channels/callback/*` onto that shell. A new `[id]` route needs
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
  to `ApiError`. Handle `401` (cloud → /login; local → the access-token sign-in
  in `AuthGate`), `402 plan_limit_exceeded`, `409`, `429`.
* Mode comes from `GET /v1/session` / `GET /v1/setup`: **local** has one
  workspace and no accounts, plans, billing, members, invitations, sign-in
  methods, API keys, recovery or welcome wizard; **cloud** shows them.
* Local sign-in: every browser signs in once with the install's access token
  (`components/auth/local-sign-in.tsx`, on /login and in `AuthGate` for a 401),
  exchanged for the session cookie with `POST /v1/sessions/api-key`. The link
  the server prints (`/#token=stk_local_...`) does the same: `lib/local-token.ts`
  takes the token out of the address bar when `<Providers>` loads (before any
  page redirects) and `AuthGate` signs in with it (connect-a-device links on
  `/connect` are left alone). Integrations shows the token masked with copy and
  rotate (`components/integrations/access-token.tsx`); snippets read it from
  `$STAQLY_API_KEY`, never print it. Never store secrets in localStorage;
  newly created keys live in memory only (`lib/session-key.ts`).

## Security headers & untrusted data

* The Go web server (`securityHeaders` in `apps/api/internal/web/web.go`)
  sends CSP, `X-Frame-Options: DENY`, `Referrer-Policy: no-referrer`,
  `nosniff` and a `Permissions-Policy` on every non-`/api` response; a static
  export can't set headers itself, and `pnpm dev` sends none. The CSP allows
  inline scripts/styles (Next bootstrap, next-themes; no nonces so pages stay
  static); images/media may come from any http(s) host (channel avatars, published posts).
  If a feature needs another source, extend the CSP there and check the
  browser console against `staqly serve` or the image.
* `/api` responses carry the API's own headers.
* Values from the API or the URL go through `lib/safe.ts`: `safeNextPath` for
  `?next=` redirects (same origin only), `safeHttpUrl` before any `href`/`src`
  built from a target's `external_url`, a channel's `profile_url` or an
  `avatar_url` (otherwise render text), `safeCssColor` before a collection
  color reaches `style`. Videos never autoplay.
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
* Colours: statuses, badges, timeline icons and action buttons take a tone
  from `lib/tones.ts` (neutral slate, info blue, warning amber, success
  emerald, attention orange, danger red, accent violet); post, target and
  channel statuses map to tones and icons in `lib/status.ts`. Don't pick a
  Tailwind hue in a component. Platform brand marks come from `lib/providers.tsx`
  (simple-icons, CC0) and never from ad-hoc SVGs.
* Post text: `components/posts/post-editor.tsx` wraps the vendored editorcn
  toolbar editor (`components/editor`, Tiptap 3, MIT, see NOTICE; owned code,
  edit it freely). Posts stay plain text: `lib/post-text.ts` turns the
  document into text (bold/italic as Unicode letters, lists as "•"/"1."
  lines, links keep their URL). ProseMirror does not type in jsdom, so unit
  tests mock `PostEditor` with a textarea; e2e/ui drives the real editor.
* Must work and look good on phones (320–430 px), tablets and desktop, light
  and dark; no horizontal page overflow; touch targets ≥ 40 px on touch
  screens (`pointer-coarse:` variants in the primitives; a small text link
  gets `pointer-coarse:-my-3 pointer-coarse:py-3`, a small chip or icon an
  `after:` inset). `pnpm --filter @staqly/e2e test:ui-responsive` checks every
  route at phone, tablet and desktop widths (see `e2e/README.md`).
  * Long unbroken text (URLs, IDs, file names, error messages) must never
    widen the page: a plain `grid` is `minmax(0, 1fr)` (`app/globals.css`), but
    flex children need `min-w-0`, inline-flex chips `max-w-full`, and text that
    may hold such words `wrap-anywhere` or `truncate`.
  * Truncated text gets a `title` with the full text (or opens the full
    thing); on phones prefer wrapping (`sm:line-clamp-*`) for errors.
  * Strips that scroll sideways (PageTabs, the collections strip) fade out the end that
    hides more (`hooks/use-scroll-fade.ts`); toolbars keep their buttons
    outside the scrolling part.
  * Tables reflow to cards on phones (collections, collection items, API
    keys) or scroll inside their own container, never clip columns.
* Focus and motion live in the primitives and `app/globals.css`: `--ring` is
  solid in light mode (a 1px `border-ring`/outline is ≥ 3:1), so a custom
  focus style needs `border-ring` or `outline-ring`, not only the soft
  `ring-ring/50` halo. Under `prefers-reduced-motion` overlays only fade
  (no slide/zoom), transitions jump and skeletons stop pulsing. Sheets pad
  their header, footer and close button for safe areas; floating content
  (menus, selects, popovers, tooltips) keeps 8 px from the screen edge.
* Use shadcn/ui primitives from `components/ui`; feature components live in
  `components/<feature>`; pages in `app/(app)/...`. Settings sections
  (`components/settings/settings-nav.tsx`): General, Members (cloud accounts,
  admins and owners), Account (the account owner), Plan & usage, Data &
  privacy, Backups (local or paid plans only), System. `useRole()` hides what a
  role cannot do; the server decides. The sidebar footer shows the person
  (Google name and picture, or the computer's user name locally) with Sign out. Notifications is its
  own page (`/notifications`, `components/notifications`).
* Notification channel secrets come back masked (`••••abcd`): show "•••• set"
  with Replace/Remove, never prefill them, and leave unchanged secrets out of
  PATCH bodies (`lib/channels.ts`).
* Downloads (event export, workspace ZIP) are plain same-origin links under
  `/api/...`; the session cookie authenticates them.
* Snippets shown to users use the real defaults: API `http://localhost:3333`,
  MCP `http://localhost:3333/mcp`, dashboard `http://localhost:8080`.

## Rendering performance

Lists can hold thousands of posts (the posts table, the calendar). An update
must re-render only the component it concerns.

* **React Compiler is on** (`reactCompiler: true`). It bails out of a whole
  component *silently* on some patterns, e.g. calling a function declared
  further down the component (hoisting), `?.`/`&&`/ternaries inside
  `try/catch`, or reading a variable before its declaration. The component
  then runs unmemoised. Declare helpers before use. When touching a hot
  component, check that it compiles: run babel with
  `babel-plugin-react-compiler` and a `logger` that prints `CompileError`
  events.
  `pnpm lint` also runs `scripts/check-react-compiler.mjs` over every app,
  component, hook and library module and fails on compiler errors, including
  silent optimisation bailouts.
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
  and `tests/render-perf.*.test.tsx` (posts, schedule, media) count renders
  per component (`tests/render-count.ts`, a DevTools hook); add a budget
  there when a list grows a new per-row control or a view gets a clock. If
  one fails, a change made some component render that shouldn't. Every
  component must compile: no `eslint-disable` of hook rules, no
  `try/finally` or `throw` inside `try` in a component or hook (use a
  module-level async function and `.then`/`.catch`).
* Clocks re-render whatever reads them: use `useIsPast(date)` for "is it in
  the past" and `useClock('minute' | 'hour')` in the smallest component
  that shows the time (the calendar's `NowLine`), never `useNow` on a page.
* Format dates with the cached formatters in `lib/format.ts` /
  `lib/schedule.ts`; `toLocale*String` with options builds a new
  `Intl.DateTimeFormat` per call (~30 us, per row or cell).
* `LiveUpdates` and `useRefetchOnForeignChange` skip events for posts this
  tab wrote into the cache (`isForeignEvent`), but never publishing outcomes
  or system events: those always refetch.
* The compiler does not memoise per item inside `.map()`. List rows and cards
  are `memo()` components (the calendar `Entry`, `AuditEventRow`). Pass them
  primitives, the post object and **stable callbacks** (setState, or a
  callback that reads the latest URL params from a ref), never inline arrows
  or per-render closures over `useSearchParams()`.
* Post lists use `usePostsInfinite` (the activity feed `useEventsInfinite`),
  whose `structuralSharing` matches entries by id (`sharePages`). Unchanged
  posts keep their identity across refetches and local removals, so memoised
  rows skip rendering. The media library does the same with
  `shareMediaPages` and writes uploads and edits into its cache
  (`components/media/media-cache.ts`).
* After a mutation, write the server response into the cache
  (`syncPostInCache`, `removePostFromLists`, `setQueryData`). Don't
  invalidate or refetch post lists: a refetch reloads every loaded page one by
  one. After creating a post use `refetchPostListsFromStart` (one request
  per list).
* Don't poll paged lists with `refetchInterval`. The posts table polls
  `useRecentEvents` (one small request) and refetches the list only when an
  event appears that this tab didn't cause itself
  (`useRefetchOnForeignChange`); `LiveUpdates` turns the same request into
  invalidations of the collections, channels, schedules and the posts named
  by the events.
* One collection list: `useCollections(false)` filters the archived ones out
  of the same cached request (`qk.collections()`); never add a second list
  query (it is ~1.2 KB per collection and reloads after every change).
* Links to `/posts/<id>` use `prefetch={false}`: every id is the same exported
  page, so per-link prefetching only repeats a HEAD and the route tree per
  link in view (hundreds in the posts table and the activity feed).
  `IdRoutePrefetch` in the shell warms the route once.
* Controls repeated per row use light markup (a button with checkbox
  semantics, like the composer's channel chips), not Radix's `Checkbox` (~12
  components each).
* Very long lists use `[content-visibility:auto]` with a
  `[contain-intrinsic-size:auto_<row height>]`, so off-screen rows skip
  layout and paint. This made virtualisation unnecessary at 3000 cards. It
  doesn't apply to `<tr>`.
* Heavy per-row children (dialogs, popovers) mount on first use, not per row
  (see `JsonBlock` and `hooks/use-lazy-dialog.ts`).
* Forms behind a button (the composer, the collection and schedule drawers) are
  `next/dynamic` + `useLazyDialog`, so zod and react-hook-form stay out of
  the page chunks (the `/settings` workspace name card too). Forms take
  `zodResolver` from `lib/zod.ts`, which sets zod's `jitless` (CSP without
  `unsafe-eval`); never import zod from a module every route loads, and never
  re-export the `z` namespace (it is then bundled whole, every locale).
* Budgets: `pnpm size` (after `pnpm build`, part of `pnpm check`) fails when
  a page's first-load JS (gzipped) or the CSS grows past
  `bundle-budget.json`; `node scripts/bundle-size.mjs` prints every route and
  the largest chunks. The budgets are ceilings with room: a change only raises
  one when it truly needs to, and says why in its commit, not in the file. Only Geist Sans is preloaded (`app/fonts.ts`; mono loads
  when used). The live performance run (Chrome, large seeded workspace) is
  `e2e/perf/` (e2e/README.md, Performance).
* The Go web server sends the export gzipped with an ETag (pages and RSC
  payloads revalidate as 304s; `_next/static` is immutable) and the API
  gzips `/v1` JSON, so payload sizes on the wire are ~1/4 to 1/25 of the raw
  ones; check `Content-Encoding` before optimising a payload by hand.
* The activity feed gets live events by `refreshEventLists` (fetch the first
  page, prepend newer events), never by invalidating `['events']`, which
  would refetch every loaded page.

## Tests

Mock fetch (see `tests/`); cover both modes where behaviour differs. Keep
`pnpm test`, lint, typecheck and build green. The browser suite in `e2e/ui`
(`pnpm --filter @staqly/e2e test:ui`, Playwright + axe-core against `staqly
serve`) covers every page and flow end to end, including an axe scan of every
page, dialog and menu in light and dark mode: keep it green too. Text never
gets `opacity-*` or a `/70` colour (contrast); coloured text takes a tone's
`text`, which is a 700 shade where 600 is under 4.5:1.
