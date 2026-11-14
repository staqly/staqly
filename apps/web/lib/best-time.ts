// Best time to post: rules of thumb per platform, in the audience's own time zone. Each window is a starting
// point (strong 3, good 2, worth a try 1) drawn from engagement patterns that social media studies report again
// and again: lunch and evening peaks, work hours for professional networks, weekends for leisure platforms.
// Written in our own words; your own audience is the real answer.

export type Day = 0 | 1 | 2 | 3 | 4 | 5 | 6 // Monday = 0

export type Window = { days: Day[]; from: number; to: number; score: 1 | 2 | 3 }

export type PlatformTimes = {
  id: string
  name: string
  why: string
  windows: Window[]
}

const WEEKDAYS: Day[] = [0, 1, 2, 3, 4]
const MID_WEEK: Day[] = [1, 2, 3]
const WEEKEND: Day[] = [5, 6]
const EVERY_DAY: Day[] = [0, 1, 2, 3, 4, 5, 6]

export const DAY_NAMES = ['Monday', 'Tuesday', 'Wednesday', 'Thursday', 'Friday', 'Saturday', 'Sunday']
export const DAY_SHORT = ['Mon', 'Tue', 'Wed', 'Thu', 'Fri', 'Sat', 'Sun']

export const PLATFORMS: PlatformTimes[] = [
  {
    id: 'instagram',
    name: 'Instagram',
    why: 'People scroll at lunch and after work; mid-week evenings tend to be strongest, weekends are slower but steady late in the morning.',
    windows: [
      { days: EVERY_DAY, from: 11, to: 14, score: 2 },
      { days: EVERY_DAY, from: 18, to: 21, score: 2 },
      { days: MID_WEEK, from: 11, to: 13, score: 3 },
      { days: MID_WEEK, from: 19, to: 21, score: 3 },
      { days: WEEKEND, from: 9, to: 11, score: 1 },
    ],
  },
  {
    id: 'tiktok',
    name: 'TikTok',
    why: 'Viewing peaks in the evening and late at night, with a smaller lunch bump; Tuesday to Thursday evenings and weekend mornings stand out.',
    windows: [
      { days: EVERY_DAY, from: 12, to: 14, score: 1 },
      { days: EVERY_DAY, from: 18, to: 23, score: 2 },
      { days: MID_WEEK, from: 19, to: 22, score: 3 },
      { days: WEEKEND, from: 9, to: 12, score: 2 },
    ],
  },
  {
    id: 'youtube',
    name: 'YouTube',
    why: 'Viewing peaks in the evening and at weekends, so publish a few hours earlier: afternoons late in the week, weekend mornings.',
    windows: [
      { days: WEEKDAYS, from: 14, to: 17, score: 2 },
      { days: [3, 4], from: 14, to: 17, score: 3 },
      { days: WEEKEND, from: 9, to: 12, score: 3 },
      { days: WEEKEND, from: 12, to: 15, score: 1 },
    ],
  },
  {
    id: 'x',
    name: 'X',
    why: 'News and conversation move fastest on weekday mornings; the middle of the week is busiest, weekends are quiet.',
    windows: [
      { days: WEEKDAYS, from: 8, to: 12, score: 2 },
      { days: MID_WEEK, from: 9, to: 11, score: 3 },
      { days: WEEKDAYS, from: 17, to: 19, score: 1 },
    ],
  },
  {
    id: 'linkedin',
    name: 'LinkedIn',
    why: 'A work network: before work, at lunch and in the early afternoon on Tuesday to Thursday; weekends are the weakest days.',
    windows: [
      { days: WEEKDAYS, from: 8, to: 10, score: 2 },
      { days: WEEKDAYS, from: 12, to: 14, score: 2 },
      { days: MID_WEEK, from: 8, to: 10, score: 3 },
      { days: MID_WEEK, from: 12, to: 13, score: 3 },
    ],
  },
  {
    id: 'facebook',
    name: 'Facebook',
    why: 'Steady through the working day with a morning to early-afternoon peak; weekends are quieter.',
    windows: [
      { days: WEEKDAYS, from: 9, to: 14, score: 2 },
      { days: MID_WEEK, from: 10, to: 12, score: 3 },
      { days: WEEKEND, from: 10, to: 12, score: 1 },
    ],
  },
  {
    id: 'threads',
    name: 'Threads',
    why: 'Conversation-led like X but more evening-friendly: early mornings and evenings on weekdays.',
    windows: [
      { days: WEEKDAYS, from: 7, to: 9, score: 2 },
      { days: WEEKDAYS, from: 19, to: 22, score: 2 },
      { days: MID_WEEK, from: 20, to: 22, score: 3 },
    ],
  },
  {
    id: 'pinterest',
    name: 'Pinterest',
    why: 'People plan and browse in their free time: evenings, Friday to Sunday most of all.',
    windows: [
      { days: EVERY_DAY, from: 20, to: 23, score: 2 },
      { days: [4, 5, 6], from: 20, to: 23, score: 3 },
      { days: WEEKEND, from: 14, to: 17, score: 1 },
    ],
  },
]

/** Score per day and hour (7 x 24) in the audience's time zone: the best window covering each hour. */
export function scoreGrid(p: PlatformTimes): number[][] {
  const grid = Array.from({ length: 7 }, () => Array.from<number>({ length: 24 }).fill(0))

  for (const w of p.windows) {
    for (const d of w.days) {
      for (let h = w.from; h < w.to; h++) {
        grid[d]![h] = Math.max(grid[d]![h]!, w.score)
      }
    }
  }

  return grid
}

/** Minutes the zone is ahead of UTC at a moment, from Intl ("GMT+05:30" -> 330). */
export function offsetMinutes(timeZone: string, at = new Date()): number {
  const part = new Intl.DateTimeFormat('en-US', { timeZone, timeZoneName: 'longOffset' })
    .formatToParts(at)
    .find((x) => x.type === 'timeZoneName')?.value
  const m = /GMT([+-])(\d{2}):(\d{2})/.exec(part ?? '')

  return m ? (m[1] === '-' ? -1 : 1) * (Number(m[2]) * 60 + Number(m[3])) : 0
}

/**
 * The grid moved from the audience's zone into yours: hour h on day d there is h + shift here, wrapping across
 * days and the week. Whole hours; a half-hour zone rounds to the nearest hour.
 */
export function shiftGrid(grid: number[][], shiftMinutes: number): number[][] {
  const shift = Math.round(shiftMinutes / 60)
  const out = Array.from({ length: 7 }, () => Array.from<number>({ length: 24 }).fill(0))

  for (let d = 0; d < 7; d++) {
    for (let h = 0; h < 24; h++) {
      const t = (((d * 24 + h + shift) % 168) + 168) % 168

      out[Math.floor(t / 24)]![t % 24] = grid[d]![h]!
    }
  }

  return out
}

/** The strongest start hours for a Staqly schedule, in the audience's zone: up to two, at least three hours apart. */
export function scheduleSuggestion(grid: number[][]): { weekdays: number[]; times: string[] } {
  const totals = Array.from({ length: 24 }, (_, h) => grid.reduce((sum, day) => sum + day[h]!, 0))
  const times: number[] = []

  for (const h of [...totals.keys()].toSorted((a, b) => totals[b]! - totals[a]! || a - b)) {
    if (totals[h]! > 0 && times.length < 2 && times.every((t) => Math.abs(t - h) >= 3)) {
      times.push(h)
    }
  }

  const best = Math.max(...grid.flat())
  // Days with a slot at the strongest level, numbered the way a Staqly schedule takes them (0 = Sunday).
  const weekdays = grid.flatMap((day, d) => (day.some((s) => s === best) ? [(d + 1) % 7] : [])).toSorted()

  return { weekdays, times: times.toSorted((a, b) => a - b).map((h) => `${String(h).padStart(2, '0')}:00`) }
}
