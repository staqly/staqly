import type { Metadata } from 'next'

import { PricingCards } from '@/components/pricing'
import { PageHero } from '@/components/site'
import { pageMeta } from '@/lib/seo'
import { wrap } from '@/lib/site'

export const metadata: Metadata = pageMeta(
  'Pricing',
  'Per user, excl. VAT: Starter €15, Plus €29, Pro €59, Business €99 a month, 20 % or more off yearly. Editors, analytics, MCP and REST on every plan; Starter has a 7-day trial.',
  '/pricing',
)

export default function PricingPage() {
  return (
    <main id="main" className="flex-1">
      <PageHero eyebrow="Pricing" title="Try Starter free for 7 days. Pick the plan that fits.">
        <p className="mt-5 max-w-2xl text-lg text-muted-foreground">
          Create, schedule and measure your posts. Choose your workspace capacity; prices are per user, excl. VAT.
        </p>
      </PageHero>
      <section aria-labelledby="plans" className={`${wrap} py-12 sm:py-16`}>
        <h2 id="plans" className="sr-only">
          Plans
        </h2>
        <PricingCards />
        <aside className="mx-auto mt-12 max-w-3xl rounded-2xl border border-border bg-card p-6">
          <h2 className="font-semibold">Coming soon: turn performance into your next post</h2>
          <p className="mt-2 text-sm leading-relaxed text-muted-foreground">
            Planned: AI performance summaries and recommendations on Plus and above, scheduled reports on Pro and above,
            and client-ready exports on Business. These features are not available yet; timing and plan allocation may
            change. Choose your plan for what you can use today.
          </p>
        </aside>
      </section>
    </main>
  )
}
