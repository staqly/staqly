import type { Post, Schedule } from './api'
import { lazyDateFormat } from './format'
import { displayZone, fromWall, startOfDay, wallOf, type Wall } from './zone'

/**
 * Scheduled publishing: `publish_at` is when the post goes out (stored UTC, shown and picked in the workspace's time zone, `lib/zone.ts`).
 * The server's job queue starts the publish itself at that time.
 */

export type ScheduleState = 'scheduled' | 'publishing' | 'published' | 'failed'

/** What a post on the calendar is doing right now; null for posts without a time. */
export function scheduleState(post: Pick<Post, 'publish_at' | 'status'>): ScheduleState | null {
  if (!post.publish_at) {
    return null
  }

  switch (post.status) {
    case 'scheduled':
      return 'scheduled'
    case 'publishing':
      return 'publishing'
    case 'published':
      return 'published'
    case 'failed':
      return 'failed'
    default:
      return 'scheduled'
  }
}

export const SCHEDULE_LABELS: Record<ScheduleState, string> = {
  scheduled: 'Scheduled',
  publishing: 'Publishing',
  published: 'Published',
  failed: 'Failed',
}

/** The time can still be set, moved or cleared: nothing has gone out yet. */
export function canReschedule(post: Pick<Post, 'status'>) {
  return post.status === 'draft' || post.status === 'scheduled' || post.status === 'failed'
}

const pad = (n: number) => String(n).padStart(2, '0')

/** The next full hour from `now`: the default of the schedule dialog. */
export function nextFullHour(now: number = Date.now()) {
  // From the instant, not the wall time: on the night clocks go back, 02:40 is followed by 02:00, not 03:00.
  return new Date(Math.floor(now / 60_000) * 60_000 - wallOf(now).minute * 60_000 + 3_600_000)
}

/** Calendar day of a date in the workspace's zone: "2026-10-18". */
export function dayKey(d: Date) {
  return wallKey(wallOf(d))
}

/** The day key of wall-clock parts already read (one `wallOf` for both the day and the hour). */
export function wallKey(w: Wall) {
  return `${w.year}-${pad(w.month)}-${pad(w.day)}`
}

/** Midnight that starts a day ("2026-10-18") in the workspace's zone. */
export function dayFromKey(key: string) {
  const [y, m, d] = key.split('-').map(Number)

  return fromWall(y, m, d)
}

/** The same wall time `n` calendar days later (23 or 25 hours across a DST change). */
export function addDays(d: Date, n: number) {
  const w = wallOf(d)

  return fromWall(w.year, w.month, w.day + n, w.hour, w.minute, w.second)
}

/** Midnight that starts the month of `d`, `n` months later. */
export function addMonths(d: Date, n: number) {
  const w = wallOf(d)

  return fromWall(w.year, w.month + n, 1)
}

/** `n` midnights from the calendar day y-m-d on. */
function daysFrom(y: number, m: number, d: number, n: number) {
  return Array.from({ length: n }, (_, i) => fromWall(y, m, d + i))
}

/** Monday of the week that holds `d` (midnight). */
export function startOfWeek(d: Date) {
  const w = wallOf(d)

  return fromWall(w.year, w.month, w.day - ((w.weekday + 6) % 7)) // Monday = 0
}

/** The 7 days of the week that holds `d`. */
export function weekDays(d: Date) {
  const w = wallOf(d)

  return daysFrom(w.year, w.month, w.day - ((w.weekday + 6) % 7), 7)
}

/** The days shown for a month: whole weeks from the Monday on or before the 1st to the Sunday on or after the last. */
export function monthGrid(d: Date) {
  const { year, month } = wallOf(d)
  const lead = (new Date(Date.UTC(year, month - 1, 1)).getUTCDay() + 6) % 7
  const length = new Date(Date.UTC(year, month, 0)).getUTCDate()

  return daysFrom(year, month, 1 - lead, Math.ceil((lead + length) / 7) * 7)
}

/** The same time of day on another calendar day, as RFC3339 UTC. */
export function moveToDay(iso: string, day: string) {
  const w = wallOf(new Date(iso))
  const [y, m, d] = day.split('-').map(Number)

  return fromWall(y, m, d, w.hour, w.minute, w.second).toISOString()
}

/** The same minutes at another hour of another day, as RFC3339 UTC. */
export function moveToHour(iso: string, day: string, hour: number) {
  const w = wallOf(new Date(iso))
  const [y, m, d] = day.split('-').map(Number)

  return fromWall(y, m, d, hour, w.minute).toISOString()
}

export type CalendarView = 'day' | 'week' | 'month'

/** Visible range of a view as the API filters (publish_after inclusive, publish_before exclusive), in UTC. */
export function rangeOf(view: CalendarView, anchor: Date) {
  const days = view === 'day' ? [startOfDay(anchor)] : view === 'week' ? weekDays(anchor) : monthGrid(anchor)
  const from = days[0]
  const to = startOfDay(days[days.length - 1], 1)

  return { publish_after: from.toISOString(), publish_before: to.toISOString(), days }
}

// Built once, on first use (see lazyDateFormat): the calendar formats hundreds of dates per render.
const timeFormat = lazyDateFormat({ hour: '2-digit', minute: '2-digit', hourCycle: 'h23' })
const scheduleFormat = lazyDateFormat({
  weekday: 'short',
  day: 'numeric',
  month: 'short',
  hour: '2-digit',
  minute: '2-digit',
})
const pickedFormat = lazyDateFormat({
  weekday: 'short',
  day: 'numeric',
  month: 'short',
  year: 'numeric',
  hour: '2-digit',
  minute: '2-digit',
  hourCycle: 'h23',
})

/** Date labels of the calendar, in the viewer's locale and the workspace's time zone. */
export const dateFormats = {
  /** "Mon" */
  weekday: lazyDateFormat({ weekday: 'short' }),
  /** "Monday" */
  longWeekday: lazyDateFormat({ weekday: 'long' }),
  /** "M" */
  narrowWeekday: lazyDateFormat({ weekday: 'narrow' }),
  /** "Monday, October 19" */
  longDay: lazyDateFormat({ weekday: 'long', day: 'numeric', month: 'long' }),
  /** "Mon, Oct 19" */
  shortDay: lazyDateFormat({ weekday: 'short', day: 'numeric', month: 'short' }),
  /** "Mon, Oct 19, 2026" */
  shortDayYear: lazyDateFormat({ weekday: 'short', day: 'numeric', month: 'short', year: 'numeric' }),
  /** The zone's short name, from `formatToParts`: "GMT+1", "EST" */
  zoneName: lazyDateFormat({ hour: 'numeric', timeZoneName: 'short' }),
  /** "Oct 19" */
  monthDay: lazyDateFormat({ month: 'short', day: 'numeric' }),
  /** "Oct 19, 2026" */
  monthDayYear: lazyDateFormat({ month: 'short', day: 'numeric', year: 'numeric' }),
  /** "2026" */
  year: lazyDateFormat({ year: 'numeric' }),
  /** "October 2026" */
  monthYear: lazyDateFormat({ month: 'long', year: 'numeric' }),
  /** "Monday, Oct 19, 2026" */
  fullDay: lazyDateFormat({ weekday: 'long', month: 'short', day: 'numeric', year: 'numeric' }),
}

export function formatTime(d: Date) {
  return timeFormat().format(d)
}

/** "Mon 18 Oct, 14:00" in the workspace's time zone. */
export function formatSchedule(iso: string) {
  return scheduleFormat().format(new Date(iso))
}

/** Minutes since midnight in the workspace's zone, for the position of a time on an hour grid. */
export function minutesOfDay(d: Date) {
  const w = wallOf(d)

  return w.hour * 60 + w.minute
}

/** Rounded up to the next `step` minutes (5 by default), seconds dropped. */
export function roundUpMinutes(d: Date, step = 5) {
  const stepMs = step * 60_000

  return new Date(Math.ceil(d.getTime() / stepMs) * stepMs)
}

/** Quick picks of the schedule picker, from `now`. */
export function presets(now: number = Date.now()) {
  const inOneHour = roundUpMinutes(new Date(now + 3_600_000))
  const w = wallOf(now)
  const tomorrow = fromWall(w.year, w.month, w.day + 1, 9)
  const until = (8 - w.weekday) % 7 || 7 // next Monday, never today
  const monday = fromWall(w.year, w.month, w.day + until, 9)

  return [
    { label: 'In 1 hour', at: inOneHour },
    { label: 'Tomorrow 09:00', at: tomorrow },
    { label: 'Next Monday 09:00', at: monday },
  ]
}

/** "Tue 21 Oct 2026, 09:00": the picked time, in the viewer's locale and the workspace's time zone (24-hour clock). */
export function formatPicked(d: Date) {
  return pickedFormat().format(d)
}

/** "14:00" on the hour axis (24-hour clock). */
export function formatHour(hour: number) {
  return formatTime(fromWall(2026, 1, 1, hour))
}

export function sameDay(a: Date, b: Date) {
  return dayKey(a) === dayKey(b)
}

/** One upcoming run of a collection schedule, placed on the calendar as a hollow chip. */
export interface ScheduleSlot {
  key: string
  at: Date
  schedule: Schedule
}

/**
 * The runs of the enabled schedules that fall on `days` and are still ahead: times × weekdays in the schedule's
 * time zone, every N minutes from the next run, or a once schedule's time. The calendar shows them so the queue
 * is visible next to the scheduled posts.
 */
export function scheduleSlots(
  schedules: Schedule[] | undefined,
  days: Date[],
  now: number = Date.now(),
): ScheduleSlot[] {
  const out: ScheduleSlot[] = []
  const first = days[0]
  const last = days[days.length - 1]

  if (!first || !last) {
    return out
  }

  const end = addDays(last, 1).getTime()
  const push = (schedule: Schedule, at: Date) => {
    if (at.getTime() >= now && at.getTime() < end && at.getTime() >= first.getTime()) {
      out.push({ key: `${schedule.id}:${at.toISOString()}`, at, schedule })
    }
  }

  for (const sch of schedules ?? []) {
    if (!sch.enabled) {
      continue
    }

    if (sch.kind === 'once') {
      if (sch.run_at) {
        push(sch, new Date(sch.run_at))
      }

      continue
    }

    const rule = sch.rule ?? {}

    if (rule.every_minutes && sch.next_run_at) {
      const step = rule.every_minutes * 60_000

      for (let t = new Date(sch.next_run_at).getTime(); t < end; t += step) {
        push(sch, new Date(t))
      }

      continue
    }

    const tz = rule.tz || displayZone()
    const weekdays = rule.weekdays?.length ? rule.weekdays : [0, 1, 2, 3, 4, 5, 6]

    for (const day of days) {
      // The same calendar date in the schedule's zone; weekdays numbered like the API: 0 = Sunday ... 6 = Saturday.
      const d = wallOf(day)

      if (!weekdays.includes(d.weekday)) {
        continue
      }

      for (const time of rule.times?.length ? rule.times : ['09:00']) {
        const [h, m] = time.split(':').map(Number)

        push(sch, fromWall(d.year, d.month, d.day, h ?? 0, m ?? 0, 0, tz))
      }
    }
  }

  return out.toSorted((a, b) => a.at.getTime() - b.at.getTime())
}
