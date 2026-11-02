import { describe, expect, it } from 'vitest'

import { formatLimit, fromInput, toInput } from './limits'

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
