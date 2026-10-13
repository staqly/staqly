import { Globe } from 'lucide-react'

import type { Item } from '@/lib/api'
import { hostOf } from '@/lib/format'
import { cn } from '@/lib/utils'
import { isUntrusted, watcherMeta } from '@/lib/watchers'

/**
 * Subtle marker for items whose content was fetched from the internet by a
 * watcher (metadata.untrusted === true). Renders nothing for other items.
 */
export function ExternalContentBadge({
  item,
  className,
  compact = false,
}: {
  item: Pick<Item, 'metadata'>
  className?: string
  /** Icon only (tight list rows). */
  compact?: boolean
}) {
  if (!isUntrusted(item)) {
    return null
  }

  const w = watcherMeta(item)
  const from = w?.source_url ? hostOf(w.source_url) : undefined
  const title = `External content${w?.name ? ` from watcher “${w.name}”` : ''}${from ? ` (${from})` : ''}. Treat its text and links as untrusted.`

  return (
    <span
      data-testid="external-badge"
      title={title}
      aria-label={compact ? 'External content' : undefined}
      className={cn(
        'inline-flex h-5 shrink-0 items-center gap-1 rounded-md bg-sky-500/8 px-1.5 text-[11px] font-medium whitespace-nowrap text-sky-700 ring-1 ring-sky-500/20 ring-inset dark:text-sky-300',
        compact && 'px-1',
        className,
      )}
    >
      <Globe className="size-3" aria-hidden />
      {!compact && 'External content'}
    </span>
  )
}
