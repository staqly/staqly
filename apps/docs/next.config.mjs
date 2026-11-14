import { createMDX } from 'fumadocs-mdx/next'

import { env } from './lib/env.mjs'

const withMDX = createMDX()

// DOCS_BASE_PATH: the site's path prefix when it is not served from a domain
// root, e.g. /staqly on staqly.github.io/staqly without the custom domain.
const basePath = env.basePath

/** @type {import('next').NextConfig} */
const config = {
  output: 'export',
  reactStrictMode: true,
  basePath,
  // Shared workspace package, shipped as TypeScript source (packages/brand).
  transpilePackages: ['@staqly/brand'],
  // For plain <img>/<a> and fetches that Next does not prefix itself.
  env: { NEXT_PUBLIC_BASE_PATH: basePath },
}

export default withMDX(config)
