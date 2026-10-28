import { request } from '@/lib/api'
import type { components } from '@/lib/api-types'

// The analytics page's data: GET /v1/analytics and the formatting its sections share. Kept out of lib/api.ts so
// the request and its types load with this page only.

export type Analytics = components['schemas']['Analytics']
export type AnalyticsTotals = components['schemas']['AnalyticsTotals']
export type AnalyticsMetrics = components['schemas']['AnalyticsMetrics']
export type ChannelAnalytics = components['schemas']['ChannelAnalytics']
export type TimeSlot = components['schemas']['AnalyticsTimeSlot']
export type SourceAnalytics = Analytics['sources'][number]
export type DayAnalytics = Analytics['daily'][number]

/** What the page asks for, as the URL keeps it: a preset (7, 30, 90 days) or a custom range of days. */
export interface AnalyticsParams {
  days?: number
  from?: string
  to?: string
  channel?: string
  source?: string
}

/** The two series colours (data-viz palette: categorical slot 1, status critical), light and dark steps. */
export const VIZ_COLORS =
  '[--viz-published:#2a78d6] [--viz-failed:#d03b3b] [--viz-grid:#e1e0d9] [--viz-axis:#c3c2b7] dark:[--viz-published:#3987e5] dark:[--viz-grid:#2c2c2a] dark:[--viz-axis:#383835]'

export const PRESETS = [7, 30, 90] as const

export function analyticsQueryString(p: AnalyticsParams): string {
  const sp = new URLSearchParams()

  if (p.from && p.to) {
    sp.set('from', p.from)
    sp.set('to', p.to)
  } else {
    sp.set('days', String(p.days ?? 30))
  }

  if (p.channel) {
    sp.set('channel', p.channel)
  }

  if (p.source !== undefined) {
    sp.set('source', p.source)
  }

  return sp.toString()
}

export function fetchAnalytics(p: AnalyticsParams, signal?: AbortSignal) {
  return request<Analytics>(`/v1/analytics?${analyticsQueryString(p)}`, { signal })
}

let compact: Intl.NumberFormat | undefined
let whole: Intl.NumberFormat | undefined
let percent: Intl.NumberFormat | undefined

/** 1,284 / 12.9K / 4.2M. */
export function formatCount(n: number): string {
  if (Math.abs(n) < 10_000) {
    whole ??= new Intl.NumberFormat('en')

    return whole.format(n)
  }

  compact ??= new Intl.NumberFormat('en', { notation: 'compact', maximumFractionDigits: 1 })

  return compact.format(n)
}

/** 0.953 → "95.3%"; null → "—". */
export function formatRate(r: number | null | undefined): string {
  if (r === null || r === undefined) {
    return '—'
  }

  percent ??= new Intl.NumberFormat('en', { style: 'percent', maximumFractionDigits: 1 })

  return percent.format(r)
}

/** Seconds of delay → "on time" under a second, else "42 s", "3 min", "1 h 20 min". */
export function formatDelay(s: number | null | undefined): string {
  if (s === null || s === undefined) {
    return '—'
  }

  if (s < 1) {
    return 'on time'
  }

  if (s < 60) {
    return `${Math.round(s)} s`
  }

  if (s < 3600) {
    return `${Math.round(s / 60)} min`
  }

  const h = Math.floor(s / 3600)
  const m = Math.round((s % 3600) / 60)

  return m ? `${h} h ${m} min` : `${h} h`
}

export const WEEKDAYS = ['Monday', 'Tuesday', 'Wednesday', 'Thursday', 'Friday', 'Saturday', 'Sunday']
export const WEEKDAYS_SHORT = ['Mon', 'Tue', 'Wed', 'Thu', 'Fri', 'Sat', 'Sun']

export function hourLabel(h: number): string {
  return `${String(h).padStart(2, '0')}:00`
}

/** "Oct 12" for a YYYY-MM-DD day (read as a calendar day, never shifted by the viewer's zone). */
const dayFormat = new Map<string, Intl.DateTimeFormat>()

export function formatDay(key: string, opts: Intl.DateTimeFormatOptions = { month: 'short', day: 'numeric' }): string {
  const id = JSON.stringify(opts)
  let f = dayFormat.get(id)

  if (!f) {
    f = new Intl.DateTimeFormat(undefined, { ...opts, timeZone: 'UTC' })
    dayFormat.set(id, f)
  }

  return f.format(new Date(`${key}T00:00:00Z`))
}

/** Where posts come from, in words. */
export const SOURCE_KINDS: Record<SourceAnalytics['kind'], string> = {
  dashboard: 'Dashboard',
  api: 'API',
  mcp: 'MCP',
  schedule: 'Schedule',
  repeat: 'Repeat',
  other: 'Other',
}

/** The change of a count against the previous period, or null when that period had nothing to compare with. */
export function change(now: number, before: number, hadData: boolean): number | null {
  if (!hadData) {
    return null
  }

  return now - before
}
