// The one place that reads process.env. NODE_ENV is inlined at build time, so these are safe in the browser;
// SITE_URL and API_INTERNAL_URL are build/dev-server settings only.

export const env = {
  isDev: process.env.NODE_ENV === 'development',
  isTest: process.env.NODE_ENV === 'test',
  /** Canonical site URL for metadata (build time). */
  siteUrl: process.env.SITE_URL ?? 'https://staqly.ai',
  /** API the `next dev` rewrite proxies /api/* to. */
  apiInternalUrl: process.env.API_INTERNAL_URL || 'http://localhost:3333',
}
