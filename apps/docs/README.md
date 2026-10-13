# staq.ly docs

User documentation for staq.ly — Fumadocs (Next.js) built as a static site.
Hosted separately from the app.

```bash
npm install
npm run dev        # http://localhost:8090
npm run build      # static export in out/ + internal link check
npm run preview    # npx serve@latest out -l 8090 (or run the Docker image)
```

The API reference is generated from `../api/openapi.yaml` (override with
`OPENAPI_SPEC=/path/to/openapi.yaml`).

## Docker

From the repository root — the build context is `apps/docs`, and the OpenAPI
contract comes in through the BuildKit named context `api`:

```bash
docker build --build-context api=apps/api -t staqly-docs apps/docs
docker run --rm -p 8090:8090 staqly-docs      # → http://localhost:8090
```

The image is Caddy (non-root, port 8090, `auto_https` off — terminate TLS in front, or see the `Caddyfile` comment to let Caddy serve a domain) serving plain static files. Writing
pages and conventions: [AGENTS.md](AGENTS.md).
