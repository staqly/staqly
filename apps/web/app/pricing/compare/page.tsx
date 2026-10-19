import type { Metadata } from 'next'
import Link from 'next/link'

import { ComparePicker, CompareTable } from '@/components/compare-table'
import { CtaButtons, PageHero } from '@/components/site'
import { FAQ, TRIAL_DAYS } from '@/lib/plans'
import { pageMeta } from '@/lib/seo'
import { wrap } from '@/lib/site'

export const metadata: Metadata = pageMeta(
  'Compare plans',
  'Starter, Plus, Pro and Business side by side, per user: channels, alerts, webhooks, API keys, storage, backups and support. Every plan has unlimited posts, MCP and the REST API; yearly billing saves 20 % or more.',
  '/pricing/compare',
)

const faqLd = {
  '@context': 'https://schema.org',
  '@type': 'FAQPage',
  mainEntity: FAQ.map(([q, a]) => ({ '@type': 'Question', name: q, acceptedAnswer: { '@type': 'Answer', text: a } })),
}

export default function ComparePage() {
  return (
    <main className="flex-1">
      <script type="application/ld+json" dangerouslySetInnerHTML={{ __html: JSON.stringify(faqLd) }} />
      <PageHero eyebrow={<Link href="/pricing">Pricing</Link>} title="Compare plans">
        <p className="mt-5 max-w-2xl text-lg text-muted-foreground">
          Every plan has unlimited posts, the calendar, MCP, the REST API and the full history. They differ in how many
          channels, people and integrations they hold. Starter comes with a {TRIAL_DAYS}-day free trial (card required,
          cancel anytime). Prices are per user, excl. VAT.
        </p>
        <CtaButtons />
      </PageHero>

      <section aria-labelledby="matrix" className={`${wrap} py-12 sm:py-16`}>
        <h2 id="matrix" className="sr-only">
          Features by plan
        </h2>
        <div className="grid gap-6">
          <CompareTable />
          <ComparePicker />
        </div>
      </section>

      <section aria-labelledby="faq" className="border-t border-border bg-card/50">
        <div className={`${wrap} py-12 sm:py-16`}>
          <h2 id="faq" className="text-2xl font-semibold tracking-[-0.03em]">
            Questions about plans
          </h2>
          <div className="mt-6 grid gap-3 md:grid-cols-2">
            {FAQ.map(([q, a]) => (
              <div key={q} className="rounded-2xl border border-border bg-background p-5">
                <h3 className="font-semibold">{q}</h3>
                <p className="mt-2 text-sm leading-relaxed text-muted-foreground">{a}</p>
              </div>
            ))}
          </div>
        </div>
      </section>
    </main>
  )
}
