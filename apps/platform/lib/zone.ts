import { browserTimeZone } from './time-zone'

/**
 * The time zone every date in the dashboard is shown and picked in: the workspace's (Settings → Workspace), set by
 * `WorkspaceZone` in the app layout, which remounts the app when it changes. Before that (sign-in pages, unit tests)
 * the browser's. Dates stay real instants everywhere; this module turns them into wall-clock parts in the zone and
 * back, so the browser's own zone never enters the math.
 */
let zone = browserTimeZone()

export function displayZone() {
  return zone
}

/** Sets the display zone; an empty or unknown name falls back to the browser's. */
export function setDisplayZone(tz?: string | null) {
  zone = tz && isZone(tz) ? tz : browserTimeZone()
}

function isZone(tz: string) {
  try {
    return !!new Intl.DateTimeFormat('en-US', { timeZone: tz }).resolvedOptions().timeZone
  } catch {
    return false
  }
}

/** A calendar date and time of day as a clock in some zone shows it. `month` 1-12, `weekday` 0 = Sunday. */
export interface Wall {
  year: number
  month: number
  day: number
  hour: number
  minute: number
  second: number
  weekday: number
}

const partFormats = new Map<string, Intl.DateTimeFormat>()

function partFormat(tz: string) {
  let f = partFormats.get(tz)

  if (!f) {
    f = new Intl.DateTimeFormat('en-US', {
      timeZone: tz,
      hourCycle: 'h23',
      year: 'numeric',
      month: 'numeric',
      day: 'numeric',
      hour: 'numeric',
      minute: 'numeric',
      second: 'numeric',
    })
    partFormats.set(tz, f)
  }

  return f
}

/** The wall-clock time of an instant in `tz` (the display zone by default). */
export function wallOf(at: Date | number, tz = zone): Wall {
  const w = { year: 0, month: 0, day: 0, hour: 0, minute: 0, second: 0, weekday: 0 }

  for (const p of partFormat(tz).formatToParts(at)) {
    if (p.type in w) {
      w[p.type as keyof Wall] = Number(p.value)
    }
  }

  // Years before 1 come with an era; not a calendar case.
  w.weekday = new Date(Date.UTC(w.year, w.month - 1, w.day)).getUTCDay()

  return w
}

/** How far `tz`'s clock is ahead of UTC at instant `t`, in ms. */
function offsetAt(t: number, tz: string) {
  const w = wallOf(t, tz)

  return Date.UTC(w.year, w.month - 1, w.day, w.hour, w.minute, w.second) - Math.floor(t / 1000) * 1000
}

const DAY = 86_400_000

/**
 * The instant a clock in `tz` shows this wall time. Fields may overflow (day 32, hour 24, month 0) like `Date.UTC`.
 * Across a DST change it resolves like Temporal's "compatible": a time that occurs twice (clocks go back) is the
 * earlier one, a time that never occurs (clocks go forward) moves forward by the gap (02:30 → 03:30).
 */
export function fromWall(year: number, month: number, day = 1, hour = 0, minute = 0, second = 0, tz = zone): Date {
  const asUtc = Date.UTC(year, month - 1, day, hour, minute, second)
  // The zone's offsets a day before and after: transitions are never closer together than that.
  const before = offsetAt(asUtc - DAY, tz)
  const after = offsetAt(asUtc + DAY, tz)
  const early = asUtc - before

  if (before === after || offsetAt(early, tz) === before) {
    return new Date(early)
  }

  const late = asUtc - after

  // The later offset fits: past the change. Neither fits: inside the gap, read with the offset before it.
  return new Date(offsetAt(late, tz) === after ? late : early)
}

/** Midnight that starts the day of `at` in the display zone, `n` days later. */
export function startOfDay(at: Date | number, n = 0) {
  const w = wallOf(at)

  return fromWall(w.year, w.month, w.day + n)
}
