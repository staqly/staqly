// An agent writes a post with a picture for two channels over REST; a
// signed-in browser reads it, schedules it and moves it through the
// dashboard's /api proxy (session + CSRF). The dashboard serves the new
// routes and redirects the old ones.
import { beforeAll, describe, expect, it } from 'vitest'

import { api, type Channel, fixture, type Media, type Post, signInBrowser, unique } from '../lib/clients.js'
import { http, type Session } from '../lib/http.js'
import { DASHBOARD } from '../lib/stack.js'

const AVATAR = fixture('avatar0.png')
const hour = (n: number) => new Date(Date.now() + n * 3600_000).toISOString()

describe('posts and the dashboard', () => {
  const agent = api()
  let browser: Session
  let collection: string
  let blog: Channel
  let social: Channel
  let picture: Media
  let post: Post

  beforeAll(async () => {
    browser = await signInBrowser()
    collection = (await agent.collection({ name: unique('Launch posts') })).id
    blog = await agent.customChannel(unique('Blog'))
    social = await agent.customChannel(unique('Social'))
  })

  it('an agent uploads a picture and writes a draft for two channels', async () => {
    picture = await agent.upload(AVATAR, 'avatar.png')
    expect(picture).toMatchObject({ mime_type: 'image/png', size_bytes: AVATAR.length })
    post = await agent.createPost(collection, {
      content: 'Spring sale starts Monday\nThree days, every avatar half price.',
      media: [picture.id],
      targets: [{ channel_id: blog.id }, { channel_id: social.id, content: 'Half price avatars from Monday' }],
      tags: ['launch', 'sale'],
      metadata: { campaign: 'spring' },
      source: 'e2e',
    })

    expect(post).toMatchObject({ status: 'draft', publish_at: null, version: 1, collection_id: collection })
    expect(post.media).toEqual([picture.id])
    expect(post.media_items[0]).toMatchObject({ id: picture.id, mime_type: 'image/png' })
    expect(post.targets.map((t) => [t.channel_name, t.status, t.content])).toEqual([
      [blog.name, 'pending', null],
      [social.name, 'pending', 'Half price avatars from Monday'],
    ])
    expect(post.targets[0]!.provider).toBe('custom')
    const bytes = await agent.raw(`/v1/media/${picture.id}`)

    expect(bytes.status).toBe(200)
    expect(bytes.body.equals(AVATAR)).toBe(true)
  })

  it('renders the post, schedule, posts and channels pages', async () => {
    for (const path of [`/posts/${post.id}`, '/schedule', '/posts', '/channels', '/alerts']) {
      expect((await http(`${DASHBOARD}${path}`)).status).toBe(200)
    }
  })

  it('redirects the routes of the review era to the new ones', async () => {
    for (const [from, to] of [
      ['/inbox', '/schedule'],
      ['/notifications', '/alerts'],
      ['/collections', '/posts'],
      [`/collections/${collection}`, `/posts?collection=${collection}`],
    ]) {
      const res = await http(`${DASHBOARD}${from}`)

      expect(res.status).toBe(301)
      expect(res.headers.location).toBe(to)
    }
  })

  it('serves the post and its picture to the browser through the proxy', async () => {
    const res = await browser.request(`/api/v1/posts/${post.id}`)

    expect(res.status).toBe(200)
    expect(res.json<Post>().content).toContain('Spring sale')
    const media = await browser.request(`/api/v1/media/${picture.id}`)

    expect(media.status).toBe(200)
    expect(media.headers['content-type']).toBe('image/png')
    // The calendar's query: posts by publish time; the collection's own listing too.
    const listed = await browser.request(`/api/v1/collections/${collection}/posts?status=draft`)

    expect(listed.json<{ data: Post[] }>().data.map((p) => p.id)).toEqual([post.id])
  })

  it('streams a 12 MB upload through the proxy without truncating it', async () => {
    // A valid PNG header followed by zero padding.
    const big = new Uint8Array(8 + (12 << 20))

    big.set([0x89, 0x50, 0x4e, 0x47, 0x0d, 0x0a, 0x1a, 0x0a])
    const form = new FormData()

    form.set('file', new Blob([big], { type: 'image/png' }), 'big.png')
    const res = await browser.request('/api/v1/media', { method: 'POST', form })

    expect(res.status).toBe(201)
    expect(res.json<Media>().size_bytes).toBe(big.length)
  })

  it('the browser schedules it with session + CSRF, then moves and clears the time', async () => {
    const at = hour(2)
    const res = await browser.request(`/api/v1/posts/${post.id}/schedule`, { method: 'POST', json: { publish_at: at } })

    expect(res.status).toBe(200)
    expect(res.json<Post>()).toMatchObject({ status: 'scheduled' })
    expect(Date.parse(res.json<Post>().publish_at!)).toBe(Date.parse(at))
    // Moving the time is an edit; clearing it makes the post a draft again.
    const moved = await browser.request(`/api/v1/posts/${post.id}`, { method: 'PATCH', json: { publish_at: hour(3) } })

    expect(moved.status).toBe(200)
    expect(moved.json<Post>().status).toBe('scheduled')
    const cleared = await browser.request(`/api/v1/posts/${post.id}/cancel`, { method: 'POST', json: {} })

    expect(cleared.status).toBe(200)
    expect(cleared.json<Post>()).toMatchObject({ status: 'draft' })
    // Cancelling a draft is a conflict; a time in the past or without a channel is refused.
    expect((await browser.request(`/api/v1/posts/${post.id}/cancel`, { method: 'POST', json: {} })).status).toBe(409)
    const past = await browser.request(`/api/v1/posts/${post.id}/schedule`, {
      method: 'POST',
      json: { publish_at: '2020-01-01T00:00:00Z' },
    })

    expect(past.status).toBe(422)
    const noChannel = await agent.createPost(collection, { content: 'nowhere to go' })

    expect(
      (await browser.request(`/api/v1/posts/${noChannel.id}/schedule`, { method: 'POST', json: { publish_at: at } }))
        .status,
    ).toBe(422)
  })

  it('an edit based on an old version conflicts', async () => {
    const current = await agent.getPost(post.id)
    const ok = await agent.patch<Post>(`/v1/posts/${post.id}`, {
      content: 'Spring sale starts Monday\nEdited by the agent.',
      expected_version: current.version,
    })

    expect(ok.version).toBe(current.version + 1)
    const stale = await browser.request(`/api/v1/posts/${post.id}`, {
      method: 'PATCH',
      json: { content: 'Edited in a stale tab', expected_version: current.version },
    })

    expect(stale.status).toBe(409)
    expect(stale.json<{ error: { code: string; message: string } }>().error.message).toMatch(/modified/)
    expect((await agent.getPost(post.id)).content).toContain('Edited by the agent')
  })

  it('records the history', async () => {
    const types = (await agent.history(post.id)).map((e) => e.type)

    // Newest first: the content edit, the cancel, the move (an edit that re-schedules), the first schedule.
    expect(types).toEqual([
      'post.updated',
      'post.unscheduled',
      'post.scheduled',
      'post.updated',
      'post.scheduled',
      'post.created',
    ])
    const scheduled = (await agent.history(post.id)).find((e) => e.type === 'post.scheduled')!

    expect(scheduled.actor.type).toBe('session')
  })

  it('duplicates into a fresh draft and deletes', async () => {
    const copy = await agent.post<Post>(`/v1/posts/${post.id}/duplicate`)

    expect(copy.id).not.toBe(post.id)
    expect(copy).toMatchObject({
      status: 'draft',
      content: (await agent.getPost(post.id)).content,
      media: [picture.id],
    })
    expect(copy.targets.map((t) => t.channel_id)).toEqual([blog.id, social.id])
    expect((await agent.raw(`/v1/posts/${copy.id}`, { method: 'DELETE' })).status).toBe(204)
    expect((await agent.raw(`/v1/posts/${copy.id}`)).status).toBe(404)
    // The collection counts its posts by status.
    const col = await agent.get<{ post_count: number; status_counts: Record<string, number> }>(
      `/v1/collections/${collection}`,
    )

    expect(col.post_count).toBe(2)
    expect(col.status_counts.draft).toBe(2)
  })
})
