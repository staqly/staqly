/** @type {import('next').NextConfig} */
const config = {
  // ponytail: static HTML at build time; switch to a server only if the page needs per-request data.
  output: 'export',
  // Pages export as <route>/index.html: static hosts (App Platform) serve /pricing from it, but not
  // from pricing.html.
  trailingSlash: true,
  reactStrictMode: true,
  images: { unoptimized: true },
  // Not experimental.inlineCss: Next.js puts the CSS in the HTML twice (the <style> and the RSC payload), which
  // nearly doubles the page and scored worse in Lighthouse than the one render-blocking stylesheet.
  // Shared workspace package, shipped as TypeScript source (packages/brand).
  transpilePackages: ['@staqly/brand'],
}

export default config
