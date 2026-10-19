// Cloud signup creates an isolated workspace on a Pro trial; the Free plan
// allows 50 posts a month. Needs the cloud-mode stack that 07-cloud-mode
// leaves behind.
import { beforeAll, describe, expect, it } from 'vitest'

import { api, type Post } from '../lib/clients.js'
import { withKey } from '../lib/cloud.js'
import { http } from '../lib/http.js'
import { API, server, SETUP_TOKEN } from '../lib/stack.js'

describe('cloud signup and plans', () => {
  let signup: { workspace: { id: string; plan: string }; api_key: { key: string } }

  beforeAll(async () => {
    if ((await api().setup()).mode !== 'cloud') {
      throw new Error('08-signup-and-plans needs the cloud-mode stack from 07-cloud-mode')
    }
  })

  it('refuses signup without the SETUP_TOKEN', async () => {
    expect((await http(`${API}/v1/workspaces`, { method: 'POST', json: { name: 'Nope' } })).status).toBe(403)
  })

  it('signs up a new workspace on a Pro trial', async () => {
    const res = await http(`${API}/v1/workspaces`, {
      method: 'POST',
      json: { name: 'Second', setup_token: SETUP_TOKEN },
    })

    expect(res.status).toBe(201)
    signup = res.json<typeof signup>()
    expect(signup.workspace.plan).toBe('pro')
    expect(signup.api_key.key).toMatch(/^stk_/)
  })

  it('the new workspace sees no other data, but has its Default collection', async () => {
    const owner = api(signup.api_key.key)

    expect(await owner.listPosts()).toHaveLength(0)
    expect((await owner.get<{ data: unknown[] }>('/v1/channels')).data).toHaveLength(0)
    expect((await owner.get<{ data: { name: string }[] }>('/v1/collections')).data.map((c) => c.name)).toEqual([
      'Default',
    ])
  })

  it('reports the trial in usage', async () => {
    const usage = await api(signup.api_key.key).get<{
      plan: { id: string }
      plan_expires_at: string | null
      suspended: boolean
    }>('/v1/workspace/usage')

    expect(usage.plan.id).toBe('pro')
    expect(usage.plan_expires_at).not.toBeNull()
    expect(usage.suspended).toBe(false)
  })

  it('on Free: 50 posts a month, then 402 until the operator upgrades', async () => {
    await server('set-plan', signup.workspace.id, 'free')
    const key = signup.api_key.key

    for (let i = 1; i <= 50; i++) {
      // 50+ requests in a row hit the rate limit; withKey waits out each 429.
      const res = await withKey(key, '/v1/posts', {
        method: 'POST',
        json: { collection: 'Default', content: `post ${i}` },
      })

      expect(res.status).toBe(201)
    }

    const over = await withKey(key, '/v1/posts', {
      method: 'POST',
      json: { collection: 'Default', content: 'post 51' },
    })

    expect(over.status).toBe(402)
    expect(
      over.json<{ error: { code: string; details: { limit: string; max: number; used: number } } }>().error,
    ).toMatchObject({
      code: 'plan_limit_exceeded',
      details: { limit: 'posts_per_month', max: 50, used: 50 },
    })
    const usage = (await withKey(key, '/v1/workspace/usage')).json<{ usage: Record<string, number> }>().usage

    expect(usage.posts_per_month).toBe(50)

    await server('set-plan', signup.workspace.id, 'pro')
    const after = await withKey(key, '/v1/posts', {
      method: 'POST',
      json: { collection: 'Default', content: 'post 51' },
    })

    expect(after.status).toBe(201)
    expect(after.json<Post>().status).toBe('draft')
  })
})
