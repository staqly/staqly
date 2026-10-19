import { QueryClient } from '@tanstack/react-query'
import { describe, expect, it } from 'vitest'

import type { StashEvent } from '@/lib/api'
import { isForeignEvent, removePostFromLists } from '@/lib/queries'

const event = (type: string, post_id: string | null, actor = 'local') =>
  ({ id: 'evt', seq: 1, type, post_id, actor: { type: actor, id: 'a', name: 'a' } }) as unknown as StashEvent

describe('isForeignEvent', () => {
  it('skips what this tab wrote itself, never what happened to the post afterwards', () => {
    removePostFromLists(new QueryClient(), 'pst_mine')

    expect(isForeignEvent(event('post.updated', 'pst_mine'))).toBe(false)
    expect(isForeignEvent(event('post.updated', 'pst_other'))).toBe(true)
    // Outcomes come from the queue or the automation, even right after this tab retried the post.
    expect(isForeignEvent(event('post.published', 'pst_mine'))).toBe(true)
    expect(isForeignEvent(event('post.target_failed', 'pst_mine'))).toBe(true)
    expect(isForeignEvent(event('post.updated', 'pst_mine', 'system'))).toBe(true)
    expect(isForeignEvent(event('channel.needs_reconnect', null))).toBe(true)
    expect(isForeignEvent(event('post.updated', 'pst_x'), (id) => id === 'pst_x')).toBe(false)
  })
})
