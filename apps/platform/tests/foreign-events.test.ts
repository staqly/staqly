import { QueryClient } from '@tanstack/react-query'
import { describe, expect, it } from 'vitest'

import type { StashEvent } from '@/lib/api'
import { isForeignEvent, removePostFromLists, syncPostInCache } from '@/lib/queries'

import { post } from './fixtures'

const event = (type: string, post_id: string | null, actor = 'local') =>
  ({ id: 'evt', seq: 1, type, post_id, actor: { type: actor, id: 'a', name: 'a' } }) as unknown as StashEvent

// An event as the API sends it for a post: the post rides along, its version included.
const at = (version: number) =>
  ({ ...event('post.scheduled', 'pst_saved', 'api_key'), data: { post: { version } } }) as unknown as StashEvent

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

  it('a later change to a post this tab just saved is news, its own save is not', () => {
    syncPostInCache(new QueryClient(), post({ id: 'pst_saved', version: 4 }))

    expect(isForeignEvent(at(4))).toBe(false)
    // An agent rescheduled it within the minute: a later version than this tab wrote.
    expect(isForeignEvent(at(5))).toBe(true)
  })
})
