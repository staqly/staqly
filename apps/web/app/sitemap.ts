import type { MetadataRoute } from 'next'

import { SITE_URL } from '@/lib/site'
import { TOOLS } from '@/lib/tools'
import { allCases } from '@/lib/use-cases'

export const dynamic = 'force-static'

export default function sitemap(): MetadataRoute.Sitemap {
  const pages: [string, number][] = [
    ['', 1],
    ['/pricing', 0.9],
    ['/pricing/compare', 0.7],
    ['/agents', 0.8],
    ['/use-cases', 0.8],
    ...allCases.map((u): [string, number] => [`/use-cases/${u.slug}`, 0.7]),
    ['/tools', 0.8],
    ...TOOLS.map((t): [string, number] => [`/tools/${t.slug}`, 0.7]),
  ]

  return pages.map(([path, priority]) => ({ url: `${SITE_URL}${path}`, changeFrequency: 'weekly', priority }))
}
