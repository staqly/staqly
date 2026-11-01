export type PromoForm = {
  code: string
  kind: 'percent' | 'amount'
  percent: string
  amount: string
  currency: string
  duration: 'once' | 'repeating' | 'forever'
  months: string
  maxRedemptions: string
  expiresAt: string // yyyy-mm-dd
}

export const emptyPromo: PromoForm = {
  code: '',
  kind: 'percent',
  percent: '',
  amount: '',
  currency: 'usd',
  duration: 'once',
  months: '',
  maxRedemptions: '',
  expiresAt: '',
}

const CODE = /^[A-Z0-9_-]{3,40}$/

export function validatePromo(f: PromoForm, now = new Date()): Record<string, string> {
  const e: Record<string, string> = {}
  const code = f.code.trim().toUpperCase()

  if (!CODE.test(code)) {
    e.code = 'Use 3–40 letters, digits, "-" or "_".'
  }

  if (f.kind === 'percent') {
    const p = Number(f.percent)

    if (f.percent.trim() === '' || !(p >= 1 && p <= 100)) {
      e.percent = 'Enter a percentage from 1 to 100.'
    }
  } else {
    if (!(Number(f.amount) > 0)) {
      e.amount = 'Enter an amount above 0.'
    }

    if (!/^[a-z]{3}$/i.test(f.currency.trim())) {
      e.currency = 'Enter a 3-letter currency code.'
    }
  }

  if (f.duration === 'repeating') {
    const m = Number(f.months)

    if (!Number.isInteger(m) || m < 1 || m > 36) {
      e.months = 'Enter whole months from 1 to 36.'
    }
  }

  if (f.maxRedemptions.trim() !== '' && !(Number.isInteger(Number(f.maxRedemptions)) && Number(f.maxRedemptions) > 0)) {
    e.maxRedemptions = 'Enter a whole number above 0, or leave empty.'
  }

  if (f.expiresAt && !(new Date(`${f.expiresAt}T23:59:59`).getTime() > now.getTime())) {
    e.expiresAt = 'Pick a date in the future.'
  }

  return e
}

export function promoPayload(f: PromoForm): Record<string, unknown> {
  const body: Record<string, unknown> = { code: f.code.trim().toUpperCase(), duration: f.duration }

  if (f.kind === 'percent') {
    body.percent_off = Number(f.percent)
  } else {
    body.amount_off = Math.round(Number(f.amount) * 100)
    body.currency = f.currency.trim().toLowerCase()
  }

  if (f.duration === 'repeating') {
    body.duration_in_months = Number(f.months)
  }

  if (f.maxRedemptions.trim() !== '') {
    body.max_redemptions = Number(f.maxRedemptions)
  }

  if (f.expiresAt) {
    body.expires_at = new Date(`${f.expiresAt}T23:59:59`).toISOString()
  }

  return body
}

const SERVER_FIELD: Record<string, keyof PromoForm> = {
  code: 'code',
  duration: 'duration',
  currency: 'currency',
  duration_in_months: 'months',
  max_redemptions: 'maxRedemptions',
  expires_at: 'expiresAt',
}

// serverPromoErrors maps the API's 422 `fields` (JSON names) onto the form's inputs; percent_off also reports "set exactly one".
export function serverPromoErrors(fields: Record<string, string>, kind: PromoForm['kind']): Record<string, string> {
  const e: Record<string, string> = {}

  for (const [k, msg] of Object.entries(fields)) {
    const own =
      k === 'percent_off'
        ? kind === 'percent'
          ? 'percent'
          : 'amount'
        : k === 'amount_off'
          ? 'amount'
          : SERVER_FIELD[k]

    if (own) {
      e[own] = msg
    }
  }

  return e
}
