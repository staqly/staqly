import { describe, expect, it } from 'vitest'

import { WaitTimeoutError } from '../src/index.js'
import { client, json, post, target } from './helpers.js'

describe('waitForPublished', () => {
  it('polls until the post is published', async () => {
    const { c, calls } = client(
      json(post({ status: 'publishing' })),
      json(post({ status: 'publishing' })),
      json(post({ status: 'published', targets: [target({ status: 'published', external_url: 'https://x/1' })] })),
    )
    const seen: string[] = []
    const out = await c.posts.waitForPublished('pst_1', { pollInterval: 100, onPoll: (p) => seen.push(p.status) })

    expect(out.status).toBe('published')
    expect(out.targets[0]!.external_url).toBe('https://x/1')
    expect(seen).toEqual(['publishing', 'publishing', 'published'])
    expect(calls.map((x) => x.path)).toEqual(['/v1/posts/pst_1', '/v1/posts/pst_1', '/v1/posts/pst_1'])
  })

  it('returns a failed post instead of throwing', async () => {
    const { c } = client(
      json(post({ status: 'failed', targets: [target({ status: 'failed', error: 'token expired' })] })),
    )
    const out = await c.posts.waitForPublished('pst_1')

    expect(out.status).toBe('failed')
    expect(out.targets[0]!.error).toBe('token expired')
  })

  it('throws WaitTimeoutError while still publishing at the deadline', async () => {
    const { c } = client(json(post({ status: 'publishing' })))
    const err = (await c.posts
      .waitForPublished('pst_1', { timeout: 1, pollInterval: 100 })
      .catch((e) => e)) as WaitTimeoutError

    expect(err).toBeInstanceOf(WaitTimeoutError)
    expect(err.id).toBe('pst_1')
    expect(err.post?.status).toBe('publishing')
  })

  it('can be aborted', async () => {
    const { c } = client(json(post({ status: 'publishing' })))
    const ctrl = new AbortController()
    const p = c.posts.waitForPublished('pst_1', { pollInterval: 10_000, signal: ctrl.signal })

    setTimeout(() => ctrl.abort(), 20)
    await expect(p).rejects.toThrow()
  })
})
