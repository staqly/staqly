import { execFileSync } from 'node:child_process'

import type { MetadataRoute } from 'next'

import { SITE_URL } from '@/lib/site'
import { TOOLS } from '@/lib/tools'
import { allCases } from '@/lib/use-cases'

export const dynamic = 'force-static'

function git(args: string[]) {
  try {
    return execFileSync('git', args, { encoding: 'utf8', stdio: ['ignore', 'pipe', 'ignore'] }).trim()
  } catch {
    return ''
  }
}

// A shallow clone dates every file to its one commit, and a wrong lastmod is worse than none: leave it out then.
const fullHistory = git(['rev-parse', '--is-shallow-repository']) === 'false'

/** The last commit touching the page's sources (paths relative to apps/web), or nothing without the history. */
function lastModified(sources: string[]) {
  return (fullHistory && git(['log', '-1', '--format=%cI', '--', ...sources])) || undefined
}

export default function sitemap(): MetadataRoute.Sitemap {
  const pages: [string, number, string[]][] = [
    ['', 1, ['app/page.tsx', 'components']],
    ['/pricing', 0.9, ['app/pricing/page.tsx', 'components/pricing.tsx', 'lib/plans.ts']],
    ['/pricing/compare', 0.7, ['app/pricing/compare', 'lib/plans.ts']],
    ['/agents', 0.8, ['app/agents']],
    ['/use-cases', 0.8, ['app/use-cases/page.tsx', 'lib/use-cases.ts']],
    ...allCases.map((u): [string, number, string[]] => [
      `/use-cases/${u.slug}`,
      0.7,
      ['app/use-cases/[slug]', 'lib/use-cases.ts'],
    ]),
    ['/tools', 0.8, ['app/tools/page.tsx', 'lib/tools.ts']],
    ...TOOLS.map((t): [string, number, string[]] => [`/tools/${t.slug}`, 0.7, [`app/tools/${t.slug}`]]),
  ]

  return pages.map(([path, priority, sources]) => ({
    url: `${SITE_URL}${path}`,
    lastModified: lastModified(sources),
    changeFrequency: 'weekly',
    priority,
  }))
}
