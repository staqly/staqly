// Full-text search over posts, and connecting a phone on the LAN: it reaches
// the dashboard with a non-local Host and is refused until it redeems a QR
// link, which works exactly once.
import { beforeAll, describe, expect, it } from 'vitest'

import { api, type Post, signInBrowser, unique } from '../lib/clients.js'
import { Session } from '../lib/http.js'
import { DASHBOARD } from '../lib/stack.js'

describe('full-text search', () => {
  const agent = api()

  it('finds a post by a word prefix, within a collection too', async () => {
    const collection = await agent.collection({ name: unique('Search') })
    const post = await agent.createPost(collection.id, { content: 'Quokka avatar\nsmiling at the camera' })

    expect((await agent.listPosts({ q: 'quokk' })).map((p) => p.id)).toContain(post.id)
    expect((await agent.listPosts({ q: 'camer', collection_id: collection.id }))[0]?.id).toBe(post.id)
    expect(await agent.listPosts({ q: 'zebra', collection_id: collection.id })).toHaveLength(0)
  })
})

describe('connect a device', () => {
  const phoneHost = { Host: '192.168.1.20:18080' }
  let browser: Session
  let phone: Session
  let post: Post
  let token: string

  beforeAll(async () => {
    const agent = api()
    const collection = await agent.collection({ name: unique('Devices') })
    const channel = await agent.customChannel(unique('Phone channel'))

    post = await agent.createPost(collection.id, {
      content: 'Schedule me from the phone',
      targets: [{ channel_id: channel.id }],
    })
    browser = await signInBrowser()
    phone = new Session(DASHBOARD, phoneHost)
  })

  it('refuses a phone that is not connected', async () => {
    expect((await phone.request('/api/v1/collections')).status).toBe(403)
  })

  it('the browser creates a QR link and the phone redeems it', async () => {
    const res = await browser.request('/api/v1/device-links', { method: 'POST', json: {} })

    expect(res.status).toBe(201)
    token = res.json<{ url: string }>().url.split('#')[1]!

    expect((await phone.redeemDeviceLink(token)).status).toBe(201)
  })

  it('the connected phone reads and acts', async () => {
    expect((await phone.request('/api/v1/collections')).status).toBe(200)
    const at = new Date(Date.now() + 86400_000).toISOString()
    const scheduled = await phone.request(`/api/v1/posts/${post.id}/schedule`, {
      method: 'POST',
      json: { publish_at: at },
    })

    expect(scheduled.status).toBe(200)
    expect(scheduled.json<Post>().status).toBe('scheduled')
    const cancelled = await phone.request(`/api/v1/posts/${post.id}/cancel`, { method: 'POST', json: {} })

    expect(cancelled.json<Post>().status).toBe('draft')
  })

  it('the QR link works only once', async () => {
    const other = new Session(DASHBOARD, phoneHost)

    expect((await other.redeemDeviceLink(token)).status).toBe(410)
  })
})
