import { describe, expect, it } from 'vitest'

import { emptyPromo, promoPayload, serverPromoErrors, validatePromo, type PromoForm } from './promo'

const now = new Date('2026-11-01T12:00:00')
const ok: PromoForm = { ...emptyPromo, code: ' spring-20 ', percent: '20' }

describe('promoPayload', () => {
  it('upper-cases the code and sends only percent_off', () => {
    expect(promoPayload(ok)).toEqual({ code: 'SPRING-20', duration: 'once', percent_off: 20 })
  })

  it('sends amount_off in minor units with a lower-case currency, never percent_off', () => {
    const p = promoPayload({ ...ok, kind: 'amount', amount: '19.99', currency: 'EUR', percent: '20' })

    expect(p).toEqual({ code: 'SPRING-20', duration: 'once', amount_off: 1999, currency: 'eur' })
  })

  it('sends months only for repeating', () => {
    expect(promoPayload({ ...ok, duration: 'repeating', months: '3' })).toMatchObject({ duration_in_months: 3 })
    expect(promoPayload({ ...ok, duration: 'forever', months: '3' })).not.toHaveProperty('duration_in_months')
  })

  it('omits empty optional fields and sends set ones', () => {
    expect(promoPayload(ok)).not.toHaveProperty('max_redemptions')
    expect(promoPayload(ok)).not.toHaveProperty('expires_at')
    expect(promoPayload({ ...ok, maxRedemptions: '5', expiresAt: '2027-01-01' })).toMatchObject({
      max_redemptions: 5,
      expires_at: expect.any(String),
    })
  })
})

describe('validatePromo', () => {
  it('accepts a valid form', () => {
    expect(validatePromo(ok, now)).toEqual({})
  })

  it('checks the code', () => {
    expect(validatePromo({ ...ok, code: 'ab' }, now)).toHaveProperty('code')
    expect(validatePromo({ ...ok, code: 'a b c' }, now)).toHaveProperty('code')
  })

  it('checks percent 1-100', () => {
    expect(validatePromo({ ...ok, percent: '0' }, now)).toHaveProperty('percent')
    expect(validatePromo({ ...ok, percent: '101' }, now)).toHaveProperty('percent')
    expect(validatePromo({ ...ok, percent: '' }, now)).toHaveProperty('percent')
    expect(validatePromo({ ...ok, percent: '100' }, now)).toEqual({})
  })

  it('checks amount > 0 and ignores percent in amount mode', () => {
    const a: PromoForm = { ...ok, kind: 'amount', percent: '' }

    expect(validatePromo({ ...a, amount: '0' }, now)).toHaveProperty('amount')
    expect(validatePromo({ ...a, amount: '5' }, now)).toEqual({})
  })

  it('checks months 1-36 only for repeating', () => {
    const r: PromoForm = { ...ok, duration: 'repeating' }

    expect(validatePromo({ ...r, months: '' }, now)).toHaveProperty('months')
    expect(validatePromo({ ...r, months: '37' }, now)).toHaveProperty('months')
    expect(validatePromo({ ...r, months: '36' }, now)).toEqual({})
    expect(validatePromo({ ...ok, months: '99' }, now)).toEqual({})
  })

  it('requires a future expiry', () => {
    expect(validatePromo({ ...ok, expiresAt: '2026-10-01' }, now)).toHaveProperty('expiresAt')
    expect(validatePromo({ ...ok, expiresAt: '2026-12-01' }, now)).toEqual({})
  })
})

describe('serverPromoErrors', () => {
  it('maps the API field names onto the form inputs', () => {
    expect(
      serverPromoErrors({ code: 'bad', duration_in_months: 'x', max_redemptions: 'y', expires_at: 'z' }, 'percent'),
    ).toEqual({ code: 'bad', months: 'x', maxRedemptions: 'y', expiresAt: 'z' })
    expect(serverPromoErrors({ percent_off: 'one' }, 'percent')).toEqual({ percent: 'one' })
    expect(serverPromoErrors({ percent_off: 'one' }, 'amount')).toEqual({ amount: 'one' })
    expect(serverPromoErrors({ unknown: 'x' }, 'percent')).toEqual({})
  })
})
