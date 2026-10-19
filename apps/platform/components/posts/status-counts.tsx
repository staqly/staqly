import type { PostStatus } from '@/lib/api'
import { POST_STATUS_META } from '@/lib/status'
import { cn } from '@/lib/utils'

const COUNT_ORDER: PostStatus[] = ['scheduled', 'publishing', 'failed', 'draft', 'published']

/** "3 scheduled · 1 failed · 12 published": a collection's posts by status, as dots. */
export function StatusCounts({ counts, className }: { counts: Record<string, number>; className?: string }) {
  const entries = COUNT_ORDER.filter((s) => (counts[s] ?? 0) > 0)

  if (!entries.length) {
    return <span className={cn('text-xs text-muted-foreground', className)}>No posts yet</span>
  }

  return (
    <div className={cn('flex flex-wrap gap-x-3 gap-y-1 text-xs', className)}>
      {entries.map((s) => (
        <span
          key={s}
          className="inline-flex items-center gap-1.5 text-muted-foreground"
          title={POST_STATUS_META[s].label}
        >
          <span className={cn('size-1.5 rounded-full', POST_STATUS_META[s].dot)} />
          <span className="text-foreground tabular-nums">{counts[s]}</span> {POST_STATUS_META[s].label.toLowerCase()}
        </span>
      ))}
    </div>
  )
}
