// The one place that reads process.env (plain .mjs so next.config.mjs and scripts/ can import it too).
// NEXT_PUBLIC_BASE_PATH is inlined into the browser bundle; DOCS_BASE_PATH is read at build time.

export const env = {
  /** Path prefix when the site is not served from a domain root, e.g. /staqly on staqly.github.io/staqly. */
  basePath: process.env.NEXT_PUBLIC_BASE_PATH || process.env.DOCS_BASE_PATH || '',
  /** The landing page (staqly.ai), linked from the docs. */
  webUrl: process.env.NEXT_PUBLIC_WEB_URL || 'https://staqly.ai',
  /** Canonical site URL for metadata (build time). */
  siteUrl: process.env.SITE_URL ?? 'https://docs.staqly.ai',
  /** OpenAPI spec the API reference is built from; defaults to the API app's file. */
  openapiSpec: process.env.OPENAPI_SPEC,
}
