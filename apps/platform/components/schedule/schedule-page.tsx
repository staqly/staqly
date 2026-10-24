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

import { ErrorState } from '@/components/common/error-state'
import { PageIcon } from '@/components/common/page-header'
import { Button } from '@/components/ui/button'
import { ButtonGroup } from '@/components/ui/button-group'
import { Skeleton } from '@/components/ui/skeleton'
import { ToggleGroup, ToggleGroupItem } from '@/components/ui/toggle-group'
import { useClock } from '@/hooks/use-clock'
import { useIsCoarsePointer, useIsMobile } from '@/hooks/use-mobile'
import type { Collection, Post, PostTarget } from '@/lib/api'
import { pluralize } from '@/lib/format'
import { postTitle } from '@/lib/posts'
import { ProviderTile } from '@/lib/providers'
import { useCollections, usePostsInfinite, useRefetchOnForeignChange, useSchedules } from '@/lib/queries'
import { safeCssColor } from '@/lib/safe'
import {
  type CalendarView,
  SCHEDULE_LABELS,
  type ScheduleState,
  canReschedule,
  dateFormats,
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
const NO_LOOKS = new Map<string, CollectionLook>()
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

/**
 * What the calendar shows of a collection. A change to a post count keeps this list as it was (structural
 * sharing), so no card re-renders.
 */
interface CollectionLook {
  id: string
  name: string
  color?: string | null
}

const lookOf = (cols: Collection[]): CollectionLook[] => cols.map((c) => ({ id: c.id, name: c.name, color: c.color }))

/** Same items in the same order: per-cell lists are rebuilt on every post change, their posts keep their identity. */
function sameItems<T>(a: readonly T[], b: readonly T[]) {
  return a === b || (a.length === b.length && a.every((x, i) => x === b[i]))
}

function push<T>(m: Map<string, T[]>, k: string, v: T) {
  const list = m.get(k)

  if (list) {
    list.push(v)
  } else {
    m.set(k, [v])
  }
}

/** A day of the shown range with its labels, formatted once per range instead of once per cell. */
interface DayInfo {
  day: Date
  key: string
  /** "Mon" */
  weekday: string
  /** "Mon, Oct 19" */
  short: string
  /** "Monday, October 19" */
  long: string
}

function dayInfos(days: Date[]): DayInfo[] {
  return days.map((d) => ({
    day: d,
    key: dayKey(d),
    weekday: dateFormats.weekday().format(d),
    short: dateFormats.shortDay().format(d),
    long: dateFormats.longDay().format(d),
  }))
}

/** One post on the calendar: time, first line, channels, collection and what it is doing now. Draggable until it goes out. */
const Entry = memo(function Entry({
  post,
  target,
  collectionName,
  collectionColor,
  variant = 'cell',
  onOpen,
}: {
  post: Post
  /** The channel this card stands for: a post with several channels is one card per channel. */
  target?: PostTarget
  collectionName?: string
  collectionColor?: string | null
  /** `cell`: a compact card in an hour or day cell; `row`: a full row of the list view. */
  variant?: 'cell' | 'row'
  onOpen: (id: string, channel?: string) => void
}) {
  const state: ScheduleState =
    target && target.status !== 'pending' ? target.status : (scheduleState(post) ?? 'scheduled')
  const tone = TONES[STATE_TONE[state]]
  const StateIcon = STATE_ICON[state]
  const draggable = variant === 'cell' && canReschedule(post)
  const color = safeCssColor(collectionColor)
  const time = formatTime(new Date(post.publish_at!))
  const muted = state === 'published'
  const title = postTitle(post)
  const where = target ? ` on ${target.channel_name}` : ''

  return (
    <button
      type="button"
      draggable={draggable}
      data-testid="schedule-entry"
      data-post-id={post.id}
      data-channel-id={target?.channel_id}
      data-state={state}
      aria-label={`${time} ${title}${where}: ${SCHEDULE_LABELS[state]}${collectionName ? `, ${collectionName}` : ''}`}
      title={`${title}${where}: ${SCHEDULE_LABELS[state]}, ${time}${collectionName ? `, ${collectionName}` : ''}${draggable ? '. Drag to another day or hour to reschedule (every channel of the post moves).' : ''}`}
      onClick={() => onOpen(post.id, target?.channel_id)}
      onDragStart={(e) => {
        e.dataTransfer.setData(DRAG_TYPE, post.id)
        e.dataTransfer.effectAllowed = 'move'
      }}
      className={cn(
        'flex w-full min-w-0 items-center gap-1.5 rounded-md border border-l-[3px] border-l-muted-foreground/40 bg-card text-left text-xs leading-tight shadow-xs transition-colors hover:bg-muted',
        variant === 'cell'
          ? 'min-h-9 px-2.5 pointer-coarse:min-h-10 py-1.5 text-[13px]'
          : 'min-h-11 gap-2 px-3 py-2 text-sm',
        // Published: a quieter card, not a see-through one (text under opacity drops below 4.5:1).
        muted && 'bg-muted/50 shadow-none',
        draggable ? 'cursor-grab active:cursor-grabbing' : 'cursor-pointer',
      )}
      style={color ? { borderLeftColor: color } : undefined}
    >
      {/* One line: the state as an icon, the time, the title, the platform's icon. The state's name, the channel's
          name and the collection are in the title and the accessible name. */}
      <StateIcon aria-hidden className={cn('size-4 shrink-0', tone.text, state === 'publishing' && 'animate-spin')} />
      <span className={cn('shrink-0 font-medium tabular-nums', muted ? 'text-muted-foreground' : 'text-foreground')}>
        {time}
      </span>{' '}
      <span className={cn('min-w-0 flex-1 truncate font-medium', muted ? 'text-muted-foreground' : 'text-foreground')}>
        {title}
      </span>
      {target && <ProviderTile id={target.provider} className="size-5 shrink-0" />}
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
  collection?: CollectionLook
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
              'pointer-events-none !absolute inset-0.5 !z-0 flex justify-end rounded-md border border-dashed border-transparent p-1 text-sky-600 transition-colors group-hover/slot:border-sky-500/60 group-hover/slot:bg-sky-500/5 dark:text-sky-400',
              // Hovering a post in the cell is about that post: no "add here" outline then.
              'group-has-[[data-post-id]:hover]/slot:border-transparent group-has-[[data-post-id]:hover]/slot:bg-transparent group-has-[[data-post-id]:hover]/slot:[&>svg]:opacity-0',
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
function DayHeader({
  day,
  weekday,
  today,
  className,
}: {
  day: Date
  weekday: string
  today: boolean
  className?: string
}) {
  return (
    <div
      className={cn('flex h-14 flex-col items-center justify-center gap-0.5 text-xs text-muted-foreground', className)}
    >
      <span>{weekday}</span>
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
function rangeTitle(first: Date, last: Date) {
  const sameYear = first.getFullYear() === last.getFullYear()
  const sameMonth = sameYear && first.getMonth() === last.getMonth()
  const year = dateFormats.year().format(last)

  if (sameMonth) {
    return `${dateFormats.monthDay().format(first)} to ${last.getDate()}, ${year}`
  }

  if (sameYear) {
    return `${dateFormats.monthDay().format(first)} to ${dateFormats.monthDay().format(last)}, ${year}`
  }

  return `${dateFormats.monthDayYear().format(first)} to ${dateFormats.monthDayYear().format(last)}`
}

/** "now 14:05" in the header: its own minute clock, so the calendar does not re-render every minute. */
function NowText() {
  const now = useClock('minute')

  return <span className="tabular-nums">{formatTime(new Date(now))}</span>
}

export function SchedulePage() {
  const params = useSearchParams()
  const router = useRouter()
  const pathname = usePathname()
  const isMobile = useIsMobile()
  // The page renders once an hour (past hours, the queue's runs ahead); the minute clock is NowText's and NowLine's.
  const hourStart = useClock('hour')
  const today = dayKey(new Date(hourStart))
  const currentHour = new Date(hourStart).getHours()
  const view: CalendarView = params.get('view') === 'month' ? 'month' : params.get('view') === 'day' ? 'day' : 'week'
  // Phones read a week or a day as a list (the hour grid needs ~720px); a month stays a calendar on every screen.
  const narrow = useIsMobile(640)
  const layout: Layout = (narrow && view !== 'month') || params.get('list') === '1' ? 'list' : 'grid'
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
  // No polling: the recent events tell when someone else changed a post.
  const { data, error, isLoading, refetch, hasNextPage, isFetchingNextPage, isFetchNextPageError, fetchNextPage } =
    usePostsInfinite(query)

  // Published by the queue, scheduled by an agent or a slot: refetch when someone else changes a post.
  useRefetchOnForeignChange(refetch)

  // The whole range at once: a calendar cannot show "load more". A page that failed waits for Try again (which
  // refetches and clears the error) instead of being asked for again in a loop.
  useEffect(() => {
    if (hasNextPage && !isFetchingNextPage && !isFetchNextPageError) {
      void fetchNextPage()
    }
  }, [hasNextPage, isFetchingNextPage, isFetchNextPageError, fetchNextPage])

  const items = useMemo(() => data?.pages.flatMap((p) => p.data) ?? NO_POSTS, [data])
  // By local day (month, list) and by day and hour (day, week), each publish time parsed once.
  const { byDay, byHour } = useMemo(() => {
    const days = new Map<string, Post[]>()
    const hours = new Map<string, Post[]>()

    for (const it of items) {
      if (!it.publish_at) {
        continue
      }

      const d = new Date(it.publish_at)
      const k = dayKey(d)

      push(days, k, it)
      push(hours, `${k}:${d.getHours()}`, it)
    }

    return { byDay: days, byHour: hours }
  }, [items])
  const looksList = useCollections(true, lookOf).data
  const looks = useMemo(() => (looksList ? new Map(looksList.map((c) => [c.id, c])) : NO_LOOKS), [looksList])
  const schedules = useSchedules()
  // From the start of this hour: a run earlier in the hour stays on until the hour is over.
  const { slotsByDay, slotsByHour } = useMemo(() => {
    const days = new Map<string, ScheduleSlot[]>()
    const hours = new Map<string, ScheduleSlot[]>()

    for (const slot of scheduleSlots(schedules.data, range.days, hourStart)) {
      const k = dayKey(slot.at)

      push(days, k, slot)
      push(hours, `${k}:${slot.at.getHours()}`, slot)
    }

    return { slotsByDay: days, slotsByHour: hours }
  }, [schedules.data, range.days, hourStart])
  const scheduledCount = useMemo(() => items.filter((it) => scheduleState(it) === 'scheduled').length, [items])
  const { mutate: reschedule } = useSetSchedule()
  const [opened, setOpened] = useState<{ id: string; channel?: string } | null>(null)
  // The post panel mounts on the first open (its code loads then) and stays mounted, so closing animates.
  const [sheetUsed, setSheetUsed] = useState(false)
  const open = useCallback((id: string, channel?: string) => {
    setOpened({ id, channel })
    setSheetUsed(true)
  }, [])
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

  // The drop handlers read the posts from a ref: depending on them would hand every cell new handlers (and a
  // render) on each post change.
  const itemsRef = useRef(items)

  useLayoutEffect(() => {
    itemsRef.current = items
  }, [items])

  const dropOnDay = useCallback(
    (postId: string, day: string) => {
      const it = itemsRef.current.find((x) => x.id === postId)

      if (!it?.publish_at || !canReschedule(it)) {
        return
      }

      const at = moveToDay(it.publish_at, day)

      if (at !== it.publish_at && new Date(at).getTime() > Date.now()) {
        reschedule({ post: it, publishAt: at })
      }
    },
    [reschedule],
  )
  const dropOnHour = useCallback(
    (postId: string, day: string, hour: number) => {
      const it = itemsRef.current.find((x) => x.id === postId)

      if (!it?.publish_at || !canReschedule(it)) {
        return
      }

      const at = moveToHour(it.publish_at, day, hour)

      if (at !== it.publish_at && new Date(at).getTime() > Date.now()) {
        reschedule({ post: it, publishAt: at })
      }
    },
    [reschedule],
  )

  const first = range.days[0]
  const last = range.days[range.days.length - 1]
  const title =
    view === 'day'
      ? dateFormats.fullDay().format(first)
      : view === 'week'
        ? rangeTitle(first, last)
        : dateFormats.monthYear().format(anchor)
  const unit = view === 'day' ? 'day' : view === 'week' ? 'week' : 'month'

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
          onClick={() => navigate({ at: new Date() })}
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
          // A range that holds today opens the new view on today (October's month on its 1st would open a past week).
          onValueChange={(v) =>
            v &&
            navigate({
              view: v as CalendarView,
              at: range.days.some((d) => dayKey(d) === today) ? dayFromKey(today) : undefined,
            })
          }
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
      <div className="flex min-w-0 items-start gap-3">
        <PageIcon>
          <CalendarClock />
        </PageIcon>
        <div className="min-w-0">
          <h1 className="text-xl font-semibold tracking-tight">Schedule</h1>
          <p className="mt-0.5 text-sm text-muted-foreground">
            <span data-testid="schedule-count">{pluralize(scheduledCount, 'post')} scheduled</span>
            <span aria-hidden> · </span>
            <span>
              now <NowText />
            </span>
          </p>
        </div>
      </div>
      {toolbar}
    </div>
  )

  const empty = !isLoading && items.length === 0
  // An empty range still shows the grid: its "+" is one way to fill it.
  const emptyState = (
    <div
      className="my-2 flex flex-wrap items-center gap-x-3 gap-y-1 rounded-xl border border-blue-500/30 bg-blue-500/10 px-3 py-2.5 text-sm dark:border-blue-400/30 dark:bg-blue-400/10"
      data-testid="schedule-empty"
    >
      <span className="inline-flex items-center gap-1.5 font-medium">
        <CalendarClock className="size-4 text-blue-600 dark:text-blue-400" aria-hidden /> Nothing scheduled this {unit}
      </span>
      <span className="text-muted-foreground">
        Click + on a future slot to schedule a draft or write a post for that time, or give a collection a schedule that
        publishes its drafts for you. Staqly sends every post at its time.
      </span>
    </div>
  )

  let body: React.ReactNode

  if (error) {
    body = <ErrorState error={error} onRetry={() => void refetch()} />
  } else if (isLoading) {
    body = <Skeleton className="flex-1 rounded-xl" />
  } else if (layout === 'list') {
    body = (
      <AgendaList days={range.days} byDay={byDay} slotsByDay={slotsByDay} today={today} looks={looks} onOpen={open} />
    )
  } else if (view === 'month') {
    body = (
      <MonthGrid
        days={range.days}
        month={anchor.getMonth()}
        byDay={byDay}
        slotsByDay={slotsByDay}
        today={today}
        looks={looks}
        onOpen={open}
        onDrop={dropOnDay}
        onAdd={addOnDay}
      />
    )
  } else {
    body = (
      <TimeGrid
        days={range.days}
        byHour={byHour}
        slotsByHour={slotsByHour}
        today={today}
        currentHour={currentHour}
        looks={looks}
        onOpen={open}
        onDrop={dropOnHour}
        onAdd={addAtHour}
      />
    )
  }

  const sheet = sheetUsed && (
    <PostSheet id={opened?.id ?? null} channel={opened?.channel} onClose={() => setOpened(null)} />
  )

  return (
    // The toolbar keeps the page padding; the grid runs to the left, right and bottom edges.
    <div className={cn('flex min-h-0 flex-col', isMobile ? 'flex-1' : 'h-svh')} data-testid="schedule">
      <div className="grid gap-2 px-4 pt-4 pb-2 sm:px-6 lg:px-8">
        {header}
        {empty && emptyState}
      </div>
      {body}
      {sheet}
      {slot && <SlotScheduleDialog at={slot} onOpenChange={(o) => !o && setSlot(null)} />}
    </div>
  )
}

/** Left offset and width of day column `i` of `cols` on the hour grid (after the 3.5rem hour axis). */
function columnStyle(i: number, cols: number) {
  return {
    left: `calc(3.5rem + ${i} * ((100% - 3.5rem) / ${cols}))`,
    width: `calc((100% - 3.5rem) / ${cols})`,
  }
}

/**
 * The current time across the hour grid, stronger on today's column, with a time pill, and the running hour
 * of today hatched up to the minute. Decorative: the time is in the header. It has its own minute clock, so a
 * tick renders only this, not the grid.
 */
function NowLine({ todayIndex, cols }: { todayIndex: number; cols: number }) {
  const now = new Date(useClock('minute'))
  const y = HEADER_PX + (minutesOfDay(now) / 60) * ROW_PX

  return (
    <>
      {todayIndex >= 0 && now.getMinutes() > 0 && (
        <div
          aria-hidden
          // Lines only, over the cell's padding box (not its left border); the hour's cards stay above it.
          className={cn('pointer-events-none absolute z-0 border-l border-transparent bg-clip-padding', HATCH_LINES)}
          style={{
            top: HEADER_PX + now.getHours() * ROW_PX,
            height: (now.getMinutes() / 60) * ROW_PX,
            ...columnStyle(todayIndex, cols),
          }}
        />
      )}
      <div
        aria-hidden
        data-testid="now-line"
        className="pointer-events-none absolute right-0 left-14 z-10 h-px bg-brand/50"
        style={{ top: y }}
      />
      {todayIndex >= 0 && (
        <div
          aria-hidden
          className="pointer-events-none absolute z-10 h-0.5 -translate-y-px bg-brand"
          style={{ top: y, ...columnStyle(todayIndex, cols) }}
        />
      )}
      <div
        aria-hidden
        className="pointer-events-none sticky left-0 z-30 col-start-1 -mt-px flex h-0 items-center"
        style={{ position: 'absolute', top: y }}
      >
        <span className="rounded-full bg-brand px-1.5 py-0.5 text-[10px] leading-none font-semibold text-brand-foreground tabular-nums shadow-xs">
          {formatTime(now)}
        </span>
      </div>
    </>
  )
}

/**
 * The hour grid of the day and week views: sticky day headers, a sticky hour axis, 80 px rows, past hours
 * hatched, today's column tinted, a "now" line with a time pill. Scrolls so that now (or 08:00 on another
 * day) sits a third of the way down.
 */
function TimeGrid({
  days,
  byHour,
  slotsByHour,
  today,
  currentHour,
  looks,
  onOpen,
  onDrop,
  onAdd,
}: {
  days: Date[]
  /** Posts by `${dayKey}:${hour}`. */
  byHour: Map<string, Post[]>
  slotsByHour: Map<string, ScheduleSlot[]>
  today: string
  /** The hour running now (0-23): today's earlier hours are hatched. */
  currentHour: number
  looks: Map<string, CollectionLook>
  onOpen: (id: string, channel?: string) => void
  onDrop: (postId: string, day: string, hour: number) => void
  onAdd: (day: string, hour: number) => void
}) {
  const scroller = useRef<HTMLDivElement>(null)
  // Read once here, not in each of the 168 cells.
  const coarse = useIsCoarsePointer()
  const infos = useMemo(() => dayInfos(days), [days])
  const todayIndex = infos.findIndex((d) => d.key === today)
  // Each hour's cells. HourRow and HourCell compare them item by item, so a change to one post renders its row
  // and its cell only.
  const rows = useMemo(
    () =>
      HOURS.map((h) => ({
        posts: infos.map((d) => byHour.get(`${d.key}:${h}`) ?? NO_POSTS),
        slots: infos.map((d) => slotsByHour.get(`${d.key}:${h}`) ?? NO_SLOTS),
      })),
    [infos, byHour, slotsByHour],
  )

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
        {infos.map((d) => (
          <div key={d.key} className="sticky top-0 z-20 h-16 border-b border-l bg-background py-1">
            <DayHeader day={d.day} weekday={d.weekday} today={d.key === today} />
          </div>
        ))}
        {HOURS.map((h) => (
          <HourRow
            key={h}
            hour={h}
            days={infos}
            posts={rows[h].posts}
            slots={rows[h].slots}
            today={today}
            currentHour={currentHour}
            looks={looks}
            coarse={coarse}
            onOpen={onOpen}
            onDrop={onDrop}
            onAdd={onAdd}
          />
        ))}
        <NowLine todayIndex={todayIndex} cols={cols} />
      </div>
    </div>
  )
}

/**
 * What one hour cell holds: the first card, and "+N more" when there are others. More opens a panel over this
 * cell only (the rows keep their height, so the calendar and the now line stay put); Escape or Show less closes it.
 */
function CellItems({
  items,
  label,
  coarse,
  limit = 1,
  head,
}: {
  items: React.ReactNode[]
  label: string
  /** Touch screens: a cell this small cannot hold a card and a 40 px button, so it shows one button. */
  coarse: boolean
  /** How many cards the closed cell shows. */
  limit?: number
  /** Repeated at the top of the open panel (the month day's date, which the panel covers). */
  head?: React.ReactNode
}) {
  const [open, setOpen] = useState(false)
  const panel = useRef<HTMLDivElement>(null)
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

/** The cards of a cell: one per channel of each post, then the queue's runs. */
function cellCards(
  posts: Post[],
  slots: ScheduleSlot[],
  looks: Map<string, CollectionLook>,
  onOpen: (id: string, channel?: string) => void,
) {
  return [
    ...posts.flatMap((it) => {
      const look = looks.get(it.collection_id)

      return cardsOf(it).map((t) => (
        <Entry
          key={`${it.id}:${t?.channel_id ?? ''}`}
          post={it}
          target={t}
          collectionName={look?.name}
          collectionColor={look?.color}
          onOpen={onOpen}
        />
      ))
    }),
    ...slots.map((s) => <SlotChip key={s.key} slot={s} collection={looks.get(s.schedule.collection_id)} />),
  ]
}

interface HourCellProps {
  day: DayInfo
  hour: number
  /** "14:00" */
  label: string
  posts: Post[]
  slots: ScheduleSlot[]
  isToday: boolean
  past: boolean
  looks: Map<string, CollectionLook>
  coarse: boolean
  onOpen: (id: string, channel?: string) => void
  onDrop: (postId: string, day: string, hour: number) => void
  onAdd: (day: string, hour: number) => void
}

/** Equal props, the cell's posts and runs compared item by item (the lists are rebuilt on every post change). */
function sameHourCell(a: HourCellProps, b: HourCellProps) {
  return (
    a.day === b.day &&
    a.hour === b.hour &&
    a.label === b.label &&
    a.isToday === b.isToday &&
    a.past === b.past &&
    a.looks === b.looks &&
    a.coarse === b.coarse &&
    a.onOpen === b.onOpen &&
    a.onDrop === b.onDrop &&
    a.onAdd === b.onAdd &&
    sameItems(a.posts, b.posts) &&
    sameItems(a.slots, b.slots)
  )
}

/** One day of one hour: a drop slot with the items planned for that hour. */
const HourCell = memo(function HourCell({
  day,
  hour,
  label,
  posts,
  slots,
  isToday,
  past,
  looks,
  coarse,
  onOpen,
  onDrop,
  onAdd,
}: HourCellProps) {
  const k = day.key

  return (
    <Slot
      testId={`schedule-slot-${k}-${hour}`}
      onDrop={(id) => onDrop(id, k, hour)}
      onAdd={past ? undefined : () => onAdd(k, hour)}
      past={past}
      addLabel={`Schedule a post for ${day.short} at ${label}`}
      addAt="bottom"
      className={cn(
        'flex h-20 min-w-0 flex-col gap-1 border-b border-l p-1',
        isToday && 'bg-brand-soft/30',
        past && HATCH,
      )}
    >
      <CellItems items={cellCards(posts, slots, looks, onOpen)} label={`${day.short}, ${label}`} coarse={coarse} />
    </Slot>
  )
}, sameHourCell)

interface HourRowProps {
  hour: number
  days: DayInfo[]
  /** Per day of `days`: the posts and the queue's runs in this hour. */
  posts: Post[][]
  slots: ScheduleSlot[][]
  today: string
  currentHour: number
  looks: Map<string, CollectionLook>
  coarse: boolean
  onOpen: (id: string, channel?: string) => void
  onDrop: (postId: string, day: string, hour: number) => void
  onAdd: (day: string, hour: number) => void
}

function sameHourRow(a: HourRowProps, b: HourRowProps) {
  return (
    a.hour === b.hour &&
    a.days === b.days &&
    a.today === b.today &&
    a.currentHour === b.currentHour &&
    a.looks === b.looks &&
    a.coarse === b.coarse &&
    a.onOpen === b.onOpen &&
    a.onDrop === b.onDrop &&
    a.onAdd === b.onAdd &&
    a.posts.every((p, i) => sameItems(p, b.posts[i])) &&
    a.slots.every((s, i) => sameItems(s, b.slots[i]))
  )
}

/** One hour across the days: the hour label, then a cell per day. */
const HourRow = memo(function HourRow({
  hour,
  days,
  posts,
  slots,
  today,
  currentHour,
  looks,
  coarse,
  onOpen,
  onDrop,
  onAdd,
}: HourRowProps) {
  const label = formatHour(hour)

  return (
    <>
      <div className="sticky left-0 z-20 h-20 border-b bg-background pr-2 text-right text-[11px] text-muted-foreground tabular-nums">
        {/* Labels sit on the line above their hour; 00:00 has no line above, so it sits just under the header. */}
        <span className={cn('inline-block bg-background px-0.5', hour === 0 ? 'translate-y-0.5' : '-translate-y-1/2')}>
          {label}
        </span>
      </div>
      {days.map((d, i) => {
        const isToday = d.key === today

        return (
          <HourCell
            key={d.key}
            day={d}
            hour={hour}
            label={label}
            posts={posts[i]}
            slots={slots[i]}
            isToday={isToday}
            past={d.key < today || (isToday && hour < currentHour)}
            looks={looks}
            coarse={coarse}
            onOpen={onOpen}
            onDrop={onDrop}
            onAdd={onAdd}
          />
        )
      })}
    </>
  )
}, sameHourRow)

interface MonthCellProps {
  day: DayInfo
  inMonth: boolean
  posts: Post[]
  slots: ScheduleSlot[]
  today: string
  looks: Map<string, CollectionLook>
  coarse: boolean
  onOpen: (id: string, channel?: string) => void
  onDrop: (postId: string, day: string) => void
  onAdd: (day: string) => void
}

function sameMonthCell(a: MonthCellProps, b: MonthCellProps) {
  return (
    a.day === b.day &&
    a.inMonth === b.inMonth &&
    a.today === b.today &&
    a.looks === b.looks &&
    a.coarse === b.coarse &&
    a.onOpen === b.onOpen &&
    a.onDrop === b.onDrop &&
    a.onAdd === b.onAdd &&
    sameItems(a.posts, b.posts) &&
    sameItems(a.slots, b.slots)
  )
}

/** A day of the month grid: the date, its cards (two shown, "+N more") and a drop slot. */
const MonthCell = memo(function MonthCell({
  day,
  inMonth,
  posts,
  slots,
  today,
  looks,
  coarse,
  onOpen,
  onDrop,
  onAdd,
}: MonthCellProps) {
  const k = day.key
  const isToday = k === today
  const date = day.day.getDate()

  return (
    <Slot
      testId={`schedule-day-${k}`}
      onDrop={(id) => onDrop(id, k)}
      onAdd={k >= today ? () => onAdd(k) : undefined}
      past={k < today}
      addLabel={`Schedule a post for ${day.long}`}
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
        {date}
      </span>
      <CellItems
        limit={2}
        label={day.long}
        coarse={coarse}
        head={<span className="px-1 text-sm font-semibold tabular-nums">{date}</span>}
        items={cellCards(posts, slots, looks, onOpen)}
      />
    </Slot>
  )
}, sameMonthCell)

/** The month: header cards, equal rows filling the height, each cell scrolling its own overflow. */
function MonthGrid({
  days,
  month,
  byDay,
  slotsByDay,
  today,
  looks,
  onOpen,
  onDrop,
  onAdd,
}: {
  days: Date[]
  month: number
  byDay: Map<string, Post[]>
  slotsByDay: Map<string, ScheduleSlot[]>
  today: string
  looks: Map<string, CollectionLook>
  onOpen: (id: string, channel?: string) => void
  onDrop: (postId: string, day: string) => void
  onAdd: (day: string) => void
}) {
  const coarse = useIsCoarsePointer()
  const infos = useMemo(() => dayInfos(days), [days])
  const rows = days.length / 7

  return (
    <div className="flex min-h-0 flex-1 flex-col overflow-hidden border-t bg-background" data-testid="schedule-grid">
      <div className="grid grid-cols-7 border-b">
        {infos.slice(0, 7).map((d) => (
          <div key={d.key} className="flex h-9 items-center justify-center text-xs font-medium text-muted-foreground">
            {d.weekday}
          </div>
        ))}
      </div>
      <div
        className="grid min-h-0 flex-1 grid-cols-7 overflow-y-auto border-l"
        style={{ gridTemplateRows: `repeat(${rows}, minmax(6rem, 1fr))` }}
      >
        {infos.map((d) => (
          <MonthCell
            key={d.key}
            day={d}
            inMonth={d.day.getMonth() === month}
            posts={byDay.get(d.key) ?? NO_POSTS}
            slots={slotsByDay.get(d.key) ?? NO_SLOTS}
            today={today}
            looks={looks}
            coarse={coarse}
            onOpen={onOpen}
            onDrop={onDrop}
            onAdd={onAdd}
          />
        ))}
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
  looks,
  onOpen,
}: {
  days: Date[]
  byDay: Map<string, Post[]>
  slotsByDay: Map<string, ScheduleSlot[]>
  today: string
  looks: Map<string, CollectionLook>
  onOpen: (id: string, channel?: string) => void
}) {
  const infos = useMemo(
    () => dayInfos(days).filter((d) => byDay.has(d.key) || slotsByDay.has(d.key)),
    [days, byDay, slotsByDay],
  )

  return (
    <div className="min-h-0 flex-1 overflow-y-auto px-4 pb-4 sm:px-6 lg:px-8" data-testid="schedule-list">
      <div className="grid gap-4">
        {infos.map((d) => {
          const k = d.key

          return (
            <section key={k} aria-label={d.long} className="grid gap-1.5">
              <h2
                className={cn(
                  'flex items-center gap-2 text-xs font-medium tracking-wide uppercase',
                  k === today ? 'text-brand' : 'text-muted-foreground',
                )}
              >
                {k === today && <span aria-hidden className="size-1.5 rounded-full bg-brand" />}
                {d.long}
              </h2>
              {(byDay.get(k) ?? NO_POSTS).flatMap((it) => {
                const look = looks.get(it.collection_id)

                return cardsOf(it).map((t) => (
                  <Entry
                    key={`${it.id}:${t?.channel_id ?? ''}`}
                    post={it}
                    target={t}
                    collectionName={look?.name}
                    collectionColor={look?.color}
                    variant="row"
                    onOpen={onOpen}
                  />
                ))
              })}
              {(slotsByDay.get(k) ?? NO_SLOTS).map((sl) => (
                <SlotChip key={sl.key} slot={sl} collection={looks.get(sl.schedule.collection_id)} variant="row" />
              ))}
            </section>
          )
        })}
      </div>
    </div>
  )
}
