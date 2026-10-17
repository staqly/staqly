// The background queues end to end: the webhook outbox (retries, giving up,
// manual retry, collection filter), notification channels (ntfy to the
// receiver, retried, one-tap approve link), watchers (RSS from the receiver)
// and the decision long-poll. Settings: WEBHOOK_RETRY_BASE, NOTIFY_RETRY_BASE,
// NOTIFY_ALLOW_PRIVATE and WATCHER_ALLOW_PRIVATE in e2e/e2e.env.
import { APIError, type Collection, type Delivery, type Webhook } from '@staqly/sdk'
import { afterAll, beforeAll, describe, expect, it } from 'vitest'

import { sdk, unique } from '../lib/clients.js'
import { http } from '../lib/http.js'
import { API, RECEIVER, RECEIVER_INTERNAL } from '../lib/stack.js'
import { sleep, waitFor } from '../lib/wait.js'

interface Received {
  path: string
  status: number
  headers: Record<string, string>
  body: string
}

const agent = sdk()

async function received(path: string): Promise<Received[]> {
  return (await http(`${RECEIVER}/requests`)).json<Received[]>().filter((r) => r.path === path)
}

async function failNext(n: number): Promise<void> {
  await http(`${RECEIVER}/reset`, { method: 'POST' })
  await http(`${RECEIVER}/fail?n=${n}`, { method: 'POST' })
}

async function status(promise: Promise<unknown>): Promise<number> {
  try {
    await promise

    return 200
  } catch (err) {
    if (err instanceof APIError) {
      return err.status
    }

    throw err
  }
}

describe('webhook outbox', () => {
  let watched: Collection
  let other: Collection
  let webhook: Webhook

  beforeAll(async () => {
    watched = await agent.collections.create({ name: unique('Outbox') })
    other = await agent.collections.create({ name: unique('Outbox other') })
    webhook = await agent.webhooks.create({
      url: `${RECEIVER_INTERNAL}/outbox`,
      events: ['item.created', 'item.rejected'],
      collection_ids: [watched.id],
      max_attempts: 3,
    })
  })

  afterAll(async () => {
    await agent.webhooks.delete(webhook.id)
  })

  it('refuses unknown event patterns (422)', async () => {
    const bad = agent.webhooks.create({ url: `${RECEIVER_INTERNAL}/x`, events: ['item.aproved'] })

    expect(await status(bad)).toBe(422)
    expect(await status(agent.webhooks.update(webhook.id, { events: ['items.*'] }))).toBe(422)
  })

  it('gives up after max_attempts and delivers on a manual retry', async () => {
    await failNext(10)
    const item = await agent.items.create(watched.id, { title: 'outbox', content: 'retry me' })

    const failed = await waitFor(
      async () => {
        const [latest] = await agent.webhooks.deliveries.list({ webhook_id: webhook.id, item_id: item.id })

        return latest?.status === 'failed' ? latest : undefined
      },
      { message: 'delivery did not fail after max_attempts' },
    )

    expect(failed.event_type).toBe('item.created')
    expect(failed.attempts).toBe(3)
    expect((await received('/outbox')).map((r) => r.status)).toEqual([500, 500, 500])

    await http(`${RECEIVER}/fail?n=0`, { method: 'POST' })
    await agent.webhooks.deliveries.retry(failed.id)

    const ok: Delivery = await waitFor(
      async () => {
        const [latest] = await agent.webhooks.deliveries.list({ webhook_id: webhook.id, item_id: item.id })

        return latest?.status === 'succeeded' ? latest : undefined
      },
      { message: 'manual retry did not succeed' },
    )

    expect(ok.id).toBe(failed.id)
    expect((await received('/outbox')).map((r) => r.status)).toEqual([500, 500, 500, 200])
  })

  it('delivers only subscribed events of the subscribed collections', async () => {
    await failNext(0)
    const outside = await agent.items.create(other.id, { title: 'not watched' })
    const inside = await agent.items.create(watched.id, { title: 'watched' })

    await agent.items.approve(inside.id) // item.approved is not subscribed
    await agent.items.reject((await agent.items.create(watched.id, { title: 'nope' })).id, { feedback: 'off-brand' })

    const types = await waitFor(
      async () => {
        const got = (await received('/outbox')).map((r) => JSON.parse(r.body) as { type: string })

        return got.length >= 3 ? got.map((e) => e.type) : undefined
      },
      { message: 'expected three deliveries' },
    )

    await sleep(1500) // nothing else may arrive
    expect((await received('/outbox')).length).toBe(3)
    expect(types.toSorted()).toEqual(['item.created', 'item.created', 'item.rejected'])
    expect(await agent.webhooks.deliveries.list({ webhook_id: webhook.id, item_id: outside.id })).toHaveLength(0)
  })
})

describe('notification channels', () => {
  it('pushes a new pending item to ntfy after a retry, and its approve link decides it', async () => {
    await failNext(1)
    const collection = await agent.collections.create({ name: unique('Notify') })
    const channel = await agent.notificationChannels.create({
      name: unique('Phone'),
      type: 'ntfy',
      config: { server_url: RECEIVER_INTERNAL, topic: 'e2e' },
      collection_ids: [collection.id],
    })

    try {
      const item = await agent.items.create(collection.id, { title: 'Review on the go', content: 'from ntfy' })
      const pushes = await waitFor(
        async () => {
          const got = await received('/')

          return got.some((r) => r.status === 200) ? got : undefined
        },
        { message: 'ntfy push did not succeed', timeout: 60_000 },
      )

      expect(pushes.map((r) => r.status)).toEqual([500, 200])

      const push = JSON.parse(pushes[1]!.body) as {
        topic: string
        title: string
        actions: { label: string; url: string }[]
      }

      expect(push.topic).toBe('e2e')
      expect(push.title).toContain('Review on the go')

      const approve = push.actions.find((a) => a.label === 'Approve')!

      expect(approve.url.startsWith(`${API}/v1/decide/`)).toBe(true)
      expect((await http(approve.url, { method: 'POST' })).status).toBe(200)
      expect((await agent.items.get(item.id)).status).toBe('approved')
      expect((await http(approve.url, { method: 'POST' })).status).toBe(410)

      const [delivery] = await agent.notificationChannels.deliveries(channel.id)

      expect(delivery).toMatchObject({ status: 'sent' })
    } finally {
      await agent.notificationChannels.delete(channel.id)
    }
  })
})

describe('watchers', () => {
  const rss = (titles: string[]): string =>
    `<?xml version="1.0"?><rss version="2.0"><channel><title>E2E feed</title>${titles
      .map((t, i) => `<item><title>${t}</title><link>https://example.com/${i}</link><guid>e2e-${t}</guid></item>`)
      .join('')}</channel></rss>`

  it('turns new RSS entries into items once, with keyword filters', async () => {
    const collection = await agent.collections.create({ name: unique('Watched') })

    await http(`${RECEIVER}/feed`, { method: 'PUT', body: rss(['staqly 1.0 released', 'unrelated news']) })

    const watcher = await agent.watchers.create({
      name: unique('Feed'),
      url: `${RECEIVER_INTERNAL}/feed.xml`,
      collection: collection.id,
      keywords: ['staqly'],
    })

    try {
      expect(watcher.kind).toBe('rss')
      // The first run records what is already there (baseline) without creating items.
      await agent.watchers.run(watcher.id)
      expect((await agent.items.list({ collection_id: collection.id })).data).toHaveLength(0)

      await http(`${RECEIVER}/feed`, {
        method: 'PUT',
        body: rss(['staqly 1.0 released', 'unrelated news', 'staqly 1.1 released', 'more unrelated news']),
      })
      await agent.watchers.run(watcher.id)
      await agent.watchers.run(watcher.id) // seen entries are not created twice

      const items = (await agent.items.list({ collection_id: collection.id })).data

      expect(items.map((i) => i.title)).toEqual(['staqly 1.1 released'])
      expect(items[0]).toMatchObject({ status: 'pending', url: 'https://example.com/2' })
    } finally {
      await agent.watchers.delete(watcher.id)
    }
  })
})

describe('decision long-poll', () => {
  it('waits for a rejection over REST', async () => {
    const collection = await agent.collections.create({ name: unique('Wait') })
    const item = await agent.items.create(collection.id, { title: 'wait for me' })
    const waiting = agent.items.waitForDecision(item.id, { timeout: 20_000 })

    await sleep(1000)
    await agent.items.reject(item.id, { feedback: 'redo' })

    expect(await waiting).toMatchObject({ id: item.id, status: 'rejected', feedback: 'redo' })
  })

  it('returns undecided when the wait times out', async () => {
    const collection = await agent.collections.create({ name: unique('Wait timeout') })
    const item = await agent.items.create(collection.id, { title: 'nobody looks' })
    const started = Date.now()
    const res = await agent.items.wait(item.id, { timeout: 1 })

    expect(res.decided).toBe(false)
    expect(res.item.status).toBe('pending')
    expect(Date.now() - started).toBeLessThan(10_000)
  })

  it('refuses an image item without media or url (422)', async () => {
    const collection = await agent.collections.create({ name: unique('Media rule') })

    expect(await status(agent.items.create(collection.id, { type: 'image', title: 'no file' }))).toBe(422)
  })
})
