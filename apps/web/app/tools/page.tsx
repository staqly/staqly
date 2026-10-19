import type { Metadata } from 'next'

import { PageHero } from '@/components/site'
import { ToolCards } from '@/components/tools/tool-cards'
import { pageMeta } from '@/lib/seo'
import { wrap } from '@/lib/site'

export const metadata: Metadata = pageMeta(
  'Free social media tools: best time to post, character counter, aspect ratio checker, schedule validator',
  'Free tools for creators and social media teams: the best time to post on Instagram, TikTok, YouTube, X and LinkedIn in your time zone, a character counter for every platform, an aspect ratio checker with a crop preview, a posting schedule validator and a link-to-plain-text converter. No sign-up.',
  '/tools',
)

export default function ToolsPage() {
  return (
    <main className="flex-1">
      <PageHero eyebrow="Free tools" title="Small tools for posts that go everywhere.">
        <p className="mt-5 max-w-2xl text-lg text-muted-foreground">
          Free, no sign-up, and everything runs in your browser: nothing you type leaves this page.
        </p>
      </PageHero>
      <section className={`${wrap} py-12 sm:py-16`}>
        <ToolCards />
      </section>
    </main>
  )
}
