import type { PostStatus } from '@/lib/api'
import { POST_STATUS_META } from '@/lib/status'
import { TONES } from '@/lib/tones'
import { cn } from '@/lib/utils'

const COUNT_ORDER: PostStatus[] = ['scheduled', 'publishing', 'failed', 'draft', 'published']

/** A collection's posts by status, in a card of its own under the title: one cell per status it has. */
export function StatusSummary({ counts, className }: { counts: Record<string, number>; className?: string }) {
  const entries = COUNT_ORDER.filter((s) => (counts[s] ?? 0) > 0)

  return (
    <dl
      className={cn(
        'flex flex-wrap divide-x rounded-xl border bg-card text-card-foreground [&>div]:min-w-28 [&>div]:flex-1',
        className,
      )}
      data-testid="status-summary"
    >
      {entries.map((s) => {
        const meta = POST_STATUS_META[s]

        return (
          <div key={s} className="grid gap-0.5 px-4 py-3">
            <dt className="flex items-center gap-1.5 text-xs text-muted-foreground">
              <meta.Icon aria-hidden className={cn('size-3.5', TONES[meta.tone].text)} />
              {meta.label}
            </dt>
            <dd className="text-xl font-semibold tabular-nums">{counts[s]}</dd>
          </div>
        )
      })}
    </dl>
  )
}
