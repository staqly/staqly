const rtf = new Intl.RelativeTimeFormat('en', { numeric: 'auto' })

const UNITS: [Intl.RelativeTimeFormatUnit, number][] = [
  ['year', 60 * 60 * 24 * 365],
  ['month', 60 * 60 * 24 * 30],
  ['week', 60 * 60 * 24 * 7],
  ['day', 60 * 60 * 24],
  ['hour', 60 * 60],
  ['minute', 60],
]

/**
 * A date format built on first use and reused: `new Intl.DateTimeFormat` costs far more than `format()`, and
 * `toLocaleString(undefined, opts)` builds one per call (hundreds per calendar or feed render). Same output: the
 * viewer's locale and time zone, read when the format is first needed.
 */
export function lazyDateFormat(opts: Intl.DateTimeFormatOptions) {
  let f: Intl.DateTimeFormat | undefined

  return () => (f ??= new Intl.DateTimeFormat(undefined, opts))
}

const dateTimeFormat = lazyDateFormat({
  year: 'numeric',
  month: 'short',
  day: 'numeric',
  hour: '2-digit',
  minute: '2-digit',
})

export function parseDate(value?: string | null): Date | null {
  if (!value) {
    return null
  }

  const d = new Date(value)

  return Number.isNaN(d.getTime()) ? null : d
}

/** "3 minutes ago", "in 2 hours", "just now". */
export function timeAgo(value?: string | null, now: number = Date.now()): string {
  const d = parseDate(value)

  if (!d) {
    return '—'
  }

  const diff = (d.getTime() - now) / 1000
  const abs = Math.abs(diff)

  if (abs < 45) {
    return diff <= 0 ? 'just now' : 'in a few seconds'
  }

  for (const [unit, secs] of UNITS) {
    if (abs >= secs || unit === 'minute') {
      return rtf.format(Math.round(diff / secs), unit)
    }
  }

  return d.toLocaleString()
}

export function formatDateTime(value?: string | null): string {
  const d = parseDate(value)

  if (!d) {
    return '—'
  }

  return dateTimeFormat().format(d)
}

const zonedFormats = new Map<string, Intl.DateTimeFormat>()

/**
 * A date and time in a given time zone (the workspace's), with the zone's short name: "Oct 27, 2026, 10:39 PM
 * GMT+1". Without a zone, or with one the browser doesn't know, the viewer's own.
 */
export function formatDateTimeIn(value?: string | null, timeZone?: string | null): string {
  const d = parseDate(value)

  if (!d) {
    return '—'
  }

  const zone = timeZone || ''
  let f = zonedFormats.get(zone)

  if (!f) {
    const opts: Intl.DateTimeFormatOptions = {
      year: 'numeric',
      month: 'short',
      day: 'numeric',
      hour: '2-digit',
      minute: '2-digit',
      timeZoneName: 'short',
    }

    try {
      f = new Intl.DateTimeFormat(undefined, zone ? { ...opts, timeZone: zone } : opts)
    } catch {
      f = new Intl.DateTimeFormat(undefined, opts)
    }

    zonedFormats.set(zone, f)
  }

  return f.format(d)
}

export function formatBytes(bytes?: number | null): string {
  if (bytes === undefined || bytes === null) {
    return '—'
  }

  if (bytes < 1024) {
    return `${bytes} B`
  }

  const units = ['KB', 'MB', 'GB', 'TB']
  let v = bytes / 1024
  let i = 0

  while (v >= 1024 && i < units.length - 1) {
    v /= 1024
    i++
  }

  return `${v.toFixed(v < 10 ? 1 : 0).replace(/\.0$/, '')} ${units[i]}`
}

let numberFormat: Intl.NumberFormat | undefined

export function pluralize(n: number, one: string, many = one + 's') {
  numberFormat ??= new Intl.NumberFormat()

  return `${numberFormat.format(n)} ${n === 1 ? one : many}`
}

export function downloadText(filename: string, text: string) {
  downloadBlob(filename, new Blob([text], { type: 'text/plain;charset=utf-8' }))
}

/** Saves a file made in the browser; its object URL is released once the download has started. */
export function downloadBlob(filename: string, blob: Blob) {
  const url = URL.createObjectURL(blob)
  const a = document.createElement('a')

  a.href = url
  a.download = filename
  document.body.appendChild(a)
  a.click()
  a.remove()
  setTimeout(() => URL.revokeObjectURL(url), 1000)
}

/** Seconds → compact human interval: 900 → "15 min", 3600 → "1 h", 5400 → "1 h 30 min", 86400 → "1 day". */
export function formatInterval(seconds: number): string {
  if (!Number.isFinite(seconds) || seconds <= 0) {
    return '—'
  }

  if (seconds < 60) {
    return `${Math.round(seconds)} s`
  }

  const days = Math.floor(seconds / 86400)
  const hours = Math.floor((seconds % 86400) / 3600)
  const mins = Math.round((seconds % 3600) / 60)

  if (days && !hours && !mins) {
    return days === 1 ? '1 day' : `${days} days`
  }

  const parts: string[] = []

  if (days) {
    parts.push(`${days} d`)
  }

  if (hours) {
    parts.push(`${hours} h`)
  }

  if (mins) {
    parts.push(`${mins} min`)
  }

  return parts.join(' ')
}
