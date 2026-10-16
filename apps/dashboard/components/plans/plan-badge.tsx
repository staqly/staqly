import { planName } from '@/lib/notify'
import { TONES } from '@/lib/tones'
import { cn } from '@/lib/utils'

const STYLES: Record<string, string> = {
  free: 'bg-muted text-muted-foreground ring-border',
  pro: 'bg-brand-gradient text-[#052817] ring-transparent',
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
        'inline-flex h-[18px] shrink-0 items-center rounded-full px-1.5 text-[10.5px] leading-none font-semibold tracking-wide uppercase ring-1 ring-inset',
        STYLES[plan] ?? STYLES.free,
        className,
      )}
    >
      {planName(plan)}
    </span>
  )
}
