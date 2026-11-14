import { describe, expect, it } from 'vitest'

import { cents, daysLeft } from '@/components/billing/billing-format'
import { formatBytes, formatDateTime, formatDateTimeIn, formatInterval, pluralize, timeAgo } from '@/lib/format'
import { lockReason, money } from '@/lib/plans'
import { usageMeter } from '@/lib/usage'

const NOW = Date.parse('2026-11-13T12:00:00Z')
const at = (seconds: number) => new Date(NOW + seconds * 1000).toISOString()

describe('money', () => {
  it('whole amounts have no cents, others two decimals, in the currency given (euros by default)', () => {
    expect(money(12)).toBe('€12')
    expect(money(87.5)).toBe('€87.50')
    expect(money(1500)).toBe('€1,500')
    expect(money(29, 'usd')).toBe('$29')
    expect(money(0)).toBe('€0')
  })

  it('Stripe amounts are cents', () => {
    expect(cents(2900, 'eur')).toBe('€29')
    expect(cents(973, 'eur')).toBe('€9.73')
    expect(cents(8750, 'usd')).toBe('$87.50')
    // An invoice without a currency (older rows) reads in the plans' currency.
    expect(cents(100, '')).toBe('€1')
  })
})

describe('days left', () => {
  it('rounds up, so a trial ending in 6 days and 2 hours has 7 days left, and never goes below 0', () => {
    expect(daysLeft(at(6 * 86_400 + 2 * 3600), NOW)).toBe(7)
    expect(daysLeft(at(7 * 86_400), NOW)).toBe(7)
    expect(daysLeft(at(60), NOW)).toBe(1)
    expect(daysLeft(at(0), NOW)).toBe(0)
    expect(daysLeft(at(-86_400), NOW)).toBe(0)
  })
})

describe('a usage meter', () => {
  it('is fine up to 80 %, warns above it and is full at the limit', () => {
    expect(usageMeter(0, 10)).toEqual({ level: 'ok', pct: 0 })
    expect(usageMeter(8, 10)).toEqual({ level: 'ok', pct: 80 })
    expect(usageMeter(81, 100)).toEqual({ level: 'warn', pct: 81 })
    expect(usageMeter(10, 10)).toEqual({ level: 'full', pct: 100 })
    expect(usageMeter(12, 10)).toEqual({ level: 'full', pct: 100 })
  })

  it('a limit of 0 allows none (full); -1, or none from an older server, is unlimited', () => {
    expect(usageMeter(0, 0)).toEqual({ level: 'full', pct: 100 })
    expect(usageMeter(500, -1)).toEqual({ level: 'unlimited', pct: 0 })
    expect(usageMeter(500, undefined)).toEqual({ level: 'unlimited', pct: 0 })
    expect(usageMeter(500, null)).toEqual({ level: 'unlimited', pct: 0 })
  })
})

describe('why a workspace is locked', () => {
  it('a subscription Stripe still holds waits for its payment; an ended one says whether a payment ended it', () => {
    expect(lockReason({ subscription_status: 'past_due' })).toBe('payment_due')
    expect(lockReason({ subscription_status: 'unpaid' })).toBe('payment_due')
    expect(lockReason({ subscription_status: 'canceled', ended_for: 'payment_failed' })).toBe('payment_failed')
    expect(lockReason({ subscription_status: 'canceled', ended_for: null })).toBe('ended')
    expect(lockReason({ subscription_status: null })).toBe('ended')
  })
})

describe('sizes, counts and intervals', () => {
  it('bytes read in the largest unit, with one decimal under 10', () => {
    expect(formatBytes(0)).toBe('0 B')
    expect(formatBytes(1023)).toBe('1023 B')
    expect(formatBytes(1024)).toBe('1 KB')
    expect(formatBytes(1536)).toBe('1.5 KB')
    expect(formatBytes(25 * 1024 ** 2)).toBe('25 MB')
    expect(formatBytes(4.5 * 1024 ** 3)).toBe('4.5 GB')
    expect(formatBytes(5000 * 1024 ** 4)).toBe('5000 TB')
    expect(formatBytes(null)).toBe('—')
    expect(formatBytes(undefined)).toBe('—')
  })

  it('counts take the singular for exactly one', () => {
    expect(pluralize(1, 'day')).toBe('1 day')
    expect(pluralize(0, 'day')).toBe('0 days')
    expect(pluralize(2, 'day')).toBe('2 days')
    expect(pluralize(3, 'person', 'people')).toBe('3 people')
  })

  it('intervals read compactly', () => {
    expect(formatInterval(45)).toBe('45 s')
    expect(formatInterval(900)).toBe('15 min')
    expect(formatInterval(3600)).toBe('1 h')
    expect(formatInterval(5400)).toBe('1 h 30 min')
    expect(formatInterval(86_400)).toBe('1 day')
    expect(formatInterval(3 * 86_400)).toBe('3 days')
    expect(formatInterval(86_400 + 7200)).toBe('1 d 2 h')
    expect(formatInterval(0)).toBe('—')
    expect(formatInterval(Number.NaN)).toBe('—')
  })
})

describe('dates', () => {
  it('a moment ago or ahead reads in the nearest unit', () => {
    expect(timeAgo(at(-10), NOW)).toBe('just now')
    expect(timeAgo(at(20), NOW)).toBe('in a few seconds')
    expect(timeAgo(at(-180), NOW)).toBe('3 minutes ago')
    expect(timeAgo(at(2 * 3600), NOW)).toBe('in 2 hours')
    expect(timeAgo(at(-86_400), NOW)).toBe('yesterday')
    expect(timeAgo(at(-3 * 86_400), NOW)).toBe('3 days ago')
  })

  it('no date, or one that is no date, is a dash everywhere', () => {
    for (const value of [undefined, null, '', 'not a date']) {
      expect(timeAgo(value, NOW)).toBe('—')
      expect(formatDateTime(value)).toBe('—')
      expect(formatDateTimeIn(value, 'Europe/Warsaw')).toBe('—')
    }
  })

  it("a date in the workspace's zone carries the zone; an unknown zone falls back to the viewer's", () => {
    const warsaw = formatDateTimeIn('2026-10-27T21:39:00Z', 'Europe/Warsaw')
    const tokyo = formatDateTimeIn('2026-10-27T21:39:00Z', 'Asia/Tokyo')

    expect(warsaw).toMatch(/27/)
    expect(warsaw).toMatch(/10:39/)
    expect(warsaw).toMatch(/GMT\+1|CET/)
    expect(tokyo).toMatch(/28/)
    expect(tokyo).toMatch(/06:39/)
    expect(formatDateTimeIn('2026-10-27T21:39:00Z', 'Mars/Olympus')).toBe(formatDateTimeIn('2026-10-27T21:39:00Z'))
  })
})
