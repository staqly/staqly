import { ArrowRight, Check } from 'lucide-react'
import type { Metadata } from 'next'
import Link from 'next/link'

import { PricingCards } from '@/components/pricing'
import { PageHero, SectionHead } from '@/components/site'
import { TerminalCard } from '@/components/terminal'
import { FAQ, TRIAL_DAYS } from '@/lib/plans'
import { pageMeta } from '@/lib/seo'
import { DOCS, INSTALL, arrowLink, wrap } from '@/lib/site'

export const metadata: Metadata = pageMeta(
  'Pricing',
  'Per user, excl. VAT: Starter €15, Plus €29, Pro €59, Business €99 a month, 20 % or more off yearly. Editors, analytics, MCP and REST on every plan; Starter has a 7-day trial.',
  '/pricing',
)

// What no plan leaves out (lib/plans.ts, apps/docs/content/docs/plans.mdx).
const included = [
  'Calendar, queues and recurring schedules',
  'The composer with per-channel text and previews',
  'Design, video and audio editors, media library',
  'The AI assistant, from a shared monthly AI allowance',
  'Preview links with comments for clients',
  'Publishing and performance analytics',
  'MCP for your agents, REST API, SDKs and CLI',
  'Signed webhooks and the full history of every post',
]

export default function PricingPage() {
  return (
    <main id="main" className="flex-1">
      <PageHero
        eyebrow="Pricing"
        title={
          <>
            Try Starter free for {TRIAL_DAYS} days. <span className="sq-gradient-text">Pick the plan that fits.</span>
          </>
        }
      >
        <p className="mt-6 max-w-2xl text-lg leading-relaxed text-muted-foreground">
          Create, schedule and measure your posts. Choose your workspace capacity; prices are per user, excl. VAT.
        </p>
      </PageHero>
      <section aria-labelledby="plans" className={`${wrap} py-14 sm:py-20`}>
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

      <section className="border-y border-border bg-card/40">
        <div className={`${wrap} grid gap-12 py-20 sm:py-24 lg:grid-cols-[minmax(0,0.8fr)_minmax(0,1.2fr)]`}>
          <SectionHead eyebrow="On every plan" title="The same tools on every plan.">
            Bigger plans add capacity: channels, new posts a month, AI allowance, X and LinkedIn posts, and storage.
          </SectionHead>
          <ul className="grid content-start gap-x-8 gap-y-4 sm:grid-cols-2">
            {included.map((f) => (
              <li key={f} className="flex gap-3 text-[15px] leading-relaxed">
                <Check className="mt-1 size-4 shrink-0 text-brand" /> {f}
              </li>
            ))}
          </ul>
        </div>
      </section>

      <section aria-labelledby="faq" className={`${wrap} py-20 sm:py-24`}>
        <div className="flex flex-wrap items-end justify-between gap-4">
          <SectionHead eyebrow="FAQ" title={<span id="faq">Questions about plans</span>} />
          <Link href="/pricing/compare" className={arrowLink}>
            Compare every feature <ArrowRight className="size-4" />
          </Link>
        </div>
        <div className="mt-10 grid gap-3 md:grid-cols-2">
          {FAQ.slice(0, 6).map(([q, a]) => (
            <div key={q} className="rounded-2xl border border-border bg-card p-6">
              <h3 className="font-semibold">{q}</h3>
              <p className="mt-2 text-sm leading-relaxed text-muted-foreground">{a}</p>
            </div>
          ))}
        </div>
      </section>

      <section className={`${wrap} pb-20 sm:pb-24`}>
        <div className="grid gap-10 rounded-3xl border border-dashed border-border p-7 sm:p-10 lg:grid-cols-[minmax(0,0.9fr)_minmax(0,1.1fr)] lg:items-center">
          <div>
            <p className="text-sm font-semibold text-brand-strong">Community Edition</p>
            <h2 className="mt-3 text-2xl font-semibold tracking-tight">Rather run it yourself? It’s free.</h2>
            <p className="mt-3 leading-relaxed text-muted-foreground">
              Staqly is open source (AGPL-3.0). Run it on your own computer or server with no plan limits; you take care
              of backups and updates.
            </p>
            <a href={`${DOCS}/quickstart`} className={`mt-6 ${arrowLink} pointer-coarse:mt-3.5`}>
              Self-hosting quickstart <ArrowRight className="size-4" />
            </a>
          </div>
          <TerminalCard code={`$ ${INSTALL}`} />
        </div>
      </section>
    </main>
  )
}
