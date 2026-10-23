import { describe, expect, it } from 'vitest'

import { splitConnectError } from '@/components/channels/connect-error-dialog'

describe('splitConnectError', () => {
  it("moves the platform's text after the first colon to the details, without the platform's name", () => {
    expect(
      splitConnectError(
        'the platform did not accept these details: Instagram: Unsupported request - method type: get',
        'Instagram',
      ),
    ).toEqual({
      summary: 'The platform did not accept these details',
      detail: 'Unsupported request - method type: get',
    })
  })

  it('without a colon, the parenthesised part is the detail', () => {
    expect(splitConnectError('Mastodon refused the sign-in (invalid_grant)', 'Mastodon')).toEqual({
      summary: 'Mastodon refused the sign-in',
      detail: 'invalid_grant',
    })
  })
})
