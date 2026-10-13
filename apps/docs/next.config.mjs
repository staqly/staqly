import { createMDX } from 'fumadocs-mdx/next'

const withMDX = createMDX()

// DOCS_BASE_PATH: the site's path prefix when it is not served from a domain
// root, e.g. /staqly on staqly.github.io/staqly (see .github/workflows/docs.yml).
const basePath = process.env.DOCS_BASE_PATH || ''

/** @type {import('next').NextConfig} */
const config = {
  output: 'export',
  reactStrictMode: true,
  basePath,
  // For plain <img>/<a> and fetches that Next does not prefix itself.
  env: { NEXT_PUBLIC_BASE_PATH: basePath },
}

export default withMDX(config)
