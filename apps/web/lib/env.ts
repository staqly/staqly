// The one place that reads process.env (build-time settings; NEXT_PUBLIC_* is inlined into the bundle).

export const env = {
  /** Hosted dashboard URL. */
  appUrl: process.env.NEXT_PUBLIC_APP_URL ?? 'https://app.staqly.ai',
  /** Documentation site. */
  docsUrl: process.env.NEXT_PUBLIC_DOCS_URL ?? 'https://docs.staqly.ai',
  /** Canonical site URL for metadata. */
  siteUrl: process.env.SITE_URL ?? 'https://staqly.ai',
}
