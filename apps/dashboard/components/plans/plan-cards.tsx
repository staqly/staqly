import { Check, Sparkles } from 'lucide-react'

import { SkeletonList } from '@/components/common/skeleton-list'
import { UNLIMITED, type Plan, type PlanLimits } from '@/lib/api'
import { formatBytes } from '@/lib/format'
import { cn } from '@/lib/utils'

export const EVERY_PLAN = 'Signed webhooks, full history and MCP on every plan'

const TAGLINES: Record<string, string> = {
  free: 'Kick the tyres. No card, no email.',
  pro: 'Everything unlimited, for agents running every day. Try it free for 7 days.',
}

function count(n: number, noun: string) {
  return n === UNLIMITED ? `Unlimited ${noun}` : `${n.toLocaleString('en-US')} ${noun}`
}

/** Feature lines derived from limits, used when the server does not send any. */
function limitFeatures(l: PlanLimits): string[] {
  return [
    l.decisions_per_month === UNLIMITED
      ? 'Unlimited approvals'
      : `${l.decisions_per_month.toLocaleString('en-US')} approvals / month`,
    'Unlimited items and collections',
    count(l.webhooks, 'webhooks'),
    count(l.api_keys, 'API keys'),
    l.storage_bytes === UNLIMITED ? 'Unlimited storage' : `${formatBytes(l.storage_bytes)} storage`,
  ]
}

function PlanCard({
  plan,
  current,
  cta,
}: {
  plan: Plan
  current?: boolean
  /** Call-to-action rendered at the bottom of the price block. */
  cta?: React.ReactNode
}) {
  const highlight = plan.highlight
  const features = plan.features?.length ? plan.features : limitFeatures(plan.limits)

  const body = (
    <div className="relative flex h-full flex-col rounded-[calc(var(--radius)*1.8)] bg-card p-5 text-card-foreground sm:p-6">
      <div className="flex items-center justify-between gap-2">
        <h3 className="text-base font-semibold tracking-tight">{plan.name}</h3>
        {current ? (
          <span className="rounded-full bg-foreground px-2 py-0.5 text-[11px] font-medium text-background">
            Current plan
          </span>
        ) : highlight ? (
          <span className="inline-flex items-center gap-1 rounded-full bg-brand-gradient px-2 py-0.5 text-[11px] font-semibold text-[#052817] shadow-sm shadow-emerald-500/30">
            <Sparkles className="size-3" /> Most popular
          </span>
        ) : null}
      </div>
      <p className="mt-1 text-sm text-muted-foreground">{TAGLINES[plan.id] ?? ''}</p>
      <div className="mt-5 flex items-baseline gap-1">
        <span className="text-4xl font-semibold tracking-tight tabular-nums">${plan.price_usd_month}</span>
        <span className="text-sm text-muted-foreground">/mo</span>
      </div>
      {cta && <div className="mt-5 grid">{cta}</div>}
      <div className="my-5 h-px bg-border" />
      <ul className="grid gap-2.5 text-sm" aria-label={`${plan.name} features`}>
        {[...features, EVERY_PLAN.replace(' on every plan', '')].map((f) => (
          <li key={f} className="flex items-start gap-2.5">
            <span
              className={cn(
                'mt-0.5 flex size-4 shrink-0 items-center justify-center rounded-full',
                highlight ? 'bg-brand text-brand-foreground' : 'bg-brand-soft text-brand',
              )}
            >
              <Check className="size-3" strokeWidth={3} />
            </span>
            <span>{f}</span>
          </li>
        ))}
      </ul>
    </div>
  )

  return (
    <div
      data-testid={`plan-${plan.id}`}
      data-current={current || undefined}
      className={cn(
        'relative rounded-[calc(var(--radius)*1.8+1px)] p-px',
        highlight
          ? 'bg-gradient-to-b from-[#7cf29c] via-[#22c774]/60 to-[#0b7a4b]/25 shadow-xl shadow-emerald-500/10'
          : 'bg-border',
        current && !highlight && 'bg-foreground/60 dark:bg-foreground/50',
      )}
    >
      {body}
    </div>
  )
}

export function PlanGrid({
  plans,
  loading,
  currentPlanId,
  renderCta,
  className,
}: {
  plans?: Plan[]
  loading?: boolean
  currentPlanId?: string
  renderCta?: (plan: Plan, current: boolean) => React.ReactNode
  className?: string
}) {
  if (loading) {
    return (
      <div className={cn('grid gap-4 md:grid-cols-3', className)}>
        <SkeletonList count={3} className="h-96 rounded-2xl" />
      </div>
    )
  }

  return (
    <div className={cn('grid items-stretch gap-4 md:grid-cols-3', className)} data-testid="plan-grid">
      {(plans ?? []).map((p) => (
        <PlanCard key={p.id} plan={p} current={p.id === currentPlanId} cta={renderCta?.(p, p.id === currentPlanId)} />
      ))}
    </div>
  )
}
