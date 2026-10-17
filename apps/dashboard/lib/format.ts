const rtf = new Intl.RelativeTimeFormat('en', { numeric: 'auto' })

const UNITS: [Intl.RelativeTimeFormatUnit, number][] = [
  ['year', 60 * 60 * 24 * 365],
  ['month', 60 * 60 * 24 * 30],
  ['week', 60 * 60 * 24 * 7],
  ['day', 60 * 60 * 24],
  ['hour', 60 * 60],
  ['minute', 60],
]

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

  return d.toLocaleString(undefined, {
    year: 'numeric',
    month: 'short',
    day: 'numeric',
    hour: '2-digit',
    minute: '2-digit',
  })
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

export function pluralize(n: number, one: string, many = one + 's') {
  return `${n.toLocaleString()} ${n === 1 ? one : many}`
}

/** Pretty-print a JSON string; returns the input unchanged if it is not JSON. */
export function prettyJSON(value: unknown): string {
  if (typeof value === 'string') {
    try {
      return JSON.stringify(JSON.parse(value), null, 2)
    } catch {
      return value
    }
  }

  return JSON.stringify(value ?? null, null, 2)
}

/** JSON.parse without throwing: `{ ok: false }` for invalid JSON. */
export function parseJSON(text: string): { ok: true; value: unknown } | { ok: false } {
  try {
    return { ok: true, value: JSON.parse(text) }
  } catch {
    return { ok: false }
  }
}

export function hostOf(url: string): string {
  try {
    return new URL(url).host
  } catch {
    return url
  }
}

export function truncate(s: string, n: number) {
  return s.length > n ? s.slice(0, n - 1) + '...' : s
}

export function downloadText(filename: string, text: string) {
  const blob = new Blob([text], { type: 'text/plain;charset=utf-8' })
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
