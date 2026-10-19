'use client'

import { ProviderIcon } from '@staqly/brand/providers'
import { CalendarClock, Flame } from 'lucide-react'
import { useMemo, useState, useSyncExternalStore } from 'react'

import { CopyButton, Select } from '@/components/tools/shared'
import {
  DAY_NAMES,
  DAY_SHORT,
  PLATFORMS,
  offsetMinutes,
  scheduleSuggestion,
  scoreGrid,
  shiftGrid,
} from '@/lib/best-time'

const LEVELS = ['Quiet', 'Worth a try', 'Good', 'Strong']
const CELL = [
  'bg-muted/60',
  'bg-[color-mix(in_oklch,var(--brand)_22%,transparent)]',
  'bg-[color-mix(in_oklch,var(--brand)_50%,transparent)]',
  'bg-brand',
]

// The browser's own zone is only known on the client; the static page renders with UTC and switches on load.
const noSubscribe = () => () => {}
const browserZone = () => Intl.DateTimeFormat().resolvedOptions().timeZone
const serverZone = () => 'UTC'

function zones(): string[] {
  try {
    return Intl.supportedValuesOf('timeZone')
  } catch {
    return ['UTC']
  }
}

const hour = (h: number) => `${String(h).padStart(2, '0')}:00`

export function BestTimeTool() {
  const mine = useSyncExternalStore(noSubscribe, browserZone, serverZone)
  const [platform, setPlatform] = useState('instagram')
  const [audienceChoice, setAudience] = useState<string | null>(null)
  const audience = audienceChoice ?? mine
  const all = useMemo(() => [...new Set([mine, 'UTC', ...zones()])], [mine])
  const p = PLATFORMS.find((x) => x.id === platform) ?? PLATFORMS[0]!
  const grid = useMemo(() => scoreGrid(p), [p])
  const shift = offsetMinutes(mine) - offsetMinutes(audience)
  const local = useMemo(() => shiftGrid(grid, shift), [grid, shift])
  const plan = scheduleSuggestion(grid)
  const rule = JSON.stringify({ times: plan.times, weekdays: plan.weekdays, tz: audience })
  const dayList = plan.weekdays.map((d) => DAY_SHORT[(d + 6) % 7]).join(', ')
  // Per day, the strong hours as ranges: "11:00 to 13:00".
  const strong = local
    .map((day, d) => {
      const ranges: string[] = []

      for (let h = 0; h < 24; h++) {
        if (day[h] === 3 && day[h - 1] !== 3) {
          let end = h

          while (day[end] === 3) {
            end++
          }

          ranges.push(`${hour(h)} to ${hour(end % 24)}`)
        }
      }

      return { d, ranges }
    })
    .filter((x) => x.ranges.length)

  return (
    <div className="grid gap-6 *:min-w-0">
      <div className="grid gap-4 *:min-w-0 sm:grid-cols-2">
        <label className="grid gap-1.5 text-sm font-medium">
          Platform
          <Select value={platform} onChange={(e) => setPlatform(e.target.value)}>
            {PLATFORMS.map((x) => (
              <option key={x.id} value={x.id}>
                {x.name}
              </option>
            ))}
          </Select>
        </label>
        <label className="grid gap-1.5 text-sm font-medium">
          Where most of your audience lives
          <Select value={audience} onChange={(e) => setAudience(e.target.value)}>
            {all.map((z) => (
              <option key={z} value={z}>
                {z === mine ? `${z} (yours)` : z}
              </option>
            ))}
          </Select>
        </label>
      </div>

      <div className="rounded-2xl border border-border bg-card p-4 sm:p-6">
        <div className="flex flex-wrap items-center gap-3">
          <ProviderIcon id={p.id} name={p.name} className="size-5" />
          <h2 className="font-semibold tracking-tight">{p.name}, in your time</h2>
          <span className="text-sm text-muted-foreground">
            {mine}
            {shift !== 0 && ` · ${shift > 0 ? '+' : ''}${shift / 60} h from your audience`}
          </span>
        </div>
        <p className="mt-2 max-w-3xl text-sm leading-relaxed text-muted-foreground">{p.why}</p>

        <div className="mt-5 overflow-x-auto">
          <table className="w-full min-w-[17rem] table-fixed border-separate border-spacing-0.5 text-xs">
            <caption className="sr-only">
              Posting strength per hour and day for {p.name}, in {mine}
            </caption>
            <thead>
              <tr>
                <th scope="col" className="w-11 text-left font-normal text-muted-foreground sm:w-14">
                  <span className="sr-only">Hour</span>
                </th>
                {DAY_SHORT.map((d, i) => (
                  <th key={d} scope="col" className="pb-1 font-medium text-muted-foreground">
                    <abbr title={DAY_NAMES[i]} className="no-underline">
                      {d}
                    </abbr>
                  </th>
                ))}
              </tr>
            </thead>
            <tbody>
              {Array.from({ length: 24 }, (_, h) => (
                <tr key={h}>
                  <th scope="row" className="pr-2 text-left font-mono font-normal text-muted-foreground tabular-nums">
                    {hour(h)}
                  </th>
                  {local.map((day, d) => (
                    <td
                      key={DAY_SHORT[d]}
                      className={`h-4 rounded-[3px] sm:h-5 ${CELL[day[h]!]}`}
                      title={`${DAY_NAMES[d]} ${hour(h)}: ${LEVELS[day[h]!]}`}
                    >
                      <span className="sr-only">{LEVELS[day[h]!]}</span>
                    </td>
                  ))}
                </tr>
              ))}
            </tbody>
          </table>
        </div>
        <ul className="mt-4 flex flex-wrap gap-x-4 gap-y-2 text-xs text-muted-foreground" aria-label="Legend">
          {LEVELS.map((l, i) => (
            <li key={l} className="inline-flex items-center gap-1.5">
              <span aria-hidden className={`size-3 rounded-[3px] ${CELL[i]}`} /> {l}
            </li>
          ))}
        </ul>
      </div>

      <div className="grid items-start gap-4 *:min-w-0 lg:grid-cols-2">
        <section className="flex flex-col rounded-2xl border border-border bg-card" aria-labelledby="top-slots">
          <header className="flex items-center gap-3 border-b border-border px-5 py-4 sm:px-6">
            <span className="flex size-8 items-center justify-center rounded-lg bg-brand-soft text-brand-strong">
              <Flame className="size-4" />
            </span>
            <div className="min-w-0">
              <h2 id="top-slots" className="font-semibold tracking-tight">
                Strongest slots
              </h2>
              <p className="truncate text-xs text-muted-foreground">In your time, {mine}</p>
            </div>
          </header>
          <ul className="grid flex-1 content-start divide-y divide-border px-5 sm:px-6">
            {strong.map(({ d, ranges }) => (
              <li key={d} className="flex flex-wrap items-center justify-between gap-2 py-3">
                <span className="text-sm font-medium">{DAY_NAMES[d]}</span>
                <span className="flex flex-wrap gap-1.5">
                  {ranges.map((r) => (
                    <span
                      key={r}
                      className="rounded-full border border-[color-mix(in_oklch,var(--brand)_30%,transparent)] bg-brand-soft/60 px-2.5 py-0.5 font-mono text-xs text-brand-strong tabular-nums"
                    >
                      {r.replace(' to ', '–')}
                    </span>
                  ))}
                </span>
              </li>
            ))}
          </ul>
        </section>
        <section className="flex flex-col rounded-2xl border border-border bg-card" aria-labelledby="as-schedule">
          <header className="flex items-center gap-3 border-b border-border px-5 py-4 sm:px-6">
            <span className="flex size-8 items-center justify-center rounded-lg bg-brand-soft text-brand-strong">
              <CalendarClock className="size-4" />
            </span>
            <div className="min-w-0">
              <h2 id="as-schedule" className="font-semibold tracking-tight">
                As a Staqly schedule
              </h2>
              <p className="truncate text-xs text-muted-foreground">Publishes the next waiting post at each slot</p>
            </div>
          </header>
          <div className="grid flex-1 content-start gap-4 px-5 py-4 sm:px-6">
            <dl className="grid grid-cols-[auto_1fr] items-center gap-x-4 gap-y-2 text-sm">
              <dt className="text-muted-foreground">Days</dt>
              <dd className="flex flex-wrap gap-1.5">
                {dayList.split(', ').map((d) => (
                  <span
                    key={d}
                    className="rounded-md border border-border bg-background px-2 py-0.5 text-xs font-medium"
                  >
                    {d}
                  </span>
                ))}
              </dd>
              <dt className="text-muted-foreground">Times</dt>
              <dd className="flex flex-wrap gap-1.5">
                {plan.times.map((t) => (
                  <span
                    key={t}
                    className="rounded-md border border-border bg-background px-2 py-0.5 font-mono text-xs tabular-nums"
                  >
                    {t}
                  </span>
                ))}
              </dd>
              <dt className="text-muted-foreground">Time zone</dt>
              <dd className="truncate text-xs">{audience}</dd>
            </dl>
            <div className="overflow-hidden rounded-xl border border-border bg-background">
              <div className="flex items-center justify-between gap-2 border-b border-border py-1.5 pr-1.5 pl-3">
                <span className="font-mono text-[11px] tracking-wide text-muted-foreground uppercase">
                  Schedule rule
                </span>
                <CopyButton text={rule} label="Copy rule" />
              </div>
              <pre className="overflow-x-auto p-3 font-mono text-xs leading-relaxed">
                {`{\n  "times": ${JSON.stringify(plan.times).replaceAll(',', ', ')},\n  "weekdays": ${JSON.stringify(plan.weekdays).replaceAll(',', ', ')},\n  "tz": ${JSON.stringify(audience)}\n}`}
              </pre>
            </div>
            <p className="text-xs leading-relaxed text-muted-foreground">
              Paste it into a collection's schedule in Staqly, or let your agent send it over the API.
            </p>
          </div>
        </section>
      </div>
    </div>
  )
}
