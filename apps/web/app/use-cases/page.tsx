import type { Metadata } from 'next'

import { PageHero } from '@/components/site'
import { UseCaseCards } from '@/components/use-case-cards'
import { pageMeta } from '@/lib/seo'
import { DOCS, wrap } from '@/lib/site'
import { allCases } from '@/lib/use-cases'

export const metadata: Metadata = pageMeta(
  'Use cases: a post scheduler for people and their agents',
  'How people use staqly: an agent writes a week of posts and a schedule publishes one every morning, one post in every channel\u2019s length, publishing through n8n, feeding a schedule from your endpoint, alerts and the calendar on your phone.',
  '/use-cases',
)

export default function UseCasesPage() {
  return (
    <main className="flex-1">
      <PageHero eyebrow="Use cases" title="What people use staqly for.">
        <p className="mt-5 max-w-2xl text-lg text-muted-foreground">
          Six ways a collection, a schedule and a few channels replace copying posts around by hand, whether the posts
          come from you, an agent or a script.
        </p>
      </PageHero>
      <section className={`${wrap} py-12 sm:py-16`}>
        <UseCaseCards items={allCases} />
      </section>
      <section className="border-t border-border">
        <div className={`${wrap} flex flex-col items-center py-12 sm:py-16 text-center`}>
          <h2 className="text-3xl font-semibold tracking-[-0.03em]">Not seeing your case?</h2>
          <p className="mt-4 max-w-xl text-muted-foreground">
            The pattern is always the same: write, schedule, publish. The{' '}
            <a href={`${DOCS}/concepts`} className="font-medium text-foreground underline-offset-4 hover:underline">
              concepts guide
            </a>{' '}
            shows how to fit it to anything.
          </p>
        </div>
      </section>
    </main>
  )
}
