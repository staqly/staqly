import type { NextConfig } from 'next'

// A static export (out/) served by the Go admin listener, which streams /api/* to the admin API.
// `next dev` has no Go server in front, so it rewrites /api/* itself.
const isDev = process.env.NODE_ENV !== 'production'

const nextConfig: NextConfig = {
  output: isDev ? undefined : 'export',
  poweredByHeader: false,
  images: { unoptimized: true },
  // Shared workspace package, shipped as TypeScript source (packages/brand).
  transpilePackages: ['@staqly/brand'],
  turbopack: {
    rules: {
      '*.css': { loaders: ['@tailwindcss/turbopack'], as: '*.css' },
    },
  },
  ...(isDev && {
    async rewrites() {
      return [
        {
          source: '/api/:path*',
          destination: `${process.env.ADMIN_API_URL ?? 'http://localhost:8081/api'}/:path*`,
        },
      ]
    },
  }),
}

export default nextConfig
