import { describe, expect, it } from 'vitest'

import { splitDeliveryError } from '@/lib/posts'

describe('splitDeliveryError', () => {
  it('keeps the sentence and moves the platform text to the details', () => {
    expect(splitDeliveryError('LinkedIn is unavailable (502 (bad gateway)); staqly tries again')).toEqual({
      summary: 'LinkedIn is unavailable; staqly tries again',
      detail: '502 (bad gateway)',
    })
  })

  it('leaves a message without details or with an unclosed one as it is', () => {
    expect(splitDeliveryError('LinkedIn comments take text only')).toEqual({
      summary: 'LinkedIn comments take text only',
      detail: '',
    })
    expect(splitDeliveryError('Odd (unclosed')).toEqual({ summary: 'Odd (unclosed', detail: '' })
  })
})
