# staqly.ai landing

Marketing site for [staqly](https://github.com/staqly/staqly): Next.js with a static export, Tailwind and Geist.
It uses the same brand tokens, logo and hero demo as the dashboard and the [docs](https://docs.staqly.ai)
(`packages/brand`, `@staqly/brand`); the link check after `next build` is `packages/check-links`.

```bash
pnpm install
pnpm dev               # http://localhost:8070
pnpm build             # static site in out/ + internal link check
pnpm preview           # serve out/
pnpm lint              # oxlint
pnpm format            # oxfmt (CI runs format:check)
pnpm typecheck
```

The monorepo CI (`.github/workflows/ci.yml`, job `web`) runs formatting, lint, types and the full build on every push and PR.
`robots.txt`, `sitemap.xml` and `llms.txt` are generated from `app/{robots,sitemap}.ts` and `app/llms.txt/route.ts`;
SEO text (tagline, description) lives in `lib/site.ts`; per-page metadata, the social card and structured data
helpers in `lib/seo.ts` (the card's source is `assets/brand/og-image.svg`).

| Path | What |
|---|---|
| `app/page.tsx` | Home page |
| `app/{agents,pricing,use-cases}` | Other pages; `use-cases/[slug]` is generated from `lib/use-cases.ts` |
| `components/site.tsx` | Nav, footer, closing CTA, shared bits |
| `components/pricing.tsx` | Plan cards (keep in sync with https://docs.staqly.ai/plans and `apps/api/internal/app/plans.go`) |
| `lib/site.ts` | URLs: docs, GitHub, hosted app, install command |
| `lib/env.ts` | The one place that reads `process.env` |

Build-time settings are read in `lib/env.ts` only: `NEXT_PUBLIC_APP_URL` sets the hosted dashboard (default
`https://app.staqly.ai`; signup goes to `/signup` there), `NEXT_PUBLIC_DOCS_URL` the docs (default
`https://docs.staqly.ai`) and `SITE_URL` the canonical URL (default `https://staqly.ai`). `make site` at the repo root
runs this site on :8070 next to the docs on :8090, linked to each other and the dev dashboard.
