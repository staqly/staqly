# staqly docs

User documentation for staqly — Fumadocs (Next.js) built as a static site.
Hosted on GitHub Pages.

```bash
npm install
npm run dev        # http://localhost:8090
npm run build      # static export in out/ + internal link check
npm run preview    # npx serve@latest out -l 8090
```

The API reference is generated from `../api/openapi.yaml` (override with
`OPENAPI_SPEC=/path/to/openapi.yaml`).

## Hosting

GitHub Pages, published by `.github/workflows/docs.yml` on every push to
`main` that touches the docs: https://staqly.github.io/staqly (built with
`DOCS_BASE_PATH=/staqly`). Writing pages and conventions: [AGENTS.md](AGENTS.md).
