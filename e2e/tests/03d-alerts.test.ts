// Alerts in depth (ntfy to the receiver service): the push for a published
// post (title, preview, link to the post), a failed one, digests that bundle
// several posts into one push, event and collection filters, the test
// endpoint, and auto-disable after NOTIFY_MAX_FAILS=3 failed sends in a row,
// then enabling again. Default events: post.published, post.failed,
// channel.needs_reconnect.
import { afterEach, beforeEach, describe, expect, it } from 'vitest'

import { type Alert, api, type Channel, type Collection, unique } from '../lib/clients.js'
import { publishNow, publishThrough } from '../lib/flow.js'
import { received, resetReceiver } from '../lib/receiver.js'
import { RECEIVER_INTERNAL } from '../lib/stack.js'
import { sleep, waitFor } from '../lib/wait.js'

const agent = api()
const alerts: string[] = []
let channel: Channel

interface Push {
  topic: string
  title: string
  message: string
  click?: string
  priority?: number
}

async function alert(topic: string, extra: Record<string, unknown> = {}): Promise<Alert> {
  const a = await agent.alert({
    name: unique(`ntfy ${topic}`),
    type: 'ntfy',
    config: { server_url: RECEIVER_INTERNAL, topic },
    ...extra,
  })

  alerts.push(a.id)

  return a
}

async function pushes(topic: string, n = 1): Promise<Push[]> {
  return waitFor(
    async () => {
      const got = (await received('/'))
        .filter((r) => r.status === 200)
        .map((r) => JSON.parse(r.body) as Push)
        .filter((p) => p.topic === topic)

      return got.length >= n ? got : undefined
    },
    { message: `no ${n} push(es) on topic ${topic}`, timeout: 60_000 },
  )
}

beforeEach(async () => {
  await resetReceiver(0)
  channel ??= await agent.customChannel(unique('Alert channel'))
})

afterEach(async () => {
  for (const id of alerts.splice(0)) {
    await agent.del(`/v1/alerts/${id}`).catch(() => undefined)
  }
})

describe('what a push says', () => {
  let collection: Collection

  beforeEach(async () => {
    collection = await agent.collection({ name: unique('Pushes') })
  })

  it('a published post: "Published: <first line>", the preview and a link to the post', async () => {
    const topic = `pub${Date.now()}`
    const a = await alert(topic, { collection_ids: [collection.id] })

    expect(a.events).toEqual(['post.published', 'post.failed', 'channel.needs_reconnect'])
    const post = await agent.createPost(collection.id, {
      content: 'Push me\nThe body of the post goes here.',
      targets: [{ channel_id: channel.id }],
    })

    await publishThrough(agent, post.id, 'https://blog.example.com/push-me')
    const [push] = await pushes(topic)

    expect(push!.title).toBe('Published: Push me')
    expect(push!.message).toContain('The body of the post goes here.')
    expect(push!.click).toMatch(new RegExp(`/posts/${post.id}$`))
    const [delivery] = await agent.alertDeliveries(a.id)

    expect(delivery).toMatchObject({ status: 'sent' })
  })

  it('a failed post: "Publish failed: <first line>" with the error', async () => {
    const topic = `fail${Date.now()}`

    await alert(topic, { collection_ids: [collection.id] })
    const post = await agent.createPost(collection.id, {
      content: 'Breaks\nbody',
      targets: [{ channel_id: channel.id }],
    })

    await publishNow(agent, post.id)
    await agent.result(post.id, channel.id, { status: 'failed', error: 'Mock API: quota exceeded' })
    const [push] = await pushes(topic)

    expect(push!.title).toBe('Publish failed: Breaks')
    expect(push!.message).toContain('Breaks')
  })

  it('post.publish_requested can be subscribed too: "Your automation is asked to publish"', async () => {
    const topic = `ask${Date.now()}`

    await alert(topic, { collection_ids: [collection.id], events: ['post.publish_requested'] })
    const post = await agent.createPost(collection.id, {
      content: 'Hand me over',
      targets: [{ channel_id: channel.id }],
    })

    await publishNow(agent, post.id)
    const [push] = await pushes(topic)

    expect(push!.title).toBe('Your automation is asked to publish: Hand me over')
  })
})

describe('alert behaviour', () => {
  it('a digest window bundles several posts into one push', async () => {
    const collection = await agent.collection({ name: unique('Digest') })
    const topic = `digest${Date.now()}`

    await alert(topic, { collection_ids: [collection.id], digest_window_seconds: 4, events: ['post.created'] })

    for (const content of ['one', 'two', 'three']) {
      await agent.createPost(collection.id, { content })
    }

    const [push] = await pushes(topic)

    await sleep(3000) // nothing else may follow
    expect(await pushes(topic)).toHaveLength(1)
    expect(push!.title).toBe('3 updates in staqly')
    expect(push!.message).toContain('one')
    expect(push!.message).toContain('three')
  })

  it('event and collection filters: other collections and unsubscribed events never push', async () => {
    const watched = await agent.collection({ name: unique('Watched') })
    const other = await agent.collection({ name: unique('Unwatched') })
    const topic = `filter${Date.now()}`

    await alert(topic, { collection_ids: [watched.id], events: ['post.published'] })
    await agent.createPost(other.id, { content: 'elsewhere' })
    const elsewhere = await agent.createPost(other.id, {
      content: 'elsewhere published',
      targets: [{ channel_id: channel.id }],
    })

    await publishThrough(agent, elsewhere.id)
    const mine = await agent.createPost(watched.id, { content: 'mine', targets: [{ channel_id: channel.id }] })

    await publishNow(agent, mine.id) // publish_requested: not subscribed
    await agent.result(mine.id, channel.id, { status: 'published', url: 'https://example.com/mine' })
    const [push] = await pushes(topic)

    await sleep(3000)
    expect(await pushes(topic)).toHaveLength(1)
    expect(push!.title).toBe('Published: mine')
  })

  it('validates the type, the events and the config (422)', async () => {
    for (const json of [
      { name: 'x', type: 'pager', config: {} },
      { name: 'x', type: 'ntfy', config: {} }, // topic required
      { name: 'x', type: 'ntfy', config: { topic: 't' }, events: ['item.approved'] },
      { name: 'x', type: 'telegram', config: { bot_token: 'abc' } }, // chat_id required
      { name: 'x', type: 'ntfy', config: { topic: 't' }, digest_window_seconds: 90_000 },
    ]) {
      expect((await agent.raw('/v1/alerts', { method: 'POST', json })).status).toBe(422)
    }
  })

  it('the test endpoint pushes a test message', async () => {
    const topic = `test${Date.now()}`
    const a = await alert(topic)
    const res = await agent.post<{ ok: boolean }>(`/v1/alerts/${a.id}/test`)

    expect(res).toMatchObject({ ok: true })
    const [push] = await pushes(topic)

    expect(push!.title).toBe('Staqly test notification')
  })

  it('is disabled after 3 failed sends in a row and can be enabled again', async () => {
    const collection = await agent.collection({ name: unique('Flaky') })
    const topic = `flaky${Date.now()}`
    const a = await alert(topic, { collection_ids: [collection.id], events: ['post.created'] })

    await resetReceiver(100)
    await agent.createPost(collection.id, { content: 'nobody receives this' })
    const disabled = await waitFor(
      async () => {
        const cur = await agent.get<Alert>(`/v1/alerts/${a.id}`)

        return cur.enabled ? undefined : cur
      },
      { message: 'alert was not disabled', timeout: 60_000 },
    )

    expect(disabled.disabled_reason).toMatch(/^disabled after 3 consecutive failures/)
    expect((await received('/')).filter((r) => r.status === 500)).toHaveLength(3)

    await resetReceiver(0)
    const enabled = await agent.patch<Alert>(`/v1/alerts/${a.id}`, { enabled: true })

    expect(enabled.enabled).toBe(true)
    expect(enabled.disabled_reason).toBe('')
    expect(enabled.fail_count).toBe(0)
    await agent.createPost(collection.id, { content: 'received again' })
    const [push] = await pushes(topic)

    // The row that failed while the alert was off goes out with it, as one push or a digest of two.
    expect([push!.title, push!.message].join('\n')).toContain('received again')
  })
})
