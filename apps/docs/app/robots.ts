import type { MetadataRoute } from 'next'

import { absUrl } from '@/lib/seo'

export const dynamic = 'force-static'

// Everything is public. /llms.mdx/ is the render source scripts/markdown-files.mjs copies to /<page>.md (those stay
// crawlable for agents); /api/search is the search index the search dialog loads.
export default function robots(): MetadataRoute.Robots {
  return {
    rules: { userAgent: '*', allow: '/', disallow: ['/llms.mdx/', '/api/search'] },
    sitemap: absUrl('/sitemap.xml'),
  }
}
