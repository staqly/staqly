'use client'

import { ArrowRight } from 'lucide-react'
import { useRef, type KeyboardEvent } from 'react'

import { useKeepInView } from '@/hooks/use-keep-in-view'
import type { Flow, Item, ItemQuery } from '@/lib/api'
import { statusMeta } from '@/lib/status'
import { TONES } from '@/lib/tones'
import { cn } from '@/lib/utils'

type State = Flow['states'][number]

export interface Step {
  id: StepId
  label: string
  /** STATUS_META key for the icon and colour. */
  status: string
  query: Pick<ItemQuery, 'decision' | 'run'>
  /** Counts the step's items from /v1/flow. */
  count: (s: State) => boolean
  /** Whether an item (after an action) still belongs here. */
  holds: (i: Pick<Item, 'decision' | 'run' | 'archived_at'>) => boolean
  empty: { title: string; description: string }
}

export type StepId = 'review' | 'approved' | 'processing' | 'done' | 'failed' | 'rejected' | 'expired' | 'archived'

const OPEN = 'pending,approved,rejected,expired'
const live = (i: Pick<Item, 'archived_at'>) => !i.archived_at

/**
 * Where an item is in its life, as the inbox's tabs: review, then approved, processing, and done or failed;
 * rejected and archived on the side. Each step is one query; together they cover every item once.
 */
export const STEPS: Record<StepId, Step> = {
  review: {
    id: 'review',
    label: 'Review',
    status: 'pending',
    query: { decision: 'pending', run: 'none,completed' },
    count: (s) => !s.archived && s.decision === 'pending' && (s.run === 'none' || s.run === 'completed'),
    holds: (i) => live(i) && i.decision === 'pending' && i.run !== 'processing' && i.run !== 'failed',
    empty: { title: 'Inbox zero', description: 'Nothing is waiting for review. New items from your agents land here.' },
  },
  approved: {
    id: 'approved',
    label: 'Approved',
    status: 'approved',
    query: { decision: 'approved', run: 'none' },
    count: (s) => !s.archived && s.decision === 'approved' && s.run === 'none',
    holds: (i) => live(i) && i.decision === 'approved' && i.run === 'none',
    empty: {
      title: 'Nothing approved and waiting',
      description: 'Approved items wait here until an action (Publish...) starts your automation.',
    },
  },
  processing: {
    id: 'processing',
    label: 'Processing',
    status: 'processing',
    query: { decision: OPEN, run: 'processing' },
    count: (s) => !s.archived && s.run === 'processing',
    holds: (i) => live(i) && i.run === 'processing',
    empty: { title: 'Nothing running', description: 'Items move here while your automation works on them.' },
  },
  done: {
    id: 'done',
    label: 'Completed',
    status: 'completed',
    query: { decision: 'approved,rejected,expired', run: 'completed' },
    count: (s) => !s.archived && s.run === 'completed' && s.decision !== 'pending',
    holds: (i) => live(i) && i.run === 'completed' && i.decision !== 'pending',
    empty: { title: 'Nothing completed yet', description: 'Runs that finished (published, regenerated) show up here.' },
  },
  failed: {
    id: 'failed',
    label: 'Failed',
    status: 'failed',
    query: { decision: OPEN, run: 'failed' },
    count: (s) => !s.archived && s.run === 'failed',
    holds: (i) => live(i) && i.run === 'failed',
    empty: { title: 'No failures', description: 'Runs that failed or timed out land here, ready to retry.' },
  },
  rejected: {
    id: 'rejected',
    label: 'Rejected',
    status: 'rejected',
    query: { decision: 'rejected', run: 'none' },
    count: (s) => !s.archived && s.decision === 'rejected' && s.run === 'none',
    holds: (i) => live(i) && i.decision === 'rejected' && i.run === 'none',
    empty: { title: 'Nothing rejected', description: 'Rejected items show up here.' },
  },
  expired: {
    id: 'expired',
    label: 'Expired',
    status: 'expired',
    query: { decision: 'expired', run: 'none' },
    count: (s) => !s.archived && s.decision === 'expired' && s.run === 'none',
    holds: (i) => live(i) && i.decision === 'expired' && i.run === 'none',
    empty: { title: 'Nothing expired', description: 'Items nobody decided before their expiry show up here.' },
  },
  archived: {
    id: 'archived',
    label: 'Archived',
    status: 'archived',
    query: { decision: 'archived' },
    count: (s) => s.archived,
    holds: (i) => !!i.archived_at,
    empty: { title: 'Nothing archived', description: 'Items you archive are kept here for reference.' },
  },
}

/** Grouped as items move: waiting, decided (approved or rejected), running, result (completed or failed), set aside. */
const GROUPS: StepId[][] = [
  ['review'],
  ['approved', 'rejected'],
  ['processing'],
  ['done', 'failed'],
  ['expired', 'archived'],
]

export function stepOf(v: string | null): Step {
  // Own keys only: ?step=constructor must not reach Object.prototype.
  return v && Object.hasOwn(STEPS, v) ? STEPS[v as StepId] : STEPS.review
}

/** Links from before the steps (?decision=approved) open the matching step. */
export function legacyStep(decision: string | null): StepId | null {
  const first = decision?.split(',')[0]

  return first === 'approved' || first === 'archived'
    ? first
    : first === 'rejected' || first === 'expired'
      ? 'rejected'
      : null
}

/**
 * The inbox's tabs: one per step, with its count, in the order items move. Counts are left out while other
 * filters narrow the list (the flow counts cover a whole collection).
 */
export function Stepper({
  active,
  counts,
  onPick,
}: {
  active: StepId
  counts: Partial<Record<StepId, number>> | null
  onPick: (s: StepId) => void
}) {
  const list = useRef<HTMLDivElement>(null)

  // The open step stays visible where the toolbar scrolls sideways (tablets).
  useKeepInView(list, `[data-testid="step-${active}"]`)

  // The tabs pattern: one Tab stop (the open step), arrows, Home and End move between steps and open them.
  function onKeyDown(e: KeyboardEvent) {
    const order = GROUPS.flat()
    const i = order.indexOf(active)
    const next =
      e.key === 'ArrowRight'
        ? order[(i + 1) % order.length]
        : e.key === 'ArrowLeft'
          ? order[(i - 1 + order.length) % order.length]
          : e.key === 'Home'
            ? order[0]
            : e.key === 'End'
              ? order[order.length - 1]
              : undefined

    if (!next || e.metaKey || e.ctrlKey || e.altKey) {
      return
    }

    e.preventDefault()
    onPick(next)
    list.current?.querySelector<HTMLElement>(`[data-testid="step-${next}"]`)?.focus()
  }

  const tab = (id: StepId) => {
    const n = counts?.[id]
    const on = active === id

    return (
      <button
        key={id}
        type="button"
        role="tab"
        aria-selected={on}
        tabIndex={on ? 0 : -1}
        onClick={() => onPick(id)}
        onKeyDown={onKeyDown}
        data-testid={`step-${id}`}
        className={cn(
          'relative flex h-12 items-center gap-1.5 px-2 text-sm whitespace-nowrap transition-colors',
          on ? 'font-medium text-foreground' : 'text-muted-foreground hover:text-foreground',
        )}
      >
        {STEPS[id].label}
        {n !== undefined && n > 0 && (
          <span
            className={cn(
              'rounded-full px-1.5 text-[11px] tabular-nums',
              // The step's own status colour: amber to review, green approved and done, blue processing, red failed.
              TONES[statusMeta(STEPS[id].status).tone].badge,
            )}
          >
            {n}
          </span>
        )}
        {/* Underlined in the step's colour, like the active row's edge. */}
        {on && <span aria-hidden className={cn('absolute inset-x-0.5 bottom-0 h-0.5 rounded-full', stepAccent(id))} />}
      </button>
    )
  }

  return (
    // Groups follow the flow: an arrow between steps that follow each other, a gap before the side step (archive).
    <div ref={list} className="flex items-center" role="tablist" aria-label="Steps">
      {GROUPS.map((group, i) => (
        <div key={group.join()} className="flex items-center">
          {i > 0 &&
            (group[0] === 'archived' ? (
              <span aria-hidden className="mx-2 h-4 w-px bg-border" />
            ) : (
              <ArrowRight aria-hidden strokeWidth={1.5} className="mx-1.5 size-3.5 text-muted-foreground/60" />
            ))}
          {group.map(tab)}
        </div>
      ))}
    </div>
  )
}

/** The step's status colour, as a solid background (the active row's edge). */
export function stepAccent(id: StepId) {
  return TONES[statusMeta(STEPS[id].status).tone].dot
}

/** Step counts from /v1/flow. */
export function stepCounts(states: Flow['states']): Record<StepId, number> {
  return Object.fromEntries(
    Object.values(STEPS).map((s) => [s.id, states.filter(s.count).reduce((n, st) => n + st.items, 0)]),
  ) as Record<StepId, number>
}
