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

describe('a gateway error', () => {
  it('says the server is not answering instead of a bare status or the proxy text', () => {
    for (const status of [502, 503, 504]) {
      expect(errorMessage(new ApiError(status, undefined, 'Bad Gateway'))).toBe(
        "The staqly server isn't answering right now (it may be restarting); try again in a moment",
      )
    }
  })

  it("keeps the API's own message when it sent one", () => {
    expect(errorMessage(new ApiError(503, { code: 'maintenance', message: 'down for maintenance' }))).toBe(
      'Down for maintenance',
    )
  })
})
