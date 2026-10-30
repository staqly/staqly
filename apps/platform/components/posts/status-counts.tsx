import type { PostStatus } from '@/lib/api'
import { POST_STATUS_META } from '@/lib/status'
import { TONES } from '@/lib/tones'
import { cn } from '@/lib/utils'

const COUNT_ORDER: PostStatus[] = ['scheduled', 'publishing', 'draft', 'published', 'failed']

/** What a cell says under its number (the statuses' own descriptions, shorter where they are long). */
const CAPTION: Record<PostStatus, string> = {
  scheduled: 'Goes out at its time',
  publishing: 'Going out right now',
  draft: 'Not scheduled yet',
  published: 'Out on every channel',
  failed: 'Retry or reschedule',
}

/**
 * A collection's posts by status, in a card under the title: one cell per status (Publishing only while a post
 * is going out), each a toggle that filters the list below to it. Failed posts turn their cell red.
 */
export function StatusSummary({
  counts,
  active,
  onSelect,
  className,
}: {
  counts: Record<string, number>
  /** The status the list is filtered to. */
  active?: PostStatus | ''
  onSelect: (status: PostStatus | undefined) => void
  className?: string
}) {
  const shown = COUNT_ORDER.filter((s) => s !== 'publishing' || (counts[s] ?? 0) > 0)

  return (
    <section aria-label="Posts by status" className={cn('overflow-hidden rounded-xl border', className)}>
      <div
        className={cn(
          'grid grid-cols-2 gap-px bg-border sm:grid-cols-[repeat(var(--cells),minmax(0,1fr))]',
          '[&>*:last-child:nth-child(odd)]:max-sm:col-span-2',
        )}
        style={{ '--cells': shown.length } as React.CSSProperties}
        data-testid="status-summary"
      >
        {shown.map((s) => {
          const meta = POST_STATUS_META[s]
          const n = counts[s] ?? 0
          const on = active === s
          const alarm = s === 'failed' && n > 0

          return (
            <button
              key={s}
              type="button"
              aria-pressed={on}
              disabled={n === 0 && !on}
              onClick={() => onSelect(on ? undefined : s)}
              data-testid={`status-count-${s}`}
              className={cn(
                'group grid gap-1 bg-card px-4 py-3 text-left text-card-foreground transition-colors outline-ring',
                'hover:bg-muted/60 focus-visible:z-10 focus-visible:outline-2 focus-visible:-outline-offset-2',
                'disabled:cursor-default disabled:hover:bg-card',
                alarm && 'bg-red-500/[0.06] hover:bg-red-500/10 dark:bg-red-400/[0.07]',
                on && 'bg-muted hover:bg-muted',
              )}
            >
              <span className="flex items-center gap-2 text-xs font-medium text-muted-foreground">
                <span aria-hidden className={cn('grid size-6 place-items-center rounded-md', TONES[meta.tone].chip)}>
                  <meta.Icon className="size-3.5" />
                </span>
                {meta.label}
                {on && <span className="ml-auto text-[11px] font-normal">Filtered</span>}
              </span>
              <span
                className={cn(
                  'text-2xl font-semibold tracking-tight tabular-nums',
                  n === 0 && 'text-muted-foreground',
                  alarm && TONES.danger.text,
                )}
              >
                {n}
              </span>
              <span className="truncate text-xs text-muted-foreground">{n === 0 ? 'None' : CAPTION[s]}</span>
            </button>
          )
        })}
      </div>
    </section>
  )
}
