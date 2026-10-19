import type { Metadata } from 'next'

import { PricingCards } from '@/components/pricing'
import { PageHero } from '@/components/site'
import { pageMeta } from '@/lib/seo'
import { wrap } from '@/lib/site'

export const metadata: Metadata = pageMeta(
  'Pricing',
  'Start free with 50 posts a month, 3 channels, unlimited collections and schedules, and a 7-day Pro trial, no card required. Pro is $30/month with unlimited posts, channels and alerts. Self-hosting is free.',
  '/pricing',
)

export default function PricingPage() {
  return (
    <main className="flex-1">
      <PageHero eyebrow="Pricing" title="Start free. Pay only for the hosted comfort.">
        <p className="mt-5 max-w-2xl text-lg text-muted-foreground">
          Every plan gets the full product: the calendar, schedules, MCP, REST, signed webhooks and the complete
          history. Pro removes the limits for agents that post every day.
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
