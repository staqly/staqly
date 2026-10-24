import type { CaptureResult } from 'posthog-js'
import { describe, expect, it } from 'vitest'

import { captureError, scrub, withoutQuery } from '@/lib/analytics'

describe('scrub', () => {
  it('drops the query and the fragment of every URL an event carries', () => {
    const event = {
      event: '$pageview',
      properties: {
        $current_url: 'https://app.staqly.ai/invite?token=secret',
        $referrer: 'https://app.staqly.ai/#token=stk_local_x',
        $pathname: '/invite',
      },
      $set_once: { $initial_current_url: 'https://app.staqly.ai/connect?code=abc&state=xyz' },
    } as unknown as CaptureResult

    expect(scrub(event)).toMatchObject({
      properties: {
        $current_url: 'https://app.staqly.ai/invite',
        $referrer: 'https://app.staqly.ai/',
        $pathname: '/invite',
      },
      $set_once: { $initial_current_url: 'https://app.staqly.ai/connect' },
    })
    expect(JSON.stringify(event)).not.toMatch(/secret|stk_local|code=|state=/)
  })

  it('passes a dropped event and non-string values through', () => {
    expect(scrub(null)).toBeNull()
    expect(withoutQuery(undefined)).toBeUndefined()
  })
})

describe('captureError', () => {
  it('does nothing while analytics are off', () => {
    expect(() => captureError(new Error('boom'), 'page')).not.toThrow()
  })
})
