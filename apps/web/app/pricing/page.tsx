import type { Metadata } from 'next'

import { PricingCards } from '@/components/pricing'
import { PageHero } from '@/components/site'
import { pageMeta } from '@/lib/seo'
import { wrap } from '@/lib/site'

export const metadata: Metadata = pageMeta(
  'Pricing',
  'Per user, excl. VAT: Starter €15, Plus €29, Pro €59, Business €99 a month, 20 % or more off yearly. Unlimited posts, MCP and REST on every plan; Starter has a 7-day trial.',
  '/pricing',
)

export default function PricingPage() {
  return (
    <main id="main" className="flex-1">
      <PageHero eyebrow="Pricing" title="Try Starter free for 7 days. Pick the plan that fits.">
        <p className="mt-5 max-w-2xl text-lg text-muted-foreground">
          Every plan has everything. Pick by how many channels you run; prices are per user, excl. VAT.
        </p>
      </PageHero>
      <section aria-labelledby="plans" className={`${wrap} py-12 sm:py-16`}>
        <h2 id="plans" className="sr-only">
          Plans
        </h2>
        <PricingCards />
      </section>
    </main>
  )
}
