import { describe, expect, it } from 'vitest'

import { formatBytes, formatDate, formatMoney, formatNumber } from './format'

describe('format', () => {
  it('formats money from minor units', () => {
    expect(formatMoney(1999, 'usd')).toBe('$19.99')
    expect(formatMoney(0, 'eur')).toBe('€0.00')
  })

  it('formats dates and shows a dash for empty values', () => {
    expect(formatDate('')).toBe('—')
    expect(formatDate(null)).toBe('—')
    expect(formatDate('nope')).toBe('—')
    expect(formatDate('2026-11-01T10:30:00Z')).toContain('2026')
  })

  it('formats bytes', () => {
    expect(formatBytes(0)).toBe('0 B')
    expect(formatBytes(2 * 1024 ** 3)).toBe('2 GB')
    expect(formatBytes(1536)).toBe('1.5 KB')
  })

  it('groups numbers', () => {
    expect(formatNumber(1234567)).toBe('1,234,567')
  })
})
