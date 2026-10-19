import { Check } from 'lucide-react'

import { DOCS, SIGNUP, btnPrimary, btnSecondary, SELF_HOST } from '@/lib/site'

// Limits mirror docs/cloud.md (source of truth: apps/api/internal/app/plans.go).
const plans = [
  {
    name: 'Free',
    price: '$0',
    blurb: 'For trying staqly with real channels. No card required.',
    items: [
      '50 posts / month',
      '3 channels',
      'Unlimited collections & schedules',
      '2 alerts, 3 webhooks, 1 API key',
      '1 GB storage, 5 MB per file',
      'Data export without files',
    ],
    cta: 'Create free workspace',
  },
  {
    name: 'Pro',
    price: '$30',
    blurb: 'For agents that post every day. Every new workspace gets 7 days free.',
    items: [
      'Unlimited posts',
      'Unlimited channels',
      'Unlimited collections & schedules',
      'Unlimited alerts, webhooks & API keys',
      '100 GB storage, 100 MB per file',
      'Backups and full data export with files',
    ],
    cta: 'Start 7-day trial',
    popular: true,
  },
]

export function PricingCards() {
  return (
    <>
      <div className="mx-auto grid max-w-3xl gap-4 md:grid-cols-2">
        {plans.map((p) => (
          <div
            key={p.name}
            className={`relative flex flex-col rounded-2xl border bg-background p-7 ${
              p.popular
                ? 'border-brand shadow-[0_0_0_1px_var(--brand),0_30px_80px_-40px_var(--brand)]'
                : 'border-border'
            }`}
          >
            {p.popular && (
              <span className="absolute -top-3 left-7 rounded-full bg-(image:--brand-gradient) px-2.5 py-0.5 text-xs font-semibold text-(--brand-gradient-foreground)">
                Most popular
              </span>
            )}
            <h3 className="font-semibold">{p.name}</h3>
            <p className="mt-1 text-sm text-muted-foreground">{p.blurb}</p>
            <p className="mt-5 text-4xl font-semibold tracking-tight">
              {p.price} <span className="text-sm font-normal text-muted-foreground">/ month</span>
            </p>
            <ul className="mt-6 flex-1 space-y-2.5 text-sm">
              {p.items.map((i) => (
                <li key={i} className="flex items-center gap-2.5">
                  <Check className="size-4 shrink-0 text-brand" /> {i}
                </li>
              ))}
            </ul>
            <a href={SIGNUP} className={`${p.popular ? btnPrimary : btnSecondary} mt-8 justify-center`}>
              {p.cta}
            </a>
          </div>
        ))}
      </div>
      <p className="mx-auto mt-8 max-w-3xl text-center text-sm leading-relaxed text-muted-foreground">
        A post counts when it is created, by you, an agent or a schedule. Collections, schedules, MCP and the full
        history are unlimited on every plan; backups and data export with files come with Pro.
        <br />
        Rather run it yourself?{' '}
        <a href={SELF_HOST} className="font-medium text-foreground underline-offset-4 hover:underline">
          Self-host staqly
        </a>{' '}
        for free, with no limits ·{' '}
        <a href={`${DOCS}/plans`} className="font-medium text-foreground underline-offset-4 hover:underline">
          Plan details
        </a>
      </p>
    </>
  )
}
