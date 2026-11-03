import { describe, expect, it } from 'vitest'

import { ApiError, errorMessage } from '@/lib/api'

describe('errorMessage', () => {
  it("doesn't repeat a field's reason the message already holds", () => {
    const msg = 'media: clip.mp4 (med_1) is in Deleted: restore it to use it again'
    const err = new ApiError(422, {
      code: 'validation_failed',
      message: msg,
      fields: { media: 'clip.mp4 (med_1) is in Deleted: restore it to use it again' },
    })

    expect(errorMessage(err)).toBe('Media: clip.mp4 (med_1) is in Deleted: restore it to use it again')
  })

  it('adds the fields a general message leaves out', () => {
    const err = new ApiError(422, {
      code: 'validation_failed',
      message: 'invalid input',
      fields: { name: 'is required' },
    })

    expect(errorMessage(err)).toBe('Invalid input: name is required')
  })
})
