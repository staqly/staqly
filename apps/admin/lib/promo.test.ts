import { describe, expect, it } from 'vitest'

import {
  couponDiscount,
  couponDuration,
  emptyPromo,
  promoPayload,
  serverPromoErrors,
  validatePromo,
  type PromoForm,
} from './promo'

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

describe('a coupon in the list', () => {
  const coupon = { percent_off: 0, amount_off: 0, currency: '', duration: 'once' as const, duration_in_months: 0 }

  it('says what it takes off and for how long', () => {
    expect(couponDiscount({ ...coupon, percent_off: 20 })).toBe('20% off')
    expect(couponDuration({ ...coupon, duration: 'repeating', duration_in_months: 3 })).toBe('3 months')
  })

  it('one deleted in Stripe says so, not "$0.00 off"', () => {
    expect(couponDiscount({ ...coupon, deleted: true })).toBe('Coupon deleted')
    expect(couponDuration({ ...coupon, deleted: true })).toBe('—')
  })
})

describe('what the form refuses before it reaches Stripe', () => {
  it('a code takes letters, digits, "-" and "_" in any case, 3 to 40 of them', () => {
    expect(validatePromo({ ...ok, code: 'abc' }, now)).toEqual({})
    expect(validatePromo({ ...ok, code: 'A_b-9' }, now)).toEqual({})
    expect(validatePromo({ ...ok, code: 'A'.repeat(40) }, now)).toEqual({})
    expect(validatePromo({ ...ok, code: 'A'.repeat(41) }, now).code).toBe('Use 3–40 letters, digits, "-" or "_".')
    expect(validatePromo({ ...ok, code: 'SAVE 20%' }, now)).toHaveProperty('code')
    expect(validatePromo({ ...ok, code: '   ' }, now)).toHaveProperty('code')
  })

  it('a percentage is a number: text and negatives are refused, with the reason', () => {
    expect(validatePromo({ ...ok, percent: 'half' }, now).percent).toBe('Enter a percentage from 1 to 100.')
    expect(validatePromo({ ...ok, percent: '-5' }, now)).toHaveProperty('percent')
    expect(validatePromo({ ...ok, percent: '0.5' }, now)).toHaveProperty('percent')
    expect(validatePromo({ ...ok, percent: '12.5' }, now)).toEqual({})
  })

  it('an amount needs a 3-letter currency and a number above 0', () => {
    const a: PromoForm = { ...ok, kind: 'amount', amount: '5', currency: 'eur' }

    expect(validatePromo(a, now)).toEqual({})
    expect(validatePromo({ ...a, currency: 'EUR' }, now)).toEqual({})
    expect(validatePromo({ ...a, currency: 'euro' }, now).currency).toBe('Enter a 3-letter currency code.')
    expect(validatePromo({ ...a, currency: '' }, now)).toHaveProperty('currency')
    expect(validatePromo({ ...a, amount: '-1' }, now).amount).toBe('Enter an amount above 0.')
    expect(validatePromo({ ...a, amount: 'ten' }, now)).toHaveProperty('amount')
    expect(validatePromo({ ...a, amount: '' }, now)).toHaveProperty('amount')
    // The percentage field is not on screen in amount mode: what it holds is neither checked nor sent.
    expect(validatePromo({ ...a, percent: '500' }, now)).toEqual({})
  })

  it('months are whole, from 1; uses are whole, above 0, or left empty', () => {
    const r: PromoForm = { ...ok, duration: 'repeating', months: '3' }

    expect(validatePromo({ ...r, months: '0' }, now).months).toBe('Enter whole months from 1 to 36.')
    expect(validatePromo({ ...r, months: '1.5' }, now)).toHaveProperty('months')
    expect(validatePromo({ ...r, months: '1' }, now)).toEqual({})
    expect(validatePromo({ ...ok, maxRedemptions: '0' }, now).maxRedemptions).toBe(
      'Enter a whole number above 0, or leave empty.',
    )
    expect(validatePromo({ ...ok, maxRedemptions: '2.5' }, now)).toHaveProperty('maxRedemptions')
    expect(validatePromo({ ...ok, maxRedemptions: 'many' }, now)).toHaveProperty('maxRedemptions')
    expect(validatePromo({ ...ok, maxRedemptions: '1' }, now)).toEqual({})
    expect(validatePromo({ ...ok, maxRedemptions: '  ' }, now)).toEqual({})
  })

  it('a code may end today: it runs to the end of that day', () => {
    expect(validatePromo({ ...ok, expiresAt: '2026-11-01' }, now)).toEqual({})
    expect(validatePromo({ ...ok, expiresAt: '2026-10-31' }, now).expiresAt).toBe('Pick a date in the future.')
    expect(promoPayload({ ...ok, expiresAt: '2026-11-01' }).expires_at).toBe(
      new Date('2026-11-01T23:59:59').toISOString(),
    )
  })

  it('several mistakes are all reported at once', () => {
    expect(
      Object.keys(validatePromo({ ...ok, code: '', percent: '', duration: 'repeating', maxRedemptions: '-1' }, now)),
    ).toEqual(['code', 'percent', 'months', 'maxRedemptions'])
  })
})

describe('what is sent for an amount', () => {
  it('is whole minor units, however the amount was typed', () => {
    const a: PromoForm = { ...ok, kind: 'amount', currency: ' USD ' }

    expect(promoPayload({ ...a, amount: '0.29' })).toMatchObject({ amount_off: 29, currency: 'usd' })
    expect(promoPayload({ ...a, amount: '4.35' })).toMatchObject({ amount_off: 435 })
    expect(promoPayload({ ...a, amount: '10' })).toMatchObject({ amount_off: 1000 })
    expect(promoPayload({ ...a, amount: '10' })).not.toHaveProperty('percent_off')
  })

  it('an amount coupon in the list reads in its currency, dollars when Stripe names none', () => {
    const coupon = {
      percent_off: 0,
      amount_off: 500,
      currency: 'eur',
      duration: 'forever' as const,
      duration_in_months: 0,
    }

    expect(couponDiscount(coupon)).toBe('€5.00 off')
    expect(couponDiscount({ ...coupon, currency: '' })).toBe('$5.00 off')
    expect(couponDuration(coupon)).toBe('forever')
    expect(couponDuration({ ...coupon, duration: 'once' })).toBe('once')
  })

  it("an amount field the server refuses lands under the amount; a percentage's under the percentage", () => {
    expect(serverPromoErrors({ amount_off: 'too large', currency: 'unknown' }, 'amount')).toEqual({
      amount: 'too large',
      currency: 'unknown',
    })
    expect(serverPromoErrors({ duration: 'unknown' }, 'percent')).toEqual({ duration: 'unknown' })
  })
})
