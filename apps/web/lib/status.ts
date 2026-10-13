import type { ItemStatus } from './api'

interface StatusMeta {
  label: string
  /** Badge classes (bg/text/ring) — light & dark aware. */
  badge: string
  /** Solid dot color. */
  dot: string
  description: string
}

export const STATUS_META: Record<ItemStatus, StatusMeta> = {
  draft: {
    label: 'Draft',
    badge: 'bg-zinc-500/10 text-zinc-600 ring-zinc-500/20 dark:text-zinc-300',
    dot: 'bg-zinc-400',
    description: 'Not ready for review',
  },
  pending: {
    label: 'Pending',
    badge: 'bg-amber-500/12 text-amber-700 ring-amber-500/25 dark:text-amber-300',
    dot: 'bg-amber-500',
    description: 'Waiting for review',
  },
  approved: {
    label: 'Approved',
    badge: 'bg-teal-500/12 text-teal-700 ring-teal-500/25 dark:text-teal-300',
    dot: 'bg-teal-500',
    description: 'Approved by a reviewer',
  },
  rejected: {
    label: 'Rejected',
    badge: 'bg-rose-500/12 text-rose-700 ring-rose-500/25 dark:text-rose-300',
    dot: 'bg-rose-500',
    description: 'Rejected by a reviewer',
  },
  expired: {
    label: 'Expired',
    badge: 'bg-stone-500/12 text-stone-600 ring-stone-500/25 dark:text-stone-300',
    dot: 'bg-stone-400',
    description: 'Nobody reviewed it in time',
  },
  processing: {
    label: 'Processing',
    badge: 'bg-sky-500/12 text-sky-700 ring-sky-500/25 dark:text-sky-300',
    dot: 'bg-sky-500',
    description: 'An agent is working on it',
  },
  completed: {
    label: 'Completed',
    badge: 'bg-violet-500/12 text-violet-700 ring-violet-500/25 dark:text-violet-300',
    dot: 'bg-violet-500',
    description: 'Downstream work finished',
  },
  failed: {
    label: 'Failed',
    badge: 'bg-orange-500/12 text-orange-700 ring-orange-500/30 dark:text-orange-300',
    dot: 'bg-orange-500',
    description: 'Downstream work failed',
  },
  archived: {
    label: 'Archived',
    badge: 'bg-stone-500/10 text-stone-500 ring-stone-500/20 dark:text-stone-400',
    dot: 'bg-stone-400',
    description: 'Kept for reference',
  },
}

export function statusMeta(status: string): StatusMeta {
  return (
    STATUS_META[status as ItemStatus] ?? {
      label: status,
      badge: 'bg-muted text-muted-foreground ring-border',
      dot: 'bg-muted-foreground',
      description: status,
    }
  )
}
