import type { Metadata } from 'next'

import { PageHero } from '@/components/site'
import { ToolCards } from '@/components/tools/tool-cards'
import { pageMeta } from '@/lib/seo'
import { wrap } from '@/lib/site'

export const metadata: Metadata = pageMeta(
  'Free social media tools for creators',
  'Best time to post, a character counter for every platform, an aspect ratio checker, a schedule validator, a link cleaner, an Instagram grid maker and carousel splitter, a LinkedIn text formatter and a YouTube title checker. Free, no sign-up.',
  '/tools',
)

export default function ToolsPage() {
  return (
    <main id="main" className="flex-1">
      <PageHero eyebrow="Free tools" title="Small tools for posts that go everywhere.">
        <p className="mt-5 max-w-2xl text-lg text-muted-foreground">
          Free, no sign-up, and everything runs in your browser: nothing you type leaves this page.
        </p>
      </PageHero>
      <section aria-labelledby="all-tools" className={`${wrap} py-12 sm:py-16`}>
        <h2 id="all-tools" className="sr-only">
          All tools
        </h2>
        <ToolCards />
      </section>
    </main>
  )
}
