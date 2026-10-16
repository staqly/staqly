import { Archive, LoaderCircle } from 'lucide-react'

import type { Item } from '@/lib/api'
import { useCollections } from '@/lib/queries'
import { CANCELED, isCanceled, statusMeta } from '@/lib/status'
import { TONES } from '@/lib/tones'
import { cn } from '@/lib/utils'

const PILL =
  'inline-flex h-5 shrink-0 items-center gap-1 rounded-full pr-2 pl-1.5 text-[11px] font-medium whitespace-nowrap ring-1 ring-inset'

/**
 * One status as a badge, one look for all: a tinted pill with its icon, coloured by the status's tone (lib/tones.ts).
 * `live` spins the processing icon for an item that is processing right now (history shows a static clock).
 */
export function StatusBadge({
  status,
  live,
  label,
  title,
  className,
}: {
  status: string
  live?: boolean
  /** Tooltip; defaults to the status description. */
  title?: string
  /** Overrides the status name while a run is in progress: Publishing, Regenerating. */
  label?: string
  className?: string
}) {
  if (status === 'archived') {
    return <ArchivedBadge className={className} title={title} />
  }

  const m = status === 'canceled' ? CANCELED : statusMeta(status)
  const spin = live && status === 'processing'
  const Icon = spin ? LoaderCircle : m.Icon

  return (
    <span data-status={status} title={title || m.description} className={cn(PILL, m.badge, className)}>
      <Icon aria-hidden className={cn('size-3', spin && 'animate-spin')} />
      {label || m.label}
    </span>
  )
}

export function ArchivedBadge({ className, title }: { className?: string; title?: string }) {
  return (
    <span
      data-status="archived"
      title={title || 'Archived: kept for reference'}
      className={cn(PILL, TONES.neutral.badge, className)}
    >
      <Archive aria-hidden className="size-3" />
      Archived
    </span>
  )
}

/**
 * An item's state as ONE badge, the thing that matters now: Archived ends everything; a run in progress or
 * failed comes next (named by its action: Publishing, Publish failed); then Pending (still waiting for review,
 * also after a regeneration finished); then a finished run (Published); then the decision. The tooltip and the
 * Decision / Run properties keep the full story.
 */
export function ItemState({
  item,
  className,
}: {
  item: Pick<Item, 'status' | 'decision' | 'run' | 'archived_at' | 'run_action' | 'collection_id'> & {
    metadata?: unknown
  }
  className?: string
}) {
  const decision = item.decision ?? (item.status === 'draft' ? 'draft' : null)
  const run = item.run && item.run !== 'none' ? item.run : null
  const story = [decision && statusMeta(decision).label, run && statusMeta(run).label, item.archived_at && 'Archived']
    .filter(Boolean)
    .join(' → ')
  // A finished regeneration of a pending item puts it back in review: Pending is what matters then.
  const showRun = run && !(run === 'completed' && decision === 'pending')

  return (
    <span className={cn('inline-flex items-center', className)} data-testid="item-state">
      {item.archived_at ? (
        <ArchivedBadge title={story} />
      ) : showRun ? (
        <RunBadge item={item} title={story} />
      ) : decision ? (
        <StatusBadge status={decision} title={story} />
      ) : null}
    </span>
  )
}

/** The decision alone (a draft shows Draft), for a Decision column or row. */
export function DecisionState({ item }: { item: Pick<Item, 'status' | 'decision'> }) {
  const decision = item.decision ?? (item.status === 'draft' ? 'draft' : null)

  return decision ? <StatusBadge status={decision} /> : null
}

/** The run alone, or a dash when nothing ran, for a Run column or row. */
export function RunState({
  item,
}: {
  item: Pick<Item, 'run' | 'run_action' | 'collection_id'> & { metadata?: unknown }
}) {
  return item.run && item.run !== 'none' ? <RunBadge item={item} /> : <span className="text-muted-foreground">—</span>
}

/** The run, named by the action that started it while it runs (Publishing, Regenerating); else Completed, Failed. */
function RunBadge({
  item,
  title,
}: {
  item: Pick<Item, 'run' | 'run_action' | 'collection_id'> & { metadata?: unknown }
  title?: string
}) {
  // Selects the one label, so refetched collections (new pending counts) don't re-render every row's badge.
  const running = useCollections(true, (cols) =>
    item.run === 'processing' && item.run_action
      ? cols.find((c) => c.id === item.collection_id)?.actions?.find((a) => a.name === item.run_action)?.run_labels
          ?.running
      : undefined,
  ).data

  return <StatusBadge status={isCanceled(item) ? 'canceled' : item.run} live title={title} label={running} />
}
