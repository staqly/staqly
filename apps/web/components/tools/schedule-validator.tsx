'use client'

import { ProviderIcon } from '@staqly/brand/providers'
import { AlertTriangle, CheckCircle2, Info, XCircle } from 'lucide-react'
import { useMemo, useState, useSyncExternalStore, type ReactNode } from 'react'

import { CopyButton, fieldClass, Select } from '@/components/tools/shared'
import {
  GUIDANCE,
  check,
  nextRuns,
  parseList,
  parseRule,
  skippedTimes,
  type Check,
  type Rule,
} from '@/lib/schedule-validator'

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

// Monday first in the form; the rule numbers days the way the API does (0 = Sunday).
const WEEK: [number, string][] = [
  [1, 'Mon'],
  [2, 'Tue'],
  [3, 'Wed'],
  [4, 'Thu'],
  [5, 'Fri'],
  [6, 'Sat'],
  [0, 'Sun'],
]

const ICON: Record<Check['level'], ReactNode> = {
  ok: <CheckCircle2 className="size-4 text-brand" />,
  info: <Info className="size-4 text-sky-400" />,
  warn: <AlertTriangle className="size-4 text-amber-400" />,
  error: <XCircle className="size-4 text-red-400" />,
}

const LABEL: Record<Check['level'], string> = { ok: 'Fine', info: 'Note', warn: 'Check', error: 'Problem' }

const RUNS = 10

const fmt = (at: Date, tz: string) =>
  new Intl.DateTimeFormat('en-GB', {
    timeZone: tz,
    weekday: 'short',
    day: 'numeric',
    month: 'short',
    hour: '2-digit',
    minute: '2-digit',
  }).format(at)

export function ScheduleValidatorTool() {
  const mine = useSyncExternalStore(noSubscribe, browserZone, serverZone)
  const [mode, setMode] = useState<'rule' | 'list'>('rule')
  const [ruleText, setRuleText] = useState<string | null>(null)
  const [listText, setListText] = useState('2026-11-03 18:00\n2026-11-04 18:00\n2026-11-06 07:30')
  const [audienceChoice, setAudience] = useState<string | null>(null)
  const [platforms, setPlatforms] = useState(['instagram', 'tiktok', 'youtube'])
  const audience = audienceChoice ?? mine
  const text =
    ruleText ?? JSON.stringify({ times: ['09:00', '18:00'], weekdays: [1, 2, 3, 4, 5], tz: audienceChoice ?? mine })
  const all = useMemo(() => [...new Set([mine, 'UTC', ...zones()])], [mine])
  const [now] = useState(() => new Date())

  const parsed = parseRule(text)
  const rule: Rule | undefined = mode === 'rule' ? parsed.rule : undefined
  const list = mode === 'list' ? parseList(listText, audience) : undefined
  const errors = mode === 'rule' ? parsed.errors : (list?.errors ?? [])
  const runs = rule ? nextRuns(rule, now, 60) : (list?.runs.filter((r) => r > now) ?? [])
  const checks = errors.length ? [] : check(runs, { platforms, audienceTz: audience, ruleTz: rule?.tz })
  const skipped = rule ? skippedTimes(rule, now) : []
  const cleaned = rule ? JSON.stringify(rule) : ''

  // The quick form edits the rule text, so the text stays the one source.
  const build = (patch: Partial<Rule>) => {
    const base = parsed.rule && !parsed.rule.every_minutes ? parsed.rule : { times: ['09:00'], tz: audience }

    setRuleText(JSON.stringify({ ...base, ...patch }))
  }
  const days = rule?.weekdays ?? []

  return (
    <div className="grid gap-6 *:min-w-0">
      <fieldset className="flex flex-wrap gap-2">
        <legend className="sr-only">What to check</legend>
        {(
          [
            ['rule', 'A Staqly schedule rule'],
            ['list', 'A list of dates and times'],
          ] as const
        ).map(([id, label]) => (
          <button
            key={id}
            type="button"
            aria-pressed={mode === id}
            onClick={() => setMode(id)}
            className={`rounded-full border px-3.5 py-1.5 text-sm font-medium transition-colors outline-none focus-visible:ring-2 focus-visible:ring-[color-mix(in_oklch,var(--brand)_45%,transparent)] pointer-coarse:min-h-10 ${
              mode === id
                ? 'border-[color-mix(in_oklch,var(--brand)_45%,transparent)] bg-brand-soft text-brand-strong'
                : 'border-border bg-card hover:bg-accent'
            }`}
          >
            {label}
          </button>
        ))}
      </fieldset>

      <div className="grid gap-4 *:min-w-0 lg:grid-cols-2">
        <section className="grid content-start gap-4 rounded-2xl border border-border bg-card p-5 sm:p-6">
          {mode === 'rule' ? (
            <>
              <fieldset className="grid gap-2">
                <legend className="text-sm font-medium">Days</legend>
                <div className="flex flex-wrap gap-1.5">
                  {WEEK.map(([n, label]) => {
                    const on = days.length === 0 || days.includes(n)

                    return (
                      <button
                        key={n}
                        type="button"
                        aria-pressed={on}
                        onClick={() => {
                          const current = days.length ? days : WEEK.map(([d]) => d)
                          const next = on ? current.filter((d) => d !== n) : [...current, n]

                          build({ weekdays: next.length === 7 ? undefined : next.toSorted((a, b) => a - b) })
                        }}
                        className={`h-9 min-w-11 rounded-lg border px-2 text-sm font-medium outline-none focus-visible:ring-2 focus-visible:ring-[color-mix(in_oklch,var(--brand)_45%,transparent)] pointer-coarse:h-11 ${
                          on
                            ? 'border-transparent bg-brand-soft text-brand-strong'
                            : 'border-border text-muted-foreground'
                        }`}
                      >
                        {label}
                      </button>
                    )
                  })}
                </div>
              </fieldset>
              <label className="grid gap-1.5 text-sm font-medium">
                Times of day, comma-separated
                <input
                  className={fieldClass}
                  defaultValue={(rule?.times ?? ['09:00', '18:00']).join(', ')}
                  key={rule?.times?.join(',') ?? 'times'}
                  placeholder="09:00, 18:00"
                  onBlur={(e) => {
                    const times = e.target.value
                      .split(',')
                      .map((t) => t.trim())
                      .filter(Boolean)

                    build({ times })
                  }}
                />
              </label>
              <label className="grid gap-1.5 text-sm font-medium">
                The schedule’s time zone
                <Select value={rule?.tz ?? audience} onChange={(e) => build({ tz: e.target.value })}>
                  {all.map((z) => (
                    <option key={z} value={z}>
                      {z === mine ? `${z} (yours)` : z}
                    </option>
                  ))}
                </Select>
              </label>
              <label className="grid gap-1.5 text-sm font-medium">
                Or paste a rule
                <textarea
                  className={`${fieldClass} min-h-24 font-mono text-xs`}
                  value={text}
                  spellCheck={false}
                  aria-invalid={errors.length > 0}
                  aria-describedby="rule-help"
                  onChange={(e) => setRuleText(e.target.value)}
                />
                <span id="rule-help" className="text-xs font-normal text-muted-foreground">
                  The rule a collection schedule takes: times, weekdays (0 is Sunday) and tz, or every_minutes.
                </span>
              </label>
            </>
          ) : (
            <label className="grid gap-1.5 text-sm font-medium">
              Dates and times, one per line
              <textarea
                className={`${fieldClass} min-h-40 font-mono text-xs`}
                value={listText}
                spellCheck={false}
                aria-invalid={errors.length > 0}
                aria-describedby="list-help"
                onChange={(e) => setListText(e.target.value)}
              />
              <span id="list-help" className="text-xs font-normal text-muted-foreground">
                In the audience’s time zone, such as 2026-11-03 18:00; a time ending in Z or +02:00 is taken as is.
              </span>
            </label>
          )}

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

          <fieldset className="grid gap-2">
            <legend className="text-sm font-medium">Platforms it posts to</legend>
            <div className="flex flex-wrap gap-1.5">
              {GUIDANCE.map((g) => {
                const on = platforms.includes(g.id)

                return (
                  <button
                    key={g.id}
                    type="button"
                    aria-pressed={on}
                    onClick={() => setPlatforms(on ? platforms.filter((p) => p !== g.id) : [...platforms, g.id])}
                    className={`inline-flex h-9 items-center gap-1.5 rounded-full border px-3 text-sm outline-none focus-visible:ring-2 focus-visible:ring-[color-mix(in_oklch,var(--brand)_45%,transparent)] pointer-coarse:h-11 ${
                      on ? 'border-transparent bg-brand-soft text-brand-strong' : 'border-border text-muted-foreground'
                    }`}
                  >
                    <ProviderIcon id={g.id} name={g.name} className="size-3.5" />
                    {g.name}
                  </button>
                )
              })}
            </div>
          </fieldset>
        </section>

        <section
          className="grid content-start gap-4 rounded-2xl border border-border bg-card p-5 sm:p-6"
          aria-labelledby="results"
          aria-live="polite"
        >
          <h2 id="results" className="font-semibold tracking-tight">
            What we found
          </h2>
          {errors.length > 0 ? (
            <ul className="grid gap-2 text-sm">
              {errors.map((e) => (
                <li key={e} className="flex gap-2.5">
                  <span className="mt-0.5 shrink-0">{ICON.error}</span>
                  <span>
                    <span className="sr-only">{LABEL.error}: </span>
                    {e}
                  </span>
                </li>
              ))}
            </ul>
          ) : (
            <ul className="grid gap-3 text-sm">
              {[
                ...checks,
                ...(skipped.length
                  ? [
                      {
                        level: 'warn' as const,
                        title: 'A time the clocks skip',
                        detail: `${skipped.join(', ')}: that hour doesn’t exist in ${rule?.tz}, so the post goes out an hour later that day.`,
                      },
                    ]
                  : []),
              ].map((c) => (
                <li key={c.title} className="flex gap-2.5">
                  <span className="mt-0.5 shrink-0">{ICON[c.level]}</span>
                  <span className="min-w-0">
                    <span className="sr-only">{LABEL[c.level]}: </span>
                    <span className="font-medium">{c.title}</span>
                    {c.detail && <span className="mt-0.5 block text-muted-foreground">{c.detail}</span>}
                  </span>
                </li>
              ))}
            </ul>
          )}

          {rule && errors.length === 0 && (
            <div className="grid gap-2 border-t border-border pt-4">
              <h3 className="text-sm font-semibold">The rule for Staqly</h3>
              <div className="relative">
                <pre className="rounded-xl border border-border bg-background p-3 pr-12 font-mono text-xs break-all whitespace-pre-wrap">
                  {cleaned}
                </pre>
                <CopyButton text={cleaned} label="Copy rule" className="absolute top-2 right-2" />
              </div>
            </div>
          )}
        </section>
      </div>

      {errors.length === 0 && runs.length > 0 && (
        <section className="rounded-2xl border border-border bg-card p-5 sm:p-6" aria-labelledby="next-runs">
          <h2 id="next-runs" className="font-semibold tracking-tight">
            The next {Math.min(RUNS, runs.length)} posts
          </h2>
          <div className="mt-3 overflow-x-auto">
            <table className="w-full min-w-[18rem] text-left text-sm">
              <thead className="text-xs text-muted-foreground">
                <tr>
                  <th scope="col" className="py-2 pr-4 font-medium">
                    Audience ({audience})
                  </th>
                  <th scope="col" className="py-2 font-medium">
                    You ({mine})
                  </th>
                </tr>
              </thead>
              <tbody className="font-mono text-xs tabular-nums sm:text-sm">
                {runs.slice(0, RUNS).map((r) => (
                  <tr key={r.getTime()} className="border-t border-border">
                    <td className="py-2 pr-4">{fmt(r, audience)}</td>
                    <td className="py-2 text-muted-foreground">{fmt(r, mine)}</td>
                  </tr>
                ))}
              </tbody>
            </table>
          </div>
        </section>
      )}
    </div>
  )
}
