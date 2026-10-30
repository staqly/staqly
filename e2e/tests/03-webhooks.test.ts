// The custom channel round trip through webhooks: publishing a post on "your
// own automation" emits post.publish_requested to the subscribed webhook, the
// automation reports each target's result, and the post settles. Webhooks are
// delivered at least once and signed. The receiver service
// (e2e/receiver/server.mjs) fails the first delivery, so the retry runs too
// (WEBHOOK_RETRY_BASE=1s in e2e/e2e.env).
import { afterAll, beforeAll, describe, expect, it } from 'vitest'

import { api, type Channel, type Delivery, isValidSignature, type Post, unique, type Webhook } from '../lib/clients.js'
import { http } from '../lib/http.js'
import { RECEIVER, RECEIVER_INTERNAL } from '../lib/stack.js'
import { waitFor } from '../lib/wait.js'

interface Received {
  path: string
  status: number
  headers: Record<string, string>
  body: string
}

interface Payload {
  id: string
  type: string
  data: {
    post: Post
    collection: { id: string; name: string }
    channel?: Channel
    target?: { channel_id: string; content: string; settings: Record<string, string> }
    timeout_seconds?: number
  }
}

describe('publishing through your own automation', () => {
  const agent = api()
  let webhook: Webhook
  let blog: Channel
  let social: Channel
  let post: Post
  let delivery: Delivery

  beforeAll(async () => {
    await http(`${RECEIVER}/reset`, { method: 'POST' })
    const collection = await agent.collection({ name: unique('Webhooks') })

    blog = await agent.customChannel(unique('Blog'))
    social = await agent.customChannel(unique('Social'))
    webhook = await agent.webhook({
      url: `${RECEIVER_INTERNAL}/hook`,
      events: ['post.publish_requested'],
      collection_ids: [collection.id],
    })
    post = await agent.createPost(collection.id, {
      content: 'Ship me\nHello from the e2e suite',
      targets: [{ channel_id: blog.id }, { channel_id: social.id, content: 'Short version for social' }],
    })
  })

  afterAll(async () => {
    await agent.del(`/v1/webhooks/${webhook.id}`)
  })

  it('returns the signing secret once, on creation', () => {
    expect(webhook.secret).toMatch(/^whsec_/)
  })

  it('publish now puts the post in publishing and asks the automation once per channel', async () => {
    const res = await agent.raw(`/v1/posts/${post.id}/publish`, { method: 'POST' })

    expect(res.status).toBe(202)
    expect(res.json<Post>().status).toBe('publishing')
    const asked = await waitFor(
      async () => {
        const p = await agent.getPost(post.id)

        return p.targets.every((t) => t.status === 'publishing' && t.attempts === 1) ? p : undefined
      },
      { message: 'targets were not handed to the automation' },
    )

    expect(asked.status).toBe('publishing')
    const requested = (await agent.history(post.id)).filter((e) => e.type === 'post.publish_requested')

    expect(requested.map((e) => e.channel_id).toSorted()).toEqual([blog.id, social.id].toSorted())
    expect(requested[0]!.actor).toMatchObject({ type: 'system', id: 'publisher' })
  })

  it('delivers post.publish_requested after one failed attempt', async () => {
    delivery = await waitFor(
      async () => {
        const list = await agent.deliveries({ webhook_id: webhook.id, post_id: post.id })

        return list.length === 2 && list.every((d) => d.status === 'succeeded') ? list[0]! : undefined
      },
      { message: 'webhook deliveries did not succeed' },
    )

    expect(delivery.event_type).toBe('post.publish_requested')
    const attempts = (await agent.deliveries({ webhook_id: webhook.id, post_id: post.id })).map((d) => d.attempts)

    // The receiver failed the very first request of the run; the other delivery went through at once.
    expect(attempts.toSorted()).toEqual([1, 2])
  })

  it('signs the request and tells the automation what to publish where', async () => {
    const received = (await http(`${RECEIVER}/requests`)).json<Received[]>().filter((r) => r.path === '/hook')

    expect(received.map((r) => r.status).toSorted()).toEqual([200, 200, 500])
    const ok = received.filter((r) => r.status === 200).map((r) => ({ r, event: JSON.parse(r.body) as Payload }))

    for (const { r, event } of ok) {
      expect(isValidSignature(webhook.secret, r.body, r.headers)).toBe(true)
      expect(r.headers['x-staqly-event']).toBe('post.publish_requested')
      expect(event.data.post).toMatchObject({ id: post.id, status: 'publishing' })
      expect(event.data.collection.name).toMatch(/^Webhooks/)
      expect(event.data.channel?.provider).toBe('custom')
      expect(event.data.timeout_seconds).toBeGreaterThan(0)
    }

    const texts = Object.fromEntries(ok.map(({ event }) => [event.data.channel!.id, event.data.target!.content]))

    expect(texts).toEqual({ [blog.id]: 'Ship me\nHello from the e2e suite', [social.id]: 'Short version for social' })
  })

  it('the automation reports one success and one failure: the post is failed, with the link kept', async () => {
    const half = await agent.result(post.id, blog.id, {
      status: 'published',
      url: 'https://blog.example.com/spring-sale',
      external_id: 'blog-42',
    })

    expect(half.status).toBe('publishing')
    expect(half.targets.find((t) => t.channel_id === blog.id)).toMatchObject({
      status: 'published',
      external_url: 'https://blog.example.com/spring-sale',
      external_id: 'blog-42',
    })
    const failed = await agent.result(post.id, social.id, { status: 'failed', error: 'Social API: 503' })

    expect(failed.status).toBe('failed')
    expect(failed.published_at).toBeNull()
    expect(failed.targets.find((t) => t.channel_id === social.id)).toMatchObject({
      status: 'failed',
      error: 'Social API: 503',
    })
    // A result for a target that is not waiting is a conflict; a bad status is 422.
    expect(
      (
        await agent.raw(`/v1/posts/${post.id}/targets/${blog.id}/result`, {
          method: 'POST',
          json: { status: 'published' },
        })
      ).status,
    ).toBe(409)
    expect(
      (
        await agent.raw(`/v1/posts/${post.id}/targets/${social.id}/result`, {
          method: 'POST',
          json: { status: 'done' },
        })
      ).status,
    ).toBe(422)
    const types = (await agent.history(post.id)).map((e) => e.type)

    expect(types.slice(0, 3)).toEqual(['post.failed', 'post.target_failed', 'post.target_published'])
  })

  it('retrying the failed target asks the automation again; its success publishes the post', async () => {
    const before = (await agent.deliveries({ webhook_id: webhook.id, post_id: post.id })).length
    const res = await agent.raw(`/v1/posts/${post.id}/targets/${social.id}/retry`, { method: 'POST' })

    expect(res.status).toBe(202)
    expect(res.json<Post>().status).toBe('publishing')
    // Only a failed target can be retried.
    expect((await agent.raw(`/v1/posts/${post.id}/targets/${blog.id}/retry`, { method: 'POST' })).status).toBe(409)
    await waitFor(
      async () => (await agent.deliveries({ webhook_id: webhook.id, post_id: post.id })).length === before + 1,
      { message: 'the retry did not ask the automation again' },
    )
    const again = await waitFor(
      async () => {
        const p = await agent.getPost(post.id)

        return p.targets.find((t) => t.channel_id === social.id)?.attempts === 2 ? p : undefined
      },
      { message: 'retried target was not handed to the automation' },
    )

    expect(again.targets.find((t) => t.channel_id === social.id)).toMatchObject({ status: 'publishing', error: '' })
    const done = await agent.result(post.id, social.id, { status: 'published', url: 'https://social.example/p/1' })

    expect(done.status).toBe('published')
    expect(done.published_at).not.toBeNull()
    expect(done.targets.every((t) => t.status === 'published')).toBe(true)
    expect((await agent.history(post.id))[0]).toMatchObject({ type: 'post.published' })
    // A published post is read-only: duplicate it to post again.
    expect((await agent.raw(`/v1/posts/${post.id}`, { method: 'PATCH', json: { content: 'x' } })).status).toBe(409)
    expect((await agent.raw(`/v1/posts/${post.id}/publish`, { method: 'POST' })).status).toBe(409)
  })

  it('a target on a channel turned off meanwhile fails without asking anyone', async () => {
    const off = await agent.customChannel(unique('Off'))
    const p = await agent.createPost(post.collection_id, { content: 'nobody home', targets: [{ channel_id: off.id }] })

    await agent.patch(`/v1/channels/${off.id}`, { status: 'disabled' })
    // A new post cannot pick a channel that is off.
    expect(
      (
        await agent.raw('/v1/posts', {
          method: 'POST',
          json: { collection: post.collection_id, title: 'Post', content: 'x', targets: [{ channel_id: off.id }] },
        })
      ).status,
    ).toBe(422)
    await agent.publish(p.id)
    const failed = await waitFor(
      async () => {
        const cur = await agent.getPost(p.id)

        return cur.status === 'failed' ? cur : undefined
      },
      { message: 'post on a disabled channel did not fail' },
    )

    expect(failed.targets[0]).toMatchObject({ status: 'failed', error: 'channel is turned off' })
    expect(await agent.deliveries({ webhook_id: webhook.id, post_id: p.id })).toHaveLength(0)
  })
})
