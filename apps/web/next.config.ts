import type { NextConfig } from 'next'

// The dashboard is a static export (out/) served by the Go web server
// (apps/api/internal/web), which also adds the security headers and streams
// /api/* to the API. `next dev` has no Go server in front, so it rewrites
// /api/* to the API itself.

const isDev = process.env.NODE_ENV === 'development'

const nextConfig: NextConfig = {
  output: isDev ? undefined : 'export',
  reactCompiler: true,
  poweredByHeader: false,
  images: { unoptimized: true },
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
      return [
        { source: '/api/:path*', destination: `${process.env.API_INTERNAL_URL || 'http://localhost:3333'}/:path*` },
      ]
    },
  }),
}

export default nextConfig
