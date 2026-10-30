'use client'

import { ArrowRight, Check } from 'lucide-react'
import Link from 'next/link'
import { useState } from 'react'

import { PLANS, TRIAL_DAYS, ctaLabel, perMonth, eur, type Interval, VAT_NOTE } from '@/lib/plans'
import { DOCS, SELF_HOST, arrowLink, btnPrimary, btnSecondary, signupFor } from '@/lib/site'

/** Monthly / Yearly switch above the plan cards. */
export function IntervalToggle({ value, onChange }: { value: Interval; onChange: (v: Interval) => void }) {
  return (
    <div
      role="radiogroup"
      aria-label="Billing period"
      className="inline-flex rounded-full border border-border bg-card p-1 text-sm"
    >
      {(
        [
          ['month', 'Monthly'],
          ['year', 'Yearly'],
        ] as const
      ).map(([v, label]) => (
        <button
          key={v}
          type="button"
          role="radio"
          aria-checked={value === v}
          onClick={() => onChange(v)}
          className={`inline-flex h-9 items-center gap-1.5 rounded-full px-4 font-medium transition-colors pointer-coarse:h-10 ${
            value === v ? 'bg-foreground text-background shadow-sm' : 'text-muted-foreground hover:text-foreground'
          }`}
        >
          {label}
          {v === 'year' && (
            <span className="rounded-full bg-(image:--brand-gradient) px-1.5 py-0.5 text-[11px] font-semibold text-(--brand-gradient-foreground)">
              Save 20%
            </span>
          )}
        </button>
      ))}
    </div>
  )
}

export function PricingCards() {
  const [interval, setPeriod] = useState<Interval>('year')

  return (
    <>
      <div className="mb-8 flex justify-center">
        <IntervalToggle value={interval} onChange={setPeriod} />
      </div>
      <div className="mx-auto grid max-w-6xl gap-4 sm:grid-cols-2 lg:grid-cols-4">
        {PLANS.map((p) => (
          <div
            key={p.id}
            data-plan={p.id}
            className={`relative flex flex-col rounded-2xl border bg-background p-6 ${
              p.highlight
                ? 'border-brand shadow-[0_0_0_1px_var(--brand),0_30px_80px_-40px_var(--brand)]'
                : 'border-border'
            }`}
          >
            {p.highlight && (
              <span className="absolute -top-3 left-6 rounded-full bg-(image:--brand-gradient) px-2.5 py-0.5 text-xs font-semibold text-(--brand-gradient-foreground)">
                Most popular
              </span>
            )}
            <h3 className="font-semibold">{p.name}</h3>
            <p className="mt-1 min-h-10 text-sm text-muted-foreground">{p.tagline}</p>
            <p className="mt-5 text-4xl font-semibold tracking-tight tabular-nums">
              {eur(perMonth(p, interval))}{' '}
              <span className="text-sm font-normal text-muted-foreground">per user / month</span>
            </p>
            <p className="mt-1 text-xs text-muted-foreground tabular-nums">
              {interval === 'year' ? `${eur(p.year)} per user, billed yearly` : 'Billed monthly'}, {VAT_NOTE}
            </p>
            <ul className="mt-6 flex-1 space-y-2.5 text-sm">
              {p.features.map((f) => (
                <li key={f} className="flex items-start gap-2.5">
                  <Check className="mt-0.5 size-4 shrink-0 text-brand" /> {f}
                </li>
              ))}
            </ul>
            <a
              href={signupFor(p.id, interval)}
              className={`${p.highlight ? btnPrimary : btnSecondary} mt-8 justify-center`}
            >
              {ctaLabel(p)}
            </a>
          </div>
        ))}
      </div>
      <p className="mx-auto mt-8 max-w-5xl text-center text-sm leading-relaxed text-muted-foreground">
        {/* One line on wide screens; phones wrap it. */}
        <span>
          Prices are per user. Channels, posts, storage and AI are shared per workspace, not multiplied by seats.
        </span>
        <br />
        Starter includes a {TRIAL_DAYS}-day free trial: card required, cancel anytime. Rather run it yourself?{' '}
        <a href={SELF_HOST} className="font-medium text-foreground underline-offset-4 hover:underline">
          Self-host Staqly Community Edition
        </a>{' '}
        for free; your hosting and provider costs apply ·{' '}
        <a href={`${DOCS}/plans`} className="font-medium text-foreground underline-offset-4 hover:underline">
          Plan details
        </a>
      </p>
      <div className="mt-6 flex justify-center">
        <Link href="/pricing/compare" className={arrowLink}>
          Compare every feature <ArrowRight className="size-4" />
        </Link>
      </div>
    </>
  )
}
