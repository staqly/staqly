import type { NextConfig } from "next";

// Rewrites are resolved at build time, so in Docker API_INTERNAL_URL must be
// provided as a build argument (see Dockerfile).
const apiInternalUrl = (process.env.API_INTERNAL_URL ?? "http://localhost:3333").replace(/\/+$/, "");

const nextConfig: NextConfig = {
  output: "standalone",
  reactCompiler: true,
  poweredByHeader: false,
  experimental: {
    // Uploads go through the /api rewrite; Next buffers proxied bodies and
    // truncates them past this limit (default 10MB). Keep >= MAX_UPLOAD_MB.
    proxyClientMaxBodySize: "110mb",
  },
  turbopack: {
    rules: {
      "*.css": {
        loaders: ["@tailwindcss/turbopack"],
        as: "*.css",
      },
    },
  },
  async rewrites() {
    return [
      {
        source: "/api/:path*",
        destination: `${apiInternalUrl}/:path*`,
      },
    ];
  },
};

export default nextConfig;
