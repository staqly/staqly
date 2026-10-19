import { planName } from '@/lib/notify'
import { TONES } from '@/lib/tones'
import { cn } from '@/lib/utils'

const PAID = 'bg-brand-soft text-brand-strong ring-[color-mix(in_oklch,var(--brand)_35%,transparent)]'

const STYLES: Record<string, string> = {
  starter: 'bg-muted text-muted-foreground ring-border',
  plus: PAID,
  pro: PAID,
  business: PAID,
  unlocked: TONES.success.badge,
}

export function PlanBadge({ plan, className }: { plan?: string; className?: string }) {
  if (!plan) {
    return null
  }

  return (
    <span
      data-testid="plan-badge"
      className={cn(
        'inline-flex h-4 shrink-0 items-center rounded px-1 text-[9.5px] leading-none font-semibold tracking-wider uppercase ring-1 ring-inset',
        STYLES[plan] ?? STYLES.starter,
        className,
      )}
    >
      {planName(plan)}
    </span>
  )
}
