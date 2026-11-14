import { describe, expect, it } from 'vitest'

import { formatLimit, fromInput, isSwitch, toInput, UNLIMITED } from './limits'

describe('limits', () => {
  it('reads stored values in their units', () => {
    expect(formatLimit('channels', 1500)).toBe('1,500')
    expect(formatLimit('channels', -1)).toBe('Unlimited')
    expect(formatLimit('storage_bytes', 10 * 1024 ** 3)).toBe('10 GB')
    expect(formatLimit('max_upload_bytes', 25 * 1024 ** 2)).toBe('25 MB')
    expect(formatLimit('ai_monthly_cents', 250)).toBe('$2.5')
    expect(formatLimit('data_export', 0)).toBe('off')
  })

  it('turns what was typed into the stored value', () => {
    expect(fromInput('storage_bytes', '2')).toBe(2 * 1024 ** 3)
    expect(fromInput('ai_monthly_cents', '1.5')).toBe(150)
    expect(fromInput('channels', '-3')).toBeNull()
    expect(fromInput('channels', '')).toBeNull()
    expect(toInput('max_upload_bytes', 100 * 1024 ** 2)).toBe('100')
  })
})

describe('what an admin types and reads', () => {
  it('data_export is a switch: on or off, never a number or Unlimited', () => {
    expect(isSwitch('data_export')).toBe(true)
    expect(isSwitch('channels')).toBe(false)
    expect(formatLimit('data_export', 1)).toBe('on')
    expect(formatLimit('data_export', UNLIMITED)).toBe('on')
  })

  it('zero is a value (none allowed), not Unlimited and not empty', () => {
    expect(fromInput('channels', '0')).toBe(0)
    expect(fromInput('storage_bytes', ' 0 ')).toBe(0)
    expect(formatLimit('channels', 0)).toBe('0')
    expect(formatLimit('storage_bytes', 0)).toBe('0 GB')
    expect(formatLimit('ai_monthly_cents', 0)).toBe('$0')
  })

  it('refuses what is no number of zero or more: text, a minus, nothing, infinity', () => {
    for (const text of ['abc', '-1', '-0.5', '', '   ', 'Infinity', '1e999', '5 GB']) {
      expect({ text, value: fromInput('storage_bytes', text) }).toEqual({ text, value: null })
    }
  })

  it('fractions of a unit are stored as whole bytes and cents', () => {
    expect(fromInput('storage_bytes', '1.5')).toBe(1.5 * 1024 ** 3)
    expect(fromInput('max_upload_bytes', '0.5')).toBe(512 * 1024)
    expect(fromInput('ai_monthly_cents', '0.29')).toBe(29)
    expect(fromInput('ai_monthly_cents', '4.35')).toBe(435)
    expect(fromInput('channels', '2.6')).toBe(3)
  })

  it('what the input shows comes back as the same stored value', () => {
    const stored: [string, number][] = [
      ['storage_bytes', 10 * 1024 ** 3],
      ['storage_bytes', 1.5 * 1024 ** 3],
      ['max_upload_bytes', 25 * 1024 ** 2],
      ['ai_monthly_cents', 250],
      ['ai_monthly_cents', 1999],
      ['channels', 15],
      ['posts_per_month', 1500],
    ]

    for (const [key, value] of stored) {
      expect({ key, value: fromInput(key, toInput(key, value)) }).toEqual({ key, value })
    }

    expect(toInput('channels', 15)).toBe('15')
    expect(toInput('ai_monthly_cents', 1999)).toBe('19.99')
    expect(formatLimit('ai_monthly_cents', 1999)).toBe('$19.99')
    expect(formatLimit('storage_bytes', 1.5 * 1024 ** 3)).toBe('1.5 GB')
    expect(formatLimit('posts_per_month', UNLIMITED)).toBe('Unlimited')
  })
})
