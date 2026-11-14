/** @type {import('next').NextConfig} */
const config = {
  // ponytail: static HTML at build time; switch to a server only if the page needs per-request data.
  output: 'export',
  // Pages export as <route>/index.html: static hosts (App Platform) serve /pricing from it, but not
  // from pricing.html.
  trailingSlash: true,
  reactStrictMode: true,
  images: { unoptimized: true },
  // Shared workspace package, shipped as TypeScript source (packages/brand).
  transpilePackages: ['@staqly/brand'],
}

export default config
