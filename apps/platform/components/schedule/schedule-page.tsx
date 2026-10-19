'use client'

import {
  CalendarClock,
  CalendarDays,
  CheckCircle2,
  ChevronLeft,
  ChevronRight,
  Clock,
  List,
  LoaderCircle,
  Plus,
  TriangleAlert,
} from 'lucide-react'
import dynamic from 'next/dynamic'
import { usePathname, useRouter, useSearchParams } from 'next/navigation'
import { memo, useCallback, useEffect, useLayoutEffect, useMemo, useRef, useState } from 'react'

import { ChannelAvatar } from '@/components/channels/channel-avatars'
import { ErrorState } from '@/components/common/error-state'
import { Button } from '@/components/ui/button'
import { ButtonGroup } from '@/components/ui/button-group'
import { Skeleton } from '@/components/ui/skeleton'
import { ToggleGroup, ToggleGroupItem } from '@/components/ui/toggle-group'
import { useIsCoarsePointer, useIsMobile } from '@/hooks/use-mobile'
import { useNow } from '@/hooks/use-now'
import type { Collection, Post, PostTarget } from '@/lib/api'
import { pluralize } from '@/lib/format'
import { postTitle } from '@/lib/posts'
import { useCollections, usePostsInfinite, useRefetchOnForeignChange, useSchedules } from '@/lib/queries'
import { safeCssColor } from '@/lib/safe'
import {
  type CalendarView,
  SCHEDULE_LABELS,
  type ScheduleState,
  canReschedule,
  dayFromKey,
  dayKey,
  formatHour,
  formatTime,
  minutesOfDay,
  moveToDay,
  moveToHour,
  nextFullHour,
  rangeOf,
  roundUpMinutes,
  type ScheduleSlot,
  scheduleSlots,
  scheduleState,
} from '@/lib/schedule'
import { TONES, type Tone } from '@/lib/tones'
import { cn } from '@/lib/utils'

import { useSetSchedule } from './use-schedule'

// The post panel loads when an entry is opened, not with the calendar; the slot dialog ("+" on a future day
// or hour) when it is first used.
const PostSheet = dynamic(() => import('@/components/posts/post-sheet').then((m) => m.PostSheet), { ssr: false })
const SlotScheduleDialog = dynamic(() => import('./slot-schedule-dialog').then((m) => m.SlotScheduleDialog), {
  ssr: false,
})

type Layout = 'grid' | 'list'

const STATE_TONE: Record<ScheduleState, Tone> = {
  scheduled: 'success',
  publishing: 'info',
  published: 'neutral',
  failed: 'danger',
}

/** The state of an entry, carried by an icon and a label in its tone; the bar beside it is the collection colour. */
const STATE_ICON: Record<ScheduleState, typeof Clock> = {
  scheduled: Clock,
  publishing: LoaderCircle,
  published: CheckCircle2,
  failed: TriangleAlert,
}

const HOURS = Array.from({ length: 24 }, (_, h) => h)
const NO_POSTS: Post[] = []
const NO_SLOTS: ScheduleSlot[] = []
const DRAG_TYPE = 'application/x-staqly-post'
/** Height of one hour on the grid, in px (h-20), and of the sticky day header row (h-16). */
const ROW_PX = 80
const HEADER_PX = 64
/** Past days and hours: a muted fill with a diagonal hatch; the tint is a share of the foreground, so it works in both themes. */
const HATCH_LINES =
  'bg-[repeating-linear-gradient(135deg,transparent_0_6px,color-mix(in_oklch,var(--foreground)_10%,transparent)_6px_7px)]'
const HATCH = `bg-muted/50 ${HATCH_LINES}`

/** The cards a post gets: one per channel, or one for a post without channels. */
function cardsOf(post: Post): (PostTarget | undefined)[] {
  return post.targets?.length ? post.targets : [undefined]
}

/** One post on the calendar: time, first line, channels, collection and what it is doing now. Draggable until it goes out. */
const Entry = memo(function Entry({
  post,
  target,
  collection,
  variant = 'cell',
  onOpen,
}: {
  post: Post
  /** The channel this card stands for: a post with several channels is one card per channel. */
  target?: PostTarget
  collection?: Collection
  /** `cell`: a compact card in an hour or day cell; `row`: a full row of the list view. */
  variant?: 'cell' | 'row'
  onOpen: (id: string, channel?: string) => void
}) {
  const state: ScheduleState =
    target && target.status !== 'pending' ? target.status : (scheduleState(post) ?? 'scheduled')
  const tone = TONES[STATE_TONE[state]]
  const StateIcon = STATE_ICON[state]
  const draggable = variant === 'cell' && canReschedule(post)
  const color = safeCssColor(collection?.color)
  const time = formatTime(new Date(post.publish_at!))
  const muted = state === 'published'
  const title = postTitle(post)
  const channel = target && {
    id: target.channel_id,
    provider: target.provider,
    name: target.channel_name,
    avatar_url: target.avatar_url,
  }
  const where = channel ? ` on ${channel.name}` : ''

  return (
    <button
      type="button"
      draggable={draggable}
      data-testid="schedule-entry"
      data-post-id={post.id}
      data-channel-id={target?.channel_id}
      data-state={state}
      aria-label={`${time} ${title}${where}: ${SCHEDULE_LABELS[state]}${collection ? `, ${collection.name}` : ''}`}
      title={`${title}${where}: ${SCHEDULE_LABELS[state]}, ${time}${collection ? `, ${collection.name}` : ''}${draggable ? '. Drag to another day or hour to reschedule (every channel of the post moves).' : ''}`}
      onClick={() => onOpen(post.id, target?.channel_id)}
      onDragStart={(e) => {
        e.dataTransfer.setData(DRAG_TYPE, post.id)
        e.dataTransfer.effectAllowed = 'move'
      }}
      className={cn(
        'flex w-full min-w-0 items-start gap-2 rounded-md border border-l-[3px] border-l-muted-foreground/40 bg-card text-left text-xs leading-tight shadow-xs transition-colors hover:bg-muted',
        variant === 'cell' ? 'min-h-10 px-2 py-1.5' : 'min-h-11 items-center px-3 py-2 text-sm',
        muted && 'opacity-70',
        draggable ? 'cursor-grab active:cursor-grabbing' : 'cursor-pointer',
      )}
      style={color ? { borderLeftColor: color } : undefined}
    >
      <span className="min-w-0 flex-1">
        <span className={cn('flex items-baseline gap-1.5', variant === 'row' && 'gap-2')}>
          <span className="shrink-0 font-medium text-foreground tabular-nums">{time}</span>
          <span className="truncate font-medium text-foreground">{title}</span>
          {channel && <ChannelAvatar channel={channel} className="ml-auto size-4 shrink-0" />}
        </span>
        <span className="mt-0.5 flex min-w-0 items-center gap-1 text-[11px] text-muted-foreground">
          <StateIcon
            aria-hidden
            className={cn('size-3 shrink-0', tone.text, state === 'publishing' && 'animate-spin')}
          />
          <span className={cn('truncate', tone.text)}>{SCHEDULE_LABELS[state]}</span>
          {collection && (
            <>
              <span aria-hidden>·</span>
              <span className="truncate">{collection.name}</span>
            </>
          )}
        </span>
      </span>
    </button>
  )
})

/** An upcoming run of a collection schedule: a hollow chip, so the queue is visible next to the scheduled posts. */
function SlotChip({
  slot,
  collection,
  variant = 'cell',
}: {
  slot: ScheduleSlot
  collection?: Collection
  variant?: 'cell' | 'row'
}) {
  const color = safeCssColor(collection?.color)
  const time = formatTime(slot.at)
  const what =
    slot.schedule.pick === 'next' ? 'next post' : slot.schedule.pick === 'all' ? 'waiting posts' : 'chosen posts'

  return (
    <span
      data-testid="schedule-slot-chip"
      title={`${slot.schedule.name}: publishes the ${what} from ${collection?.name ?? 'the collection'} at ${time}`}
      className={cn(
        'flex w-full min-w-0 items-center gap-1.5 rounded-md border border-dashed border-l-[3px] border-l-muted-foreground/40 bg-background/60 text-left text-[11px] leading-tight text-muted-foreground',
        variant === 'cell' ? 'min-h-7 px-2 py-1' : 'min-h-9 px-3 py-1.5 text-xs',
      )}
      style={color ? { borderLeftColor: color } : undefined}
    >
      <span className="shrink-0 font-medium tabular-nums">{time}</span>
      <span className="truncate">
        {slot.schedule.name} · {what}
      </span>
    </span>
  )
}

/** A drop target for entries; a slot in the past is not one (the cursor says not-allowed, a drop does nothing). */
function useDrop(onDrop: (postId: string) => void, enabled = true) {
  const [over, setOver] = useState(false)

  return {
    over: over && enabled,
    props: {
      onDragOver: (e: React.DragEvent) => {
        if (!e.dataTransfer.types.includes(DRAG_TYPE)) {
          return
        }

        if (!enabled) {
          e.dataTransfer.dropEffect = 'none'

          return
        }

        e.preventDefault()
        e.dataTransfer.dropEffect = 'move'
        setOver(true)
      },
      onDragLeave: () => setOver(false),
      onDrop: (e: React.DragEvent) => {
        e.preventDefault()
        setOver(false)

        if (!enabled) {
          return
        }

        const id = e.dataTransfer.getData(DRAG_TYPE)

        if (id) {
          onDrop(id)
        }
      },
    },
  }
}

/**
 * A drop target: an hour of the day/week grid or a day of the month grid. A future slot also offers "+":
 * schedule a post for that time (shown on hover or focus, always on touch screens).
 */
function Slot({
  className,
  children,
  onDrop,
  onAdd,
  addLabel,
  testId,
  past,
  addAt = 'top',
}: {
  className?: string
  children?: React.ReactNode
  onDrop: (postId: string) => void
  onAdd?: () => void
  addLabel?: string
  testId: string
  /** Before now: no "+", no drop. */
  past?: boolean
  /** Where "+" sits: top right (month days, beside the date) or bottom right (hour cells, under the cards). */
  addAt?: 'top' | 'bottom'
}) {
  const drop = useDrop(onDrop, !past)

  return (
    // eslint-disable-next-line jsx-a11y/click-events-have-key-events, jsx-a11y/no-static-element-interactions -- keyboards use the slot-add button inside
    <div
      {...drop.props}
      data-testid={testId}
      data-past={past || undefined}
      onClick={(e) => {
        // A click on the cell's empty space (not on a card or a button inside it) adds a post there.
        if (onAdd && e.target === e.currentTarget) {
          onAdd()
        }
      }}
      className={cn(
        'group/slot relative *:relative *:z-[1]',
        onAdd && 'cursor-copy',
        className,
        drop.over && 'bg-brand-soft ring-2 ring-brand ring-inset',
      )}
    >
      {children}
      {onAdd && (
        <>
          {/* The cell itself is the "+": a click on its empty space adds a post (cards stay theirs). A dashed
              outline and a quiet plus show on hover; keyboards get a button that appears on focus. */}
          <span
            aria-hidden
            className={cn(
              'pointer-events-none !absolute inset-0.5 !z-0 flex justify-end rounded-md border border-dashed border-transparent p-1 text-muted-foreground transition-colors group-hover/slot:border-border',
              addAt === 'top' ? 'items-start' : 'items-end',
            )}
          >
            <Plus className="size-4 opacity-0 transition-opacity group-hover/slot:opacity-100" />
          </span>
          <button
            type="button"
            onClick={onAdd}
            aria-label={addLabel ?? 'Schedule a post here'}
            data-testid="slot-add"
            className={cn(
              'sr-only focus-visible:not-sr-only focus-visible:!absolute focus-visible:right-1 focus-visible:!z-10 focus-visible:flex focus-visible:size-7 focus-visible:items-center focus-visible:justify-center focus-visible:rounded-full focus-visible:bg-primary focus-visible:text-primary-foreground',
              addAt === 'top' ? 'focus-visible:top-1' : 'focus-visible:bottom-1',
            )}
          >
            <Plus className="size-4" />
          </button>
        </>
      )}
    </div>
  )
}

/** The weekday over the date, as a card; today in the accent colour with a dot. */
function DayHeader({ day, today, className }: { day: Date; today: boolean; className?: string }) {
  return (
    <div
      className={cn('flex h-14 flex-col items-center justify-center gap-0.5 text-xs text-muted-foreground', className)}
    >
      <span>{day.toLocaleDateString(undefined, { weekday: 'short' })}</span>
      <span
        className={cn(
          'flex size-7 items-center justify-center rounded-full text-sm font-semibold tabular-nums',
          today ? 'bg-primary text-primary-foreground' : 'text-foreground',
        )}
      >
        {day.getDate()}
      </span>
    </div>
  )
}

/** "Oct 12 to 18, 2026" when both ends share the month, else "Sep 28 to Oct 4, 2026" (years named when they differ). */
export function rangeTitle(first: Date, last: Date) {
  const sameYear = first.getFullYear() === last.getFullYear()
  const sameMonth = sameYear && first.getMonth() === last.getMonth()
  const year = last.toLocaleDateString(undefined, { year: 'numeric' })

  if (sameMonth) {
    return `${first.toLocaleDateString(undefined, { month: 'short', day: 'numeric' })} to ${last.getDate()}, ${year}`
  }

  if (sameYear) {
    return `${first.toLocaleDateString(undefined, { month: 'short', day: 'numeric' })} to ${last.toLocaleDateString(undefined, { month: 'short', day: 'numeric' })}, ${year}`
  }

  return `${first.toLocaleDateString(undefined, { month: 'short', day: 'numeric', year: 'numeric' })} to ${last.toLocaleDateString(undefined, { month: 'short', day: 'numeric', year: 'numeric' })}`
}

function dayLabel(d: Date) {
  return d.toLocaleDateString(undefined, { weekday: 'long', day: 'numeric', month: 'long' })
}

export function SchedulePage() {
  const params = useSearchParams()
  const router = useRouter()
  const pathname = usePathname()
  const isMobile = useIsMobile()
  const now = useNow(true, 60_000)
  const today = dayKey(new Date(now))
  const view: CalendarView = params.get('view') === 'month' ? 'month' : params.get('view') === 'day' ? 'day' : 'week'
  const layout: Layout = isMobile || params.get('list') === '1' ? 'list' : 'grid'
  const anchor = useMemo(() => {
    const at = params.get('at')

    return dayFromKey(at && /^\d{4}-\d{2}-\d{2}$/.test(at) ? at : today)
  }, [params, today])
  const range = useMemo(() => rangeOf(view, anchor), [view, anchor])
  const query = useMemo(
    () => ({
      publish_after: range.publish_after,
      publish_before: range.publish_before,
      sort: 'publish_asc' as const,
      limit: 200,
    }),
    [range.publish_after, range.publish_before],
  )
  const list = usePostsInfinite(query, { refetchInterval: 30_000 })

  // Published by the queue, scheduled by an agent or a slot: refetch when someone else changes a post.
  useRefetchOnForeignChange(list.refetch)
  const { hasNextPage, isFetchingNextPage, fetchNextPage } = list

  // The whole range at once: a calendar cannot show "load more".
  useEffect(() => {
    if (hasNextPage && !isFetchingNextPage) {
      void fetchNextPage()
    }
  }, [hasNextPage, isFetchingNextPage, fetchNextPage])

  const items = useMemo(() => list.data?.pages.flatMap((p) => p.data) ?? NO_POSTS, [list.data])
  const byDay = useMemo(() => {
    const m = new Map<string, Post[]>()

    for (const it of items) {
      if (!it.publish_at) {
        continue
      }

      const k = dayKey(new Date(it.publish_at))

      m.set(k, [...(m.get(k) ?? []), it])
    }

    return m
  }, [items])
  const collections = useCollections(true)
  const collectionOf = useCallback((id: string) => collections.data?.find((c) => c.id === id), [collections.data])
  const schedules = useSchedules()
  const slotsByDay = useMemo(() => {
    const m = new Map<string, ScheduleSlot[]>()

    for (const slot of scheduleSlots(schedules.data, range.days, now)) {
      const k = dayKey(slot.at)

      m.set(k, [...(m.get(k) ?? []), slot])
    }

    return m
  }, [schedules.data, range.days, now])
  const scheduledCount = useMemo(() => items.filter((it) => scheduleState(it) === 'scheduled').length, [items])
  const { mutate: reschedule } = useSetSchedule()
  const [opened, setOpened] = useState<{ id: string; channel?: string } | null>(null)
  const open = useCallback((id: string, channel?: string) => setOpened({ id, channel }), [])
  // "+" on a future slot: the time to schedule for (an hour of the grid, or 09:00 / the next full hour of a day).
  const [slot, setSlot] = useState<Date | null>(null)
  const addAtHour = useCallback((day: string, hour: number) => {
    const d = dayFromKey(day)
    const current = new Date()

    d.setHours(hour, 0, 0, 0)

    if (d.getTime() <= current.getTime()) {
      // The hour that is running now: the next five-minute mark, never a time that has passed.
      setSlot(roundUpMinutes(current, 5))

      return
    }

    setSlot(d)
  }, [])
  const addOnDay = useCallback((day: string) => {
    const d = dayFromKey(day)
    const current = new Date()

    d.setHours(9, 0, 0, 0)
    setSlot(d.getTime() > current.getTime() ? d : nextFullHour(current.getTime()))
  }, [])

  const navigate = useCallback(
    (next: { view?: CalendarView; at?: Date; layout?: Layout }) => {
      const p = new URLSearchParams(params.toString())
      const v = next.view ?? view

      if (v === 'week') {
        p.delete('view')
      } else {
        p.set('view', v)
      }

      if (next.at) {
        p.set('at', dayKey(next.at))
      }

      if (next.layout) {
        if (next.layout === 'list') {
          p.set('list', '1')
        } else {
          p.delete('list')
        }
      }

      const qs = p.toString()

      router.replace(qs ? `${pathname}?${qs}` : pathname, { scroll: false })
    },
    [params, pathname, router, view],
  )
  const step = (n: number) => {
    const d = new Date(anchor)

    if (view === 'day') {
      d.setDate(d.getDate() + n)
    } else if (view === 'week') {
      d.setDate(d.getDate() + 7 * n)
    } else {
      d.setDate(1)
      d.setMonth(d.getMonth() + n)
    }

    navigate({ at: d })
  }

  const dropOnDay = useCallback(
    (postId: string, day: string) => {
      const it = items.find((x) => x.id === postId)

      if (!it?.publish_at || !canReschedule(it)) {
        return
      }

      const at = moveToDay(it.publish_at, day)

      if (at !== it.publish_at && new Date(at).getTime() > Date.now()) {
        reschedule({ post: it, publishAt: at })
      }
    },
    [items, reschedule],
  )
  const dropOnHour = useCallback(
    (postId: string, day: string, hour: number) => {
      const it = items.find((x) => x.id === postId)

      if (!it?.publish_at || !canReschedule(it)) {
        return
      }

      const at = moveToHour(it.publish_at, day, hour)

      if (at !== it.publish_at && new Date(at).getTime() > Date.now()) {
        reschedule({ post: it, publishAt: at })
      }
    },
    [items, reschedule],
  )

  const first = range.days[0]
  const last = range.days[range.days.length - 1]
  const title =
    view === 'day'
      ? first.toLocaleDateString(undefined, { weekday: 'long', month: 'short', day: 'numeric', year: 'numeric' })
      : view === 'week'
        ? rangeTitle(first, last)
        : anchor.toLocaleDateString(undefined, { month: 'long', year: 'numeric' })
  const unit = view === 'day' ? 'day' : view === 'week' ? 'week' : 'month'
  const nowText = new Date(now).toLocaleTimeString(undefined, { hour: '2-digit', minute: '2-digit', hourCycle: 'h23' })

  const toolbar = (
    <div className="flex flex-wrap items-center gap-2" data-testid="schedule-toolbar">
      <ButtonGroup aria-label="Move the calendar">
        <Button
          variant="outline"
          size="icon"
          className="size-8 pointer-coarse:size-10"
          aria-label={`Previous ${unit}`}
          onClick={() => step(-1)}
        >
          <ChevronLeft />
        </Button>
        <Button
          variant="outline"
          size="sm"
          className="h-8 pointer-coarse:h-10"
          onClick={() => navigate({ at: new Date(now) })}
        >
          Today
        </Button>
        <Button
          variant="outline"
          size="icon"
          className="size-8 pointer-coarse:size-10"
          aria-label={`Next ${unit}`}
          onClick={() => step(1)}
        >
          <ChevronRight />
        </Button>
      </ButtonGroup>
      <h2
        className="min-w-0 truncate text-sm font-medium whitespace-nowrap"
        aria-live="polite"
        data-testid="schedule-range"
      >
        {title}
      </h2>
      <div className="flex items-center gap-2 sm:ml-2">
        <ToggleGroup
          type="single"
          variant="outline"
          spacing={0}
          value={view}
          onValueChange={(v) => v && navigate({ view: v as CalendarView })}
          aria-label="Calendar view"
          className="h-8 pointer-coarse:h-10"
        >
          {(['day', 'week', 'month'] as const).map((v) => (
            <ToggleGroupItem
              key={v}
              value={v}
              aria-label={v === 'day' ? 'Day' : v === 'week' ? 'Week' : 'Month'}
              className="h-8 bg-background px-2.5 text-[0.8rem] text-foreground pointer-coarse:h-10 data-[state=on]:bg-primary data-[state=on]:text-primary-foreground"
            >
              <span aria-hidden className="sm:hidden">
                {v[0].toUpperCase()}
              </span>
              <span aria-hidden className="max-sm:hidden">
                {v === 'day' ? 'Day' : v === 'week' ? 'Week' : 'Month'}
              </span>
            </ToggleGroupItem>
          ))}
        </ToggleGroup>
        <ToggleGroup
          type="single"
          variant="outline"
          spacing={0}
          value={layout}
          onValueChange={(v) => v && navigate({ layout: v as Layout })}
          aria-label="Layout"
          className="hidden h-8 pointer-coarse:h-10 md:flex"
        >
          <ToggleGroupItem
            value="grid"
            aria-label="Calendar"
            className="h-8 min-w-8 bg-background px-2 text-foreground pointer-coarse:h-10 data-[state=on]:bg-primary data-[state=on]:text-primary-foreground"
          >
            <CalendarDays />
          </ToggleGroupItem>
          <ToggleGroupItem
            value="list"
            aria-label="List"
            className="h-8 min-w-8 bg-background px-2 text-foreground pointer-coarse:h-10 data-[state=on]:bg-primary data-[state=on]:text-primary-foreground"
          >
            <List />
          </ToggleGroupItem>
        </ToggleGroup>
      </div>
    </div>
  )

  const header = (
    // The controls sit on the title's row, vertically centred, nav and range left of the view toggles.
    <div className="flex flex-wrap items-center justify-between gap-x-6 gap-y-3">
      <div>
        <h1 className="text-xl font-semibold tracking-tight">Schedule</h1>
        <p className="mt-0.5 text-sm text-muted-foreground">
          <span data-testid="schedule-count">{pluralize(scheduledCount, 'post')} scheduled</span>
          <span aria-hidden> · </span>
          <span>
            now <span className="tabular-nums">{nowText}</span>
          </span>
        </p>
      </div>
      {toolbar}
    </div>
  )

  const empty = !list.isLoading && items.length === 0
  // An empty range still shows the grid: its "+" is one way to fill it.
  const emptyState = (
    <div
      className="flex flex-wrap items-center gap-x-3 gap-y-1 rounded-xl border border-dashed px-3 py-2 text-sm"
      data-testid="schedule-empty"
    >
      <span className="inline-flex items-center gap-1.5 font-medium">
        <CalendarClock className="size-4 text-muted-foreground" aria-hidden /> Nothing scheduled this {unit}
      </span>
      <span className="text-muted-foreground">
        Click + on a future slot to schedule a draft or write a post for that time, or give a collection a schedule that
        publishes its drafts for you. Staqly sends every post at its time.
      </span>
    </div>
  )

  let body: React.ReactNode

  if (list.error) {
    body = <ErrorState error={list.error} onRetry={() => void list.refetch()} />
  } else if (list.isLoading) {
    body = <Skeleton className="flex-1 rounded-xl" />
  } else if (layout === 'list') {
    body = (
      <AgendaList
        days={range.days}
        byDay={byDay}
        slotsByDay={slotsByDay}
        today={today}
        collectionOf={collectionOf}
        onOpen={open}
      />
    )
  } else if (view === 'month') {
    body = (
      <MonthGrid
        days={range.days}
        month={anchor.getMonth()}
        byDay={byDay}
        slotsByDay={slotsByDay}
        today={today}
        collectionOf={collectionOf}
        onOpen={open}
        onDrop={dropOnDay}
        onAdd={addOnDay}
      />
    )
  } else {
    body = (
      <TimeGrid
        days={range.days}
        byDay={byDay}
        slotsByDay={slotsByDay}
        today={today}
        now={now}
        collectionOf={collectionOf}
        onOpen={open}
        onDrop={dropOnHour}
        onAdd={addAtHour}
      />
    )
  }

  return (
    // The toolbar keeps the page padding; the grid runs to the left, right and bottom edges.
    <div className={cn('flex min-h-0 flex-col', isMobile ? 'flex-1' : 'h-svh')} data-testid="schedule">
      <div className="grid gap-2 px-4 pt-4 pb-2 sm:px-6 lg:px-8">
        {header}
        {empty && emptyState}
      </div>
      {body}
      <PostSheet id={opened?.id ?? null} channel={opened?.channel} onClose={() => setOpened(null)} />
      {slot && <SlotScheduleDialog at={slot} onOpenChange={(o) => !o && setSlot(null)} />}
    </div>
  )
}

/**
 * The hour grid of the day and week views: sticky day headers, a sticky hour axis, 80 px rows, past hours
 * hatched, today's column tinted, a "now" line with a time pill. Scrolls so that now (or 08:00 on another
 * day) sits a third of the way down.
 */
function TimeGrid({
  days,
  byDay,
  slotsByDay,
  today,
  now,
  collectionOf,
  onOpen,
  onDrop,
  onAdd,
}: {
  days: Date[]
  byDay: Map<string, Post[]>
  slotsByDay: Map<string, ScheduleSlot[]>
  today: string
  now: number
  collectionOf: (id: string) => Collection | undefined
  onOpen: (id: string, channel?: string) => void
  onDrop: (postId: string, day: string, hour: number) => void
  onAdd: (day: string, hour: number) => void
}) {
  const scroller = useRef<HTMLDivElement>(null)
  const todayIndex = days.findIndex((d) => dayKey(d) === today)
  const nowMinutes = minutesOfDay(new Date(now))
  const nowY = HEADER_PX + (nowMinutes / 60) * ROW_PX

  // On load and on another range: now (today on show) or 08:00 a third of the way down the viewport.
  useLayoutEffect(() => {
    const el = scroller.current

    if (!el) {
      return
    }

    const current = new Date()
    const hasToday = days.some((d) => dayKey(d) === dayKey(current))
    const target = hasToday ? HEADER_PX + (minutesOfDay(current) / 60) * ROW_PX : HEADER_PX + 8 * ROW_PX

    el.scrollTop = Math.max(0, target - el.clientHeight / 3)
  }, [days])

  const cols = days.length
  const nowTime = new Date(now).toLocaleTimeString(undefined, { hour: '2-digit', minute: '2-digit', hourCycle: 'h23' })

  return (
    <div
      ref={scroller}
      className="relative min-h-0 flex-1 overflow-auto border-t bg-background"
      data-testid="schedule-grid"
    >
      <div
        className={cn('relative grid', cols > 1 && 'min-w-[720px] md:min-w-0')}
        style={{ gridTemplateColumns: `3.5rem repeat(${cols}, minmax(0, 1fr))` }}
      >
        {/* Day headers: sticky at the top; the corner over the hour axis stays blank. */}
        <div className="sticky top-0 left-0 z-30 h-16 border-b bg-background" />
        {days.map((d) => {
          const k = dayKey(d)

          return (
            <div key={k} className="sticky top-0 z-20 h-16 border-b border-l bg-background py-1">
              <DayHeader day={d} today={k === today} />
            </div>
          )
        })}
        {HOURS.map((h) => (
          <HourRow
            key={h}
            hour={h}
            days={days}
            byDay={byDay}
            slotsByDay={slotsByDay}
            today={today}
            now={now}
            collectionOf={collectionOf}
            onOpen={onOpen}
            onDrop={onDrop}
            onAdd={onAdd}
          />
        ))}
        {/* The current time across the grid, stronger on today's column. Decorative: the time is in the header. */}
        <div
          aria-hidden
          data-testid="now-line"
          className="pointer-events-none absolute right-0 left-14 z-10 h-px bg-brand/50"
          style={{ top: nowY }}
        />
        {todayIndex >= 0 && (
          <div
            aria-hidden
            className="pointer-events-none absolute z-10 h-0.5 -translate-y-px bg-brand"
            style={{
              top: nowY,
              left: `calc(3.5rem + ${todayIndex} * ((100% - 3.5rem) / ${cols}))`,
              width: `calc((100% - 3.5rem) / ${cols})`,
            }}
          />
        )}
        <div
          aria-hidden
          className="pointer-events-none sticky left-0 z-30 col-start-1 -mt-px flex h-0 items-center"
          style={{ position: 'absolute', top: nowY }}
        >
          <span className="rounded-full bg-brand px-1.5 py-0.5 text-[10px] leading-none font-semibold text-brand-foreground tabular-nums shadow-xs">
            {nowTime}
          </span>
        </div>
      </div>
    </div>
  )
}

/** One hour across the days: the hour label, then a drop slot per day with the items planned for that hour. */
/**
 * What one hour cell holds: the first card, and "+N more" when there are others. More opens a panel over this
 * cell only (the rows keep their height, so the calendar and the now line stay put); Escape or Show less closes it.
 */
function CellItems({
  items,
  label,
  limit = 1,
  head,
}: {
  items: React.ReactNode[]
  label: string
  /** How many cards the closed cell shows. */
  limit?: number
  /** Repeated at the top of the open panel (the month day's date, which the panel covers). */
  head?: React.ReactNode
}) {
  const [open, setOpen] = useState(false)
  const panel = useRef<HTMLDivElement>(null)
  // Touch screens: a cell this small cannot hold a card and a 40 px button, so it shows one button.
  const coarse = useIsCoarsePointer()
  const shown = coarse ? 0 : limit
  const more = items.length - shown

  // Escape or a press outside closes the panel; focus moves into it when it opens.
  useEffect(() => {
    if (!open) {
      return
    }

    panel.current?.querySelector<HTMLElement>('button')?.focus()
    const onKey = (e: KeyboardEvent) => e.key === 'Escape' && setOpen(false)
    const onDown = (e: PointerEvent) => {
      if (!panel.current?.contains(e.target as Node)) {
        setOpen(false)
      }
    }

    document.addEventListener('keydown', onKey)
    document.addEventListener('pointerdown', onDown)

    return () => {
      document.removeEventListener('keydown', onKey)
      document.removeEventListener('pointerdown', onDown)
    }
  }, [open])

  if (items.length <= 1 || items.length <= shown) {
    return <>{items}</>
  }

  // While the panel is open it holds every card; the cell under it stays empty so nothing is listed twice.
  return open ? (
    <div
      ref={panel}
      role="group"
      aria-label={`Everything at ${label}`}
      className="!absolute inset-x-0 top-0 !z-30 flex min-h-full flex-col gap-1 rounded-md bg-popover p-1 shadow-lg ring-1 ring-border"
    >
      {head}
      {items}
      <button
        type="button"
        onClick={() => setOpen(false)}
        className="self-start rounded px-1 text-[11px] font-medium text-muted-foreground hover:bg-muted hover:text-foreground pointer-coarse:min-h-8"
      >
        Show less
      </button>
    </div>
  ) : (
    <>
      {items.slice(0, shown)}
      <button
        type="button"
        onClick={() => setOpen(true)}
        aria-expanded={false}
        data-testid="cell-more"
        className={cn(
          'rounded px-1 text-[11px] font-medium text-muted-foreground hover:bg-muted hover:text-foreground',
          coarse ? 'min-h-10 w-full border bg-card text-xs text-foreground' : 'mr-8 self-start',
        )}
      >
        {coarse ? `${items.length} posts` : `+${more} more`}
      </button>
    </>
  )
}

const HourRow = memo(function HourRow({
  hour,
  days,
  byDay,
  slotsByDay,
  today,
  now,
  collectionOf,
  onOpen,
  onDrop,
  onAdd,
}: {
  hour: number
  days: Date[]
  byDay: Map<string, Post[]>
  slotsByDay: Map<string, ScheduleSlot[]>
  today: string
  now: number
  collectionOf: (id: string) => Collection | undefined
  onOpen: (id: string, channel?: string) => void
  onDrop: (postId: string, day: string, hour: number) => void
  onAdd: (day: string, hour: number) => void
}) {
  const label = formatHour(hour)
  const current = new Date(now)
  const currentHour = current.getHours()

  return (
    <>
      <div className="sticky left-0 z-20 h-20 border-b bg-background pr-2 text-right text-[11px] text-muted-foreground tabular-nums">
        {/* Labels sit on the line above their hour; 00:00 has no line above, so it sits just under the header. */}
        <span className={cn('inline-block bg-background px-0.5', hour === 0 ? 'translate-y-0.5' : '-translate-y-1/2')}>
          {label}
        </span>
      </div>
      {days.map((d) => {
        const k = dayKey(d)
        const slotItems = (byDay.get(k) ?? NO_POSTS).filter((it) => new Date(it.publish_at!).getHours() === hour)
        const isToday = k === today
        const past = k < today || (isToday && hour < currentHour)
        // The running hour: hatched up to the minute, so the hatch stops exactly at the now line.
        const partial = isToday && hour === currentHour ? current.getMinutes() / 60 : 0

        return (
          <Slot
            key={k}
            testId={`schedule-slot-${k}-${hour}`}
            onDrop={(id) => onDrop(id, k, hour)}
            onAdd={past ? undefined : () => onAdd(k, hour)}
            past={past}
            addLabel={`Schedule a post for ${d.toLocaleDateString(undefined, { weekday: 'short', day: 'numeric', month: 'short' })} at ${label}`}
            addAt="bottom"
            className={cn(
              'flex h-20 min-w-0 flex-col gap-1 border-b border-l p-1',
              isToday && 'bg-brand-soft/30',
              past && HATCH,
            )}
          >
            {partial > 0 && (
              <div
                aria-hidden
                // Lines only: this overlay sits above the hour's cards, a fill would dim them.
                className={cn('pointer-events-none !absolute inset-x-0 top-0 !z-0', HATCH_LINES)}
                style={{ height: `${partial * 100}%` }}
              />
            )}
            <CellItems
              items={[
                ...slotItems.flatMap((it) =>
                  cardsOf(it).map((t) => (
                    <Entry
                      key={`${it.id}:${t?.channel_id ?? ''}`}
                      post={it}
                      target={t}
                      collection={collectionOf(it.collection_id)}
                      onOpen={onOpen}
                    />
                  )),
                ),
                ...(slotsByDay.get(k) ?? NO_SLOTS)
                  .filter((s) => s.at.getHours() === hour)
                  .map((s) => <SlotChip key={s.key} slot={s} collection={collectionOf(s.schedule.collection_id)} />),
              ]}
              label={`${d.toLocaleDateString(undefined, { weekday: 'short', day: 'numeric', month: 'short' })}, ${label}`}
            />
          </Slot>
        )
      })}
    </>
  )
})

/** The month: header cards, equal rows filling the height, each cell scrolling its own overflow. */
function MonthGrid({
  days,
  month,
  byDay,
  slotsByDay,
  today,
  collectionOf,
  onOpen,
  onDrop,
  onAdd,
}: {
  days: Date[]
  month: number
  byDay: Map<string, Post[]>
  slotsByDay: Map<string, ScheduleSlot[]>
  today: string
  collectionOf: (id: string) => Collection | undefined
  onOpen: (id: string, channel?: string) => void
  onDrop: (postId: string, day: string) => void
  onAdd: (day: string) => void
}) {
  const rows = days.length / 7

  return (
    <div className="flex min-h-0 flex-1 flex-col overflow-hidden border-t bg-background" data-testid="schedule-grid">
      <div className="grid grid-cols-7 border-b">
        {days.slice(0, 7).map((d) => (
          <div
            key={dayKey(d)}
            className="flex h-9 items-center justify-center text-xs font-medium text-muted-foreground"
          >
            {d.toLocaleDateString(undefined, { weekday: 'short' })}
          </div>
        ))}
      </div>
      <div
        className="grid min-h-0 flex-1 grid-cols-7 overflow-y-auto border-l"
        style={{ gridTemplateRows: `repeat(${rows}, minmax(6rem, 1fr))` }}
      >
        {days.map((d) => {
          const k = dayKey(d)
          const dayItems = byDay.get(k) ?? NO_POSTS
          const inMonth = d.getMonth() === month
          const isToday = k === today

          return (
            <Slot
              key={k}
              testId={`schedule-day-${k}`}
              onDrop={(id) => onDrop(id, k)}
              onAdd={k >= today ? () => onAdd(k) : undefined}
              past={k < today}
              addLabel={`Schedule a post for ${d.toLocaleDateString(undefined, { weekday: 'long', day: 'numeric', month: 'long' })}`}
              className={cn(
                'flex min-h-24 min-w-0 flex-col gap-1 border-r border-b p-1.5',
                !inMonth && 'bg-muted/40 text-muted-foreground',
                // Days before today get the same hatch as past hours in the week view.
                k < today && HATCH,
              )}
            >
              <span
                className={cn(
                  'inline-flex size-6 items-center justify-center rounded-full text-sm tabular-nums',
                  isToday ? 'bg-primary font-semibold text-primary-foreground' : 'text-foreground',
                )}
              >
                {d.getDate()}
              </span>
              <CellItems
                limit={2}
                label={d.toLocaleDateString(undefined, { weekday: 'long', day: 'numeric', month: 'long' })}
                head={<span className="px-1 text-sm font-semibold tabular-nums">{d.getDate()}</span>}
                items={[
                  ...dayItems.flatMap((it) =>
                    cardsOf(it).map((t) => (
                      <Entry
                        key={`${it.id}:${t?.channel_id ?? ''}`}
                        post={it}
                        target={t}
                        collection={collectionOf(it.collection_id)}
                        onOpen={onOpen}
                      />
                    )),
                  ),
                  ...(slotsByDay.get(k) ?? NO_SLOTS).map((s) => (
                    <SlotChip key={s.key} slot={s} collection={collectionOf(s.schedule.collection_id)} />
                  )),
                ]}
              />
            </Slot>
          )
        })}
      </div>
    </div>
  )
}

/** The range as a list grouped by day (the list layout, and phones). */
function AgendaList({
  days,
  byDay,
  slotsByDay,
  today,
  collectionOf,
  onOpen,
}: {
  days: Date[]
  byDay: Map<string, Post[]>
  slotsByDay: Map<string, ScheduleSlot[]>
  today: string
  collectionOf: (id: string) => Collection | undefined
  onOpen: (id: string, channel?: string) => void
}) {
  return (
    <div className="min-h-0 flex-1 overflow-y-auto px-4 pb-4 sm:px-6 lg:px-8" data-testid="schedule-list">
      <div className="grid gap-4">
        {days
          .filter((d) => byDay.has(dayKey(d)) || slotsByDay.has(dayKey(d)))
          .map((d) => {
            const k = dayKey(d)

            return (
              <section key={k} aria-label={dayLabel(d)} className="grid gap-1.5">
                <h2
                  className={cn(
                    'flex items-center gap-2 text-xs font-medium tracking-wide uppercase',
                    k === today ? 'text-brand' : 'text-muted-foreground',
                  )}
                >
                  {k === today && <span aria-hidden className="size-1.5 rounded-full bg-brand" />}
                  {dayLabel(d)}
                </h2>
                {(byDay.get(k) ?? NO_POSTS).flatMap((it) =>
                  cardsOf(it).map((t) => (
                    <Entry
                      key={`${it.id}:${t?.channel_id ?? ''}`}
                      post={it}
                      target={t}
                      collection={collectionOf(it.collection_id)}
                      variant="row"
                      onOpen={onOpen}
                    />
                  )),
                )}
                {(slotsByDay.get(k) ?? NO_SLOTS).map((sl) => (
                  <SlotChip key={sl.key} slot={sl} collection={collectionOf(sl.schedule.collection_id)} variant="row" />
                ))}
              </section>
            )
          })}
      </div>
    </div>
  )
}
