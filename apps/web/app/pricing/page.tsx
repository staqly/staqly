import type { Metadata } from 'next'

import { PricingCards } from '@/components/pricing'
import { PageHero } from '@/components/site'
import { VAT_NOTE, X_BUNDLES, eur } from '@/lib/plans'
import { pageMeta } from '@/lib/seo'
import { wrap } from '@/lib/site'

export const metadata: Metadata = pageMeta(
  'Pricing',
  'Per user: Starter €15, Plus €29, Pro €59 and Business €99 a month, or €12, €23, €47 and €79 a month billed yearly (20 % off or more). Every plan has unlimited posts, MCP and the REST API, and includes posts to X (extra ones from €10 for 200); Starter comes with a 7-day free trial (card required, cancel anytime). Prices exclude VAT. Self-hosting is free.',
  '/pricing',
)

export default function PricingPage() {
  return (
    <main className="flex-1">
      <PageHero eyebrow="Pricing" title="Try Starter free for 7 days. Pick the plan that fits.">
        <p className="mt-5 max-w-2xl text-lg text-muted-foreground">
          Every plan gets the full product: the calendar, schedules, MCP, REST, signed webhooks and the complete
          history. Prices are per user: everyone you invite pays the same plan price. Plans differ in channels and
          storage. Billed yearly, every plan saves 20 % or more. Prices exclude VAT.
        </p>
      </PageHero>
      <section aria-labelledby="plans" className={`${wrap} py-12 sm:py-16`}>
        <h2 id="plans" className="sr-only">
          Plans
        </h2>
        <PricingCards />
      </section>
      <section aria-labelledby="x-posts" className={`${wrap} pb-16`}>
        <div className="rounded-2xl border bg-card p-6 sm:p-8">
          <h2 id="x-posts" className="text-xl font-semibold tracking-tight">
            Extra posts to X
          </h2>
          <p className="mt-2 max-w-2xl text-sm text-muted-foreground">
            X charges for every post sent through its API, so each plan includes posts to X every month (250, 450, 950
            or 1,600; thread replies count too). Need more? Buy a bundle once: it is used after the included posts and
            expires at the end of the billing period. Other platforms are not counted.
          </p>
          <ul className="mt-6 grid gap-3 sm:grid-cols-3">
            {X_BUNDLES.map((b) => (
              <li key={b.posts} className="rounded-xl border p-4">
                <p className="text-sm text-muted-foreground">{b.posts.toLocaleString('en-US')} posts to X</p>
                <p className="mt-1 text-2xl font-semibold tabular-nums">{eur(b.price)}</p>
                <p className="text-xs text-muted-foreground tabular-nums">
                  {eur(b.price / b.posts)} a post, one-time, {VAT_NOTE}
                </p>
              </li>
            ))}
          </ul>
        </div>
      </section>
    </main>
  )
}
