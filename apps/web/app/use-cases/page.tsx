import type { Metadata } from 'next'

import { PageHero } from '@/components/site'
import { UseCaseCards } from '@/components/use-case-cards'
import { pageMeta } from '@/lib/seo'
import { DOCS, wrap } from '@/lib/site'
import { allCases } from '@/lib/use-cases'

export const metadata: Metadata = pageMeta(
  'Use cases: human approval for AI agents',
  'How teams use staqly: approve AI-generated posts before they publish, gate risky agent actions and production deploys, approve from chat, triage research feeds and review on the phone.',
  '/use-cases',
)

export default function UseCasesPage() {
  return (
    <main className="flex-1">
      <PageHero eyebrow="Use cases" title="What people use staqly for.">
        <p className="mt-5 max-w-2xl text-lg text-muted-foreground">
          Six places where a human decision sits between what an agent makes and what happens next — in the real world,
          in production or in front of your audience.
        </p>
      </PageHero>
      <section className={`${wrap} py-12 sm:py-16`}>
        <UseCaseCards items={allCases} />
      </section>
      <section className="border-t border-border">
        <div className={`${wrap} flex flex-col items-center py-12 sm:py-16 text-center`}>
          <h2 className="text-3xl font-semibold tracking-[-0.03em]">Not seeing your case?</h2>
          <p className="mt-4 max-w-xl text-muted-foreground">
            The pattern is always the same: send, review, act. The{' '}
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
