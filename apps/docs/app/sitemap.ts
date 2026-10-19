import type { MetadataRoute } from 'next'

import { absUrl } from '@/lib/seo'
import { source } from '@/lib/source'

export const dynamic = 'force-static'

// Every page at its canonical URL (no trailing slash). No lastmod: CI checks out one commit, so git dates would all
// be the same day, and a wrong lastmod is worse than none.
export default function sitemap(): MetadataRoute.Sitemap {
  return [{ url: absUrl('/') }, ...source.getPages().map((p) => ({ url: absUrl(p.url) }))]
}
