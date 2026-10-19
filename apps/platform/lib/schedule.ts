import type { Post, Schedule } from './api'

/**
 * Scheduled publishing: `publish_at` is when the post goes out (stored UTC, shown in the viewer's local time).
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

/** A Date as the value of an `<input type="datetime-local">` (local time, minute precision). */
export function toLocalInput(d: Date) {
  return `${d.getFullYear()}-${pad(d.getMonth() + 1)}-${pad(d.getDate())}T${pad(d.getHours())}:${pad(d.getMinutes())}`
}

/** The value of an `<input type="datetime-local">` as an RFC3339 UTC string, or null when empty or invalid. */
export function fromLocalInput(value: string): string | null {
  if (!value) {
    return null
  }

  const d = new Date(value)

  return Number.isNaN(d.getTime()) ? null : d.toISOString()
}

/** The next full hour from `now`: the default of the schedule dialog. */
export function nextFullHour(now: number = Date.now()) {
  const d = new Date(now)

  d.setMinutes(0, 0, 0)
  d.setHours(d.getHours() + 1)

  return d
}

/** Local calendar day of a date: "2026-10-18". */
export function dayKey(d: Date) {
  return `${d.getFullYear()}-${pad(d.getMonth() + 1)}-${pad(d.getDate())}`
}

export function dayFromKey(key: string) {
  const [y, m, d] = key.split('-').map(Number)

  return new Date(y, m - 1, d)
}

export function addDays(d: Date, n: number) {
  const out = new Date(d)

  out.setDate(out.getDate() + n)

  return out
}

/** Monday of the week that holds `d` (local time, midnight). */
export function startOfWeek(d: Date) {
  const out = new Date(d.getFullYear(), d.getMonth(), d.getDate())
  const dow = (out.getDay() + 6) % 7 // Monday = 0

  return addDays(out, -dow)
}

/** The 7 days of the week that holds `d`. */
export function weekDays(d: Date) {
  const start = startOfWeek(d)

  return Array.from({ length: 7 }, (_, i) => addDays(start, i))
}

/** The days shown for a month: whole weeks from the Monday on or before the 1st to the Sunday on or after the last. */
export function monthGrid(d: Date) {
  const first = new Date(d.getFullYear(), d.getMonth(), 1)
  const last = new Date(d.getFullYear(), d.getMonth() + 1, 0)
  const start = startOfWeek(first)
  const end = addDays(startOfWeek(last), 6)
  const days: Date[] = []

  for (let x = start; x <= end; x = addDays(x, 1)) {
    days.push(x)
  }

  return days
}

/** The same time of day on another calendar day, as RFC3339 UTC. */
export function moveToDay(iso: string, day: string) {
  const at = new Date(iso)
  const target = dayFromKey(day)

  target.setHours(at.getHours(), at.getMinutes(), at.getSeconds(), 0)

  return target.toISOString()
}

/** The same minutes at another hour of another day, as RFC3339 UTC. */
export function moveToHour(iso: string, day: string, hour: number) {
  const at = new Date(iso)
  const target = dayFromKey(day)

  target.setHours(hour, at.getMinutes(), 0, 0)

  return target.toISOString()
}

export type CalendarView = 'day' | 'week' | 'month'

/** Visible range of a view as the API filters (publish_after inclusive, publish_before exclusive), in UTC. */
export function rangeOf(view: CalendarView, anchor: Date) {
  const days =
    view === 'day'
      ? [new Date(anchor.getFullYear(), anchor.getMonth(), anchor.getDate())]
      : view === 'week'
        ? weekDays(anchor)
        : monthGrid(anchor)
  const from = days[0]
  const to = addDays(days[days.length - 1], 1)

  return { publish_after: from.toISOString(), publish_before: to.toISOString(), days }
}

export function formatTime(d: Date) {
  return d.toLocaleTimeString(undefined, { hour: '2-digit', minute: '2-digit', hourCycle: 'h23' })
}

/** "Mon 18 Oct, 14:00" in the viewer's time zone. */
export function formatSchedule(iso: string) {
  return new Date(iso).toLocaleString(undefined, {
    weekday: 'short',
    day: 'numeric',
    month: 'short',
    hour: '2-digit',
    minute: '2-digit',
  })
}

/** Minutes since local midnight, for the position of a time on an hour grid. */
export function minutesOfDay(d: Date) {
  return d.getHours() * 60 + d.getMinutes()
}

/** Rounded up to the next `step` minutes (5 by default), seconds dropped. */
export function roundUpMinutes(d: Date, step = 5) {
  const stepMs = step * 60_000

  return new Date(Math.ceil(d.getTime() / stepMs) * stepMs)
}

/** Quick picks of the schedule picker, from `now`. */
export function presets(now: number = Date.now()) {
  const inOneHour = roundUpMinutes(new Date(now + 3_600_000))
  const tomorrow = new Date(now)

  tomorrow.setDate(tomorrow.getDate() + 1)
  tomorrow.setHours(9, 0, 0, 0)

  const monday = new Date(now)
  const until = (8 - monday.getDay()) % 7 || 7 // next Monday, never today

  monday.setDate(monday.getDate() + until)
  monday.setHours(9, 0, 0, 0)

  return [
    { label: 'In 1 hour', at: inOneHour },
    { label: 'Tomorrow 09:00', at: tomorrow },
    { label: 'Next Monday 09:00', at: monday },
  ]
}

/** "Tue 21 Oct 2026, 09:00": the picked time, in the viewer's locale and time zone (24-hour clock). */
export function formatPicked(d: Date) {
  return d.toLocaleString(undefined, {
    weekday: 'short',
    day: 'numeric',
    month: 'short',
    year: 'numeric',
    hour: '2-digit',
    minute: '2-digit',
    hourCycle: 'h23',
  })
}

/** "14:00" on the hour axis (24-hour clock). */
export function formatHour(hour: number) {
  const d = new Date(2026, 0, 1, hour, 0)

  return d.toLocaleTimeString(undefined, { hour: '2-digit', minute: '2-digit', hourCycle: 'h23' })
}

export function sameDay(a: Date, b: Date) {
  return a.getFullYear() === b.getFullYear() && a.getMonth() === b.getMonth() && a.getDate() === b.getDate()
}

/** One upcoming run of a collection schedule, placed on the calendar as a hollow chip. */
export interface ScheduleSlot {
  key: string
  at: Date
  schedule: Schedule
}

/** Wall-clock parts of `d` in another time zone. */
function partsIn(d: Date, tz: string) {
  const f = new Intl.DateTimeFormat('en-US', {
    timeZone: tz,
    hourCycle: 'h23',
    year: 'numeric',
    month: '2-digit',
    day: '2-digit',
    hour: '2-digit',
    minute: '2-digit',
    weekday: 'short',
  })
  const get = (type: string) => f.formatToParts(d).find((x) => x.type === type)?.value ?? ''

  return { y: get('year'), m: get('month'), d: get('day'), h: get('hour'), min: get('minute'), wd: get('weekday') }
}

/** The instant of `time` (HH:MM) on the calendar day of `day` in `tz`, by correcting a UTC guess with the zone's offset at that moment. */
function instantIn(day: Date, time: string, tz: string): Date {
  const [h, m] = time.split(':').map(Number)
  const guess = new Date(Date.UTC(day.getFullYear(), day.getMonth(), day.getDate(), h ?? 0, m ?? 0))
  const got = partsIn(guess, tz)
  const asIfUtc = Date.UTC(Number(got.y), Number(got.m) - 1, Number(got.d), Number(got.h), Number(got.min))

  return new Date(guess.getTime() - (asIfUtc - guess.getTime()))
}

const WD = ['Sun', 'Mon', 'Tue', 'Wed', 'Thu', 'Fri', 'Sat']

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

    const tz = rule.tz || Intl.DateTimeFormat().resolvedOptions().timeZone || 'UTC'
    const weekdays = rule.weekdays?.length ? rule.weekdays : [0, 1, 2, 3, 4, 5, 6]

    for (const day of days) {
      // The day as seen in the schedule's zone, numbered like the API: 0 = Sunday ... 6 = Saturday.
      const wd = WD.indexOf(partsIn(new Date(day.getFullYear(), day.getMonth(), day.getDate(), 12), tz).wd)

      if (!weekdays.includes(wd)) {
        continue
      }

      for (const time of rule.times?.length ? rule.times : ['09:00']) {
        push(sch, instantIn(day, time, tz))
      }
    }
  }

  return out.toSorted((a, b) => a.at.getTime() - b.at.getTime())
}
