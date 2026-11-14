import type { NextConfig } from 'next'

import { env } from './lib/env'

// The dashboard is a static export (out/) served by the Go web server
// (apps/api/internal/web), which also adds the security headers and streams
// /api/* to the API. `next dev` has no Go server in front, so it rewrites
// /api/* to the API itself.

const isDev = env.isDev

const nextConfig: NextConfig = {
  output: isDev ? undefined : 'export',
  reactCompiler: true,
  poweredByHeader: false,
  images: { unoptimized: true },
  // Shared workspace package, shipped as TypeScript source (packages/brand).
  transpilePackages: ['@staqly/brand'],
  turbopack: {
    rules: {
      '*.css': {
        loaders: ['@tailwindcss/turbopack'],
        as: '*.css',
      },
    },
  },
  ...(isDev && {
    async rewrites() {
      return [{ source: '/api/:path*', destination: `${env.apiInternalUrl}/:path*` }]
    },
  }),
}

export default nextConfig
