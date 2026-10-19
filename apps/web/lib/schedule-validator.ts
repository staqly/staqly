// Schedule validator: checks a posting plan the way Staqly reads a collection schedule
// (apps/api/internal/app/schedules.go: validateRule and nextRun) and against posting rules of thumb.
// Everything runs in the browser with Intl; the per-platform numbers are guidance, not platform limits.

export type Rule = { times?: string[]; weekdays?: number[]; tz?: string; every_minutes?: number }

export type Level = 'ok' | 'warn' | 'error' | 'info'
export type Check = { level: Level; title: string; detail?: string }

export type Plan = { runs: Date[]; rule?: Rule; errors: string[]; notes: Check[] }

// Rules of thumb per platform: how many posts a day most accounts stay under, and the shortest sensible gap
// between two posts on one account. Ours, drawn from common practice; a platform may allow more.
export type Guidance = { id: string; name: string; perDay: number; minGapMinutes: number }

export const GUIDANCE: Guidance[] = [
  { id: 'instagram', name: 'Instagram', perDay: 3, minGapMinutes: 120 },
  { id: 'tiktok', name: 'TikTok', perDay: 4, minGapMinutes: 120 },
  { id: 'youtube', name: 'YouTube', perDay: 2, minGapMinutes: 240 },
  { id: 'threads', name: 'Threads', perDay: 10, minGapMinutes: 30 },
  { id: 'x', name: 'X', perDay: 15, minGapMinutes: 30 },
  { id: 'linkedin', name: 'LinkedIn', perDay: 2, minGapMinutes: 240 },
  { id: 'facebook', name: 'Facebook', perDay: 3, minGapMinutes: 120 },
  { id: 'pinterest', name: 'Pinterest', perDay: 15, minGapMinutes: 30 },
  { id: 'bluesky', name: 'Bluesky', perDay: 15, minGapMinutes: 30 },
  { id: 'mastodon', name: 'Mastodon', perDay: 10, minGapMinutes: 30 },
]

const DAY = ['Sunday', 'Monday', 'Tuesday', 'Wednesday', 'Thursday', 'Friday', 'Saturday']

const CLOCK = /^([01]\d|2[0-3]):([0-5]\d)$/

export function validZone(tz: string): boolean {
  try {
    return !!Intl.DateTimeFormat('en-US', { timeZone: tz }).resolvedOptions().timeZone
  } catch {
    return false
  }
}

// One formatter per zone and kind: building an Intl.DateTimeFormat is slow, and the checks call these thousands of
// times per keystroke (a year of days times every time of day).
const formatters = new Map<string, Intl.DateTimeFormat>()

function formatter(key: string, locale: string, options: Intl.DateTimeFormatOptions): Intl.DateTimeFormat {
  let f = formatters.get(key)

  if (!f) {
    f = new Intl.DateTimeFormat(locale, options)
    formatters.set(key, f)
  }

  return f
}

const wallFormat = (tz: string) =>
  formatter(`wall ${tz}`, 'en-US', {
    timeZone: tz,
    hourCycle: 'h23',
    year: 'numeric',
    month: '2-digit',
    day: '2-digit',
    hour: '2-digit',
    minute: '2-digit',
    weekday: 'short',
  })

/** The wall clock of an instant in a zone. */
export function wall(at: Date, tz: string) {
  const parts = Object.fromEntries(
    wallFormat(tz)
      .formatToParts(at)
      .map((p) => [p.type, p.value]),
  )

  return {
    y: Number(parts.year),
    m: Number(parts.month),
    d: Number(parts.day),
    hh: Number(parts.hour),
    mm: Number(parts.minute),
    weekday: ['Sun', 'Mon', 'Tue', 'Wed', 'Thu', 'Fri', 'Sat'].indexOf(parts.weekday ?? ''),
  }
}

/** Minutes a zone is ahead of UTC at an instant. */
export function offset(at: Date, tz: string): number {
  const w = wall(at, tz)

  return Math.round((Date.UTC(w.y, w.m - 1, w.d, w.hh, w.mm) - Math.floor(at.getTime() / 60000) * 60000) / 60000)
}

/**
 * The instant a wall-clock time happens in a zone. A time the clocks skip (spring forward) moves on by the jump,
 * the way Staqly's server resolves it; `shifted` says so.
 */
export function zoned(y: number, m: number, d: number, hh: number, mm: number, tz: string) {
  const naive = Date.UTC(y, m - 1, d, hh, mm)
  let at = new Date(naive - offset(new Date(naive), tz) * 60000)

  at = new Date(naive - offset(at, tz) * 60000)
  const back = wall(at, tz)
  const shifted = back.hh !== hh || back.mm !== mm

  if (shifted) {
    // In the gap: keep the offset from before the jump, which lands after it (02:30 becomes 03:30).
    at = new Date(naive - offset(new Date(naive - 6 * 3600000), tz) * 60000)
  }

  return { at, shifted }
}

/** Reads a pasted rule: JSON as a collection schedule takes it. Mirrors the server's checks. */
export function parseRule(text: string): { rule?: Rule; errors: string[] } {
  let raw: unknown

  try {
    raw = JSON.parse(text)
  } catch {
    return { errors: ['This is not JSON. A rule looks like {"times":["09:00"],"weekdays":[1,2,3,4,5],"tz":"UTC"}.'] }
  }

  if (!raw || typeof raw !== 'object' || Array.isArray(raw)) {
    return { errors: ['A rule is a JSON object with times, weekdays and tz, or every_minutes.'] }
  }

  const r = raw as Record<string, unknown>
  const errors: string[] = []

  if (r.every_minutes !== undefined) {
    const n = Number(r.every_minutes)

    if (!Number.isInteger(n) || n < 5 || n > 10080) {
      errors.push('every_minutes is a whole number from 5 to 10080 (a week).')
    }

    if (r.times !== undefined || r.weekdays !== undefined) {
      errors.push('Use either every_minutes, or times with weekdays, not both.')
    }

    return errors.length ? { errors } : { rule: { every_minutes: n }, errors }
  }

  const times = Array.isArray(r.times) ? r.times.map(String) : []

  if (times.length === 0 || times.length > 24) {
    errors.push('times needs 1 to 24 times of day, such as ["09:00", "18:30"].')
  }

  for (const t of times) {
    if (!CLOCK.test(t)) {
      errors.push(`${t} is not a time of day (HH:MM, 24-hour).`)
    }
  }

  const weekdays = r.weekdays === undefined ? [] : Array.isArray(r.weekdays) ? r.weekdays.map(Number) : [NaN]

  for (const d of weekdays) {
    if (!Number.isInteger(d) || d < 0 || d > 6) {
      errors.push('weekdays are 0 (Sunday) to 6 (Saturday).')
      break
    }
  }

  const tz = r.tz === undefined || r.tz === '' ? 'UTC' : String(r.tz)

  if (!validZone(tz)) {
    errors.push(`${tz} is not a time zone name such as Europe/Warsaw or America/New_York.`)
  }

  if (errors.length) {
    return { errors }
  }

  return {
    rule: {
      times: [...new Set(times)].toSorted(),
      ...(weekdays.length ? { weekdays: [...new Set(weekdays)].toSorted((a, b) => a - b) } : {}),
      tz,
    },
    errors,
  }
}

/** The next `count` runs after `from`, the way the server computes them. */
export function nextRuns(rule: Rule, from: Date, count: number): Date[] {
  if (rule.every_minutes) {
    return Array.from({ length: count }, (_, i) => new Date(from.getTime() + (i + 1) * rule.every_minutes! * 60000))
  }

  const tz = rule.tz || 'UTC'
  const out: Date[] = []
  const start = wall(from, tz)

  for (let i = 0; out.length < count && i < 400; i++) {
    const day = new Date(Date.UTC(start.y, start.m - 1, start.d + i))
    const wd = day.getUTCDay()

    if (rule.weekdays?.length && !rule.weekdays.includes(wd)) {
      continue
    }

    for (const t of rule.times ?? []) {
      const [hh, mm] = t.split(':').map(Number) as [number, number]
      const { at } = zoned(day.getUTCFullYear(), day.getUTCMonth() + 1, day.getUTCDate(), hh, mm, tz)

      if (at > from && out.length < count) {
        out.push(at)
      }
    }
  }

  return out
}

/** A pasted list of date-times, one per line, read in the audience's zone ("2026-11-03 18:00" or ISO). */
export function parseList(text: string, tz: string): { runs: Date[]; errors: string[] } {
  const runs: Date[] = []
  const errors: string[] = []

  for (const line of text.split('\n').map((l) => l.trim())) {
    if (!line) {
      continue
    }

    const m = /^(\d{4})-(\d{2})-(\d{2})[ T](\d{2}):(\d{2})/.exec(line)

    if (!m) {
      errors.push(`"${line}" is not a date and time such as 2026-11-03 18:00.`)
      continue
    }

    // An explicit offset or Z wins; otherwise the time is in the audience's zone.
    if (/(Z|[+-]\d{2}:?\d{2})$/.test(line)) {
      const at = new Date(line.replace(' ', 'T'))

      if (Number.isNaN(at.getTime())) {
        errors.push(`"${line}" is not a date and time such as 2026-11-03 18:00.`)
      } else {
        runs.push(at)
      }

      continue
    }

    const [, y, mo, d, hh, mi] = m.map(Number) as number[]

    runs.push(zoned(y!, mo!, d!, hh!, mi!, tz).at)
  }

  return { runs: runs.toSorted((a, b) => a.getTime() - b.getTime()), errors }
}

const fmtTime = (at: Date, tz: string) =>
  formatter(`time ${tz}`, 'en-GB', { timeZone: tz, weekday: 'short', hour: '2-digit', minute: '2-digit' }).format(at)

const fmtDay = (at: Date, tz?: string) =>
  formatter(`day ${tz ?? ''}`, 'en-GB', { day: 'numeric', month: 'long', ...(tz ? { timeZone: tz } : {}) }).format(at)

const minutes = (n: number) =>
  n >= 1440 && n % 1440 === 0
    ? `${n / 1440} ${n === 1440 ? 'day' : 'days'}`
    : n >= 60
      ? `${Math.round(n / 6) / 10} h`
      : `${n} min`

/**
 * Checks a plan's runs over the next weeks: rhythm per platform, too-close posts, quiet days and gaps, hours the
 * audience is asleep, and time changes that move the posts in the audience's day.
 */
export function check(
  runs: Date[],
  opts: { platforms: string[]; audienceTz: string; ruleTz?: string; wakeFrom?: number; wakeTo?: number },
): Check[] {
  const out: Check[] = []
  const { audienceTz } = opts
  const wakeFrom = opts.wakeFrom ?? 7
  const wakeTo = opts.wakeTo ?? 23

  if (runs.length === 0) {
    return [{ level: 'error', title: 'Nothing would go out', detail: 'The plan has no times in the coming weeks.' }]
  }

  // Posts per audience day, over the first week of runs.
  const weekEnd = runs[0]!.getTime() + 7 * 86400000
  const week = runs.filter((r) => r.getTime() < weekEnd)
  const perDay = new Map<string, number>()

  for (const r of week) {
    const w = wall(r, audienceTz)
    const key = `${w.y}-${w.m}-${w.d}`

    perDay.set(key, (perDay.get(key) ?? 0) + 1)
  }

  const busiest = Math.max(...perDay.values())

  out.push({
    level: 'info',
    title: `${week.length} ${week.length === 1 ? 'post' : 'posts'} in the first week, at most ${busiest} a day`,
    detail: 'Each run publishes one post to every channel the schedule names.',
  })

  // The shortest gap between two runs.
  let gap = Infinity

  for (let i = 1; i < runs.length; i++) {
    gap = Math.min(gap, (runs[i]!.getTime() - runs[i - 1]!.getTime()) / 60000)
  }

  for (const p of GUIDANCE.filter((g) => opts.platforms.includes(g.id))) {
    const problems: string[] = []

    if (busiest > p.perDay) {
      problems.push(`up to ${busiest} posts a day, where most accounts stay at ${p.perDay} or fewer`)
    }

    if (gap < p.minGapMinutes) {
      problems.push(`two posts only ${minutes(gap)} apart, where ${minutes(p.minGapMinutes)} or more is usual`)
    }

    out.push(
      problems.length
        ? { level: 'warn', title: `${p.name}: busier than usual`, detail: `${problems.join('; ')}.` }
        : { level: 'ok', title: `${p.name}: a comfortable rhythm` },
    )
  }

  // Quiet days and the longest silence.
  const seenDays = new Set(week.map((r) => wall(r, audienceTz).weekday))
  const quiet = [1, 2, 3, 4, 5, 6, 0].filter((i) => !seenDays.has(i)).map((i) => DAY[i]!) // Monday first
  let longest = 0

  for (let i = 1; i < runs.length; i++) {
    longest = Math.max(longest, (runs[i]!.getTime() - runs[i - 1]!.getTime()) / 60000)
  }

  if (longest > 3 * 1440) {
    out.push({
      level: 'warn',
      title: `A ${minutes(Math.round(longest / 1440) * 1440)} silence`,
      detail: 'Followers forget quiet accounts; a post every two or three days keeps the rhythm.',
    })
  } else if (quiet.length && quiet.length < 7) {
    out.push({
      level: 'info',
      title: `No posts on ${quiet.join(', ')}`,
      detail: quiet.every((d) => d === 'Saturday' || d === 'Sunday')
        ? 'Fine for work audiences; leisure platforms are often busy at weekends.'
        : undefined,
    })
  } else if (!quiet.length) {
    out.push({ level: 'ok', title: 'Every day of the week has a post' })
  }

  // Asleep hours in the audience's zone.
  const night = week.filter((r) => {
    const h = wall(r, audienceTz).hh

    return h < wakeFrom || h >= wakeTo
  })

  out.push(
    night.length
      ? {
          level: 'warn',
          title: `${night.length} of ${week.length} posts land while the audience sleeps`,
          detail: `${night
            .slice(0, 4)
            .map((r) => fmtTime(r, audienceTz))
            .join(', ')} in ${audienceTz}, outside ${String(wakeFrom).padStart(2, '0')}:00 to ${wakeTo}:00.`,
        }
      : {
          level: 'ok',
          title: `Every post lands between ${String(wakeFrom).padStart(2, '0')}:00 and ${wakeTo}:00 for the audience`,
        },
  )

  // Time changes over the next half year: the schedule keeps its own wall clock, the audience's may move.
  const ruleTz = opts.ruleTz

  if (ruleTz && ruleTz !== audienceTz) {
    const now = runs[0]!
    let moved = 0
    let when = ''
    const base = offset(now, audienceTz) - offset(now, ruleTz)

    for (let d = 1; d <= 183; d++) {
      const at = new Date(now.getTime() + d * 86400000)
      const diff = offset(at, audienceTz) - offset(at, ruleTz)

      if (diff !== base) {
        moved = diff - base
        when = fmtDay(at)
        break
      }
    }

    out.push(
      moved
        ? {
            level: 'warn',
            title: `Around ${when} the posts move ${Math.abs(moved) / 60} h ${moved > 0 ? 'later' : 'earlier'} for the audience`,
            detail: `The schedule follows ${ruleTz} and the audience lives in ${audienceTz}; they change clocks on different dates or not at all. Set the schedule's tz to the audience's zone to keep their hours.`,
          }
        : { level: 'ok', title: 'No clock change moves the posts in the next six months' },
    )
  }

  return out
}

/** Clock times in a rule that the clocks skip (they run an hour later that day) within the next year. */
export function skippedTimes(rule: Rule, from: Date): string[] {
  if (!rule.times || !rule.tz) {
    return []
  }

  const found = new Set<string>()
  const start = wall(from, rule.tz)

  for (let i = 0; i < 366; i++) {
    const day = new Date(Date.UTC(start.y, start.m - 1, start.d + i))

    for (const t of rule.times) {
      const [hh, mm] = t.split(':').map(Number) as [number, number]

      if (zoned(day.getUTCFullYear(), day.getUTCMonth() + 1, day.getUTCDate(), hh, mm, rule.tz).shifted) {
        found.add(`${t} on ${fmtDay(day, 'UTC')}`)
      }
    }
  }

  return [...found]
}
