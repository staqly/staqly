// Plan limits as the API stores them (whole numbers, -1 = unlimited) and as an admin reads and types them: bytes in
// GB or MB, AI spend in dollars, data_export on or off.

export const UNLIMITED = -1

type Unit = { label: string; factor: number }

const UNITS: Record<string, Unit> = {
  storage_bytes: { label: 'GB', factor: 1024 ** 3 },
  max_upload_bytes: { label: 'MB', factor: 1024 ** 2 },
  ai_monthly_cents: { label: '$', factor: 100 },
}

export const isSwitch = (key: string) => key === 'data_export'

export function unitOf(key: string): Unit | undefined {
  return UNITS[key]
}

/** A stored value as an admin reads it: "Unlimited", "10 GB", "$2", "on", "1,500". */
export function formatLimit(key: string, v: number): string {
  if (isSwitch(key)) {
    return v ? 'on' : 'off'
  }

  if (v === UNLIMITED) {
    return 'Unlimited'
  }

  const u = unitOf(key)

  if (!u) {
    return v.toLocaleString('en-US')
  }

  const n = Number((v / u.factor).toFixed(2)).toLocaleString('en-US')

  return u.label === '$' ? `$${n}` : `${n} ${u.label}`
}

/** What the input shows for a stored value (in the limit's unit). */
export function toInput(key: string, v: number): string {
  const u = unitOf(key)

  return String(u ? Number((v / u.factor).toFixed(2)) : v)
}

/** The stored value for what was typed; null when it is not a number of zero or more. */
export function fromInput(key: string, text: string): number | null {
  const n = Number(text.trim())

  if (text.trim() === '' || !Number.isFinite(n) || n < 0) {
    return null
  }

  return Math.round(n * (unitOf(key)?.factor ?? 1))
}
