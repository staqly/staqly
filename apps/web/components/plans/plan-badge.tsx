import { planName } from '@/lib/notify'
import { cn } from '@/lib/utils'

const STYLES: Record<string, string> = {
  free: 'bg-muted text-muted-foreground ring-border',
  indie: 'bg-brand-gradient text-[#052817] ring-transparent',
  team: 'bg-zinc-900 text-white ring-white/10 dark:bg-white dark:text-zinc-900',
  unlocked: 'bg-emerald-500/12 text-emerald-700 ring-emerald-500/25 dark:text-emerald-300',
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
