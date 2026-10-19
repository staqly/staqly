// Webhook delivery in depth (local mode, the receiver service on the compose
// network): the exact payload and headers, HMAC verification done by hand
// (tampering, wrong or rotated secrets, stale timestamps), retries keep the
// delivery and event IDs, patterns and "*" (never audit events),
// pause/resume, a slow receiver hitting timeout_ms, a receiver that is gone
// for a while (deliveries give up, a manual retry succeeds) and the test
// endpoint. Settings: WEBHOOK_RETRY_BASE=1s, WEBHOOK_RETRY_MAX=2s.
import { afterAll, afterEach, beforeAll, beforeEach, describe, expect, it } from 'vitest'

import {
  api,
  type Channel,
  type Collection,
  type Delivery,
  isValidSignature,
  sign,
  unique,
  type Webhook,
} from '../lib/clients.js'
import { publishNow } from '../lib/flow.js'
import { http } from '../lib/http.js'
import { delayReceiver, received, resetReceiver, startReceiver, stopReceiver } from '../lib/receiver.js'
import { API, RECEIVER_INTERNAL } from '../lib/stack.js'
import { sleep, waitFor } from '../lib/wait.js'

const agent = api()
let collection: Collection
let channel: Channel
const cleanup: string[] = []

interface Payload {
  id: string
  type: string
  created_at: string
  workspace_id: string
  delivery_id: string
  attempt: number
  actor: { type: string; id: string; name: string }
  data: { post?: { id: string; status: string; content: string } }
}

async function hook(path: string, events: string[], extra: Record<string, unknown> = {}): Promise<Webhook> {
  const webhook = await agent.webhook({
    url: `${RECEIVER_INTERNAL}${path}`,
    events,
    collection_ids: [collection.id],
    ...extra,
  })

  cleanup.push(webhook.id)

  return webhook
}

const draft = (content: string) => agent.createPost(collection.id, { content })

async function settled(webhookId: string, postId?: string): Promise<Delivery> {
  return waitFor(
    async () => {
      const [latest] = await agent.deliveries({ webhook_id: webhookId, ...(postId ? { post_id: postId } : {}) })

      return latest && latest.status !== 'pending' ? latest : undefined
    },
    { message: `delivery of ${webhookId} did not finish`, timeout: 60_000 },
  )
}

beforeAll(async () => {
  collection = await agent.collection({ name: unique('Delivery') })
  channel = await agent.customChannel(unique('Delivery channel'))
})

beforeEach(async () => {
  await resetReceiver(0)
})

// Every test's webhooks go away after it, so the next test's events (and the
// receiver's failure budget) are not shared with them.
afterEach(async () => {
  for (const id of cleanup.splice(0)) {
    await agent.del(`/v1/webhooks/${id}`).catch(() => undefined)
  }
})

afterAll(async () => {
  await resetReceiver(0)
})

describe('payload and signature', () => {
  it('posts the documented headers and body, signed with HMAC-SHA256 over "<ts>.<body>"', async () => {
    const webhook = await hook('/shape', ['post.created'])
    const post = await draft('Shape ✓ 日本語\nbody')
    const [req] = await waitFor(async () => {
      const got = await received('/shape')

      return got.length > 0 ? got : undefined
    })
    const h = req!.headers
    const body = JSON.parse(req!.body) as Payload

    expect(h['content-type']).toBe('application/json')
    expect(h['user-agent']).toBe('Staqly-Webhooks/1.0')
    expect(h['x-staqly-event']).toBe('post.created')
    expect(h['x-staqly-event-id']).toBe(body.id)
    expect(h['x-staqly-delivery']).toBe(body.delivery_id)
    expect(Math.abs(Number(h['x-staqly-timestamp']) - Date.now() / 1000)).toBeLessThan(60)
    expect(body).toMatchObject({
      type: 'post.created',
      attempt: 1,
      actor: { type: 'local' },
      data: { post: { id: post.id, status: 'draft', content: 'Shape ✓ 日本語\nbody' } },
    })
    expect(body.workspace_id).toMatch(/^ws_/)
    expect(body.id).toMatch(/^evt_/)
    expect(h['x-staqly-signature']).toBe(sign(webhook.secret, h['x-staqly-timestamp']!, req!.body))
    expect(isValidSignature(webhook.secret, req!.body, h)).toBe(true)

    // Tampering with anything signed breaks it.
    expect(isValidSignature(webhook.secret, req!.body.replace('Shape', 'Shap3'), h)).toBe(false)
    expect(isValidSignature('whsec_wrong', req!.body, h)).toBe(false)
    expect(
      isValidSignature(webhook.secret, req!.body, {
        ...h,
        'x-staqly-timestamp': String(Number(h['x-staqly-timestamp']) + 1),
      }),
    ).toBe(false)
    // A replayed old delivery is outside the tolerance.
    expect(isValidSignature(webhook.secret, req!.body, h, { now: Number(h['x-staqly-timestamp']) + 3600 })).toBe(false)
  })

  it('never shows the secret again, and a rotated secret signs from then on', async () => {
    const webhook = await hook('/rotate', ['post.created'])

    expect((await agent.get<Webhook>(`/v1/webhooks/${webhook.id}`)).secret).not.toBe(webhook.secret)
    expect(
      (await agent.get<{ data: Webhook[] }>('/v1/webhooks')).data.find((w) => w.id === webhook.id)?.secret,
    ).toMatch(/^whsec_\*{4}/)

    const rotated = await agent.patch<Webhook>(`/v1/webhooks/${webhook.id}`, { rotate_secret: true })

    expect(rotated.secret).toMatch(/^whsec_/)
    expect(rotated.secret).not.toBe(webhook.secret)
    await draft('after rotation')
    const [req] = await waitFor(async () => {
      const got = await received('/rotate')

      return got.length > 0 ? got : undefined
    })

    expect(isValidSignature(rotated.secret, req!.body, req!.headers)).toBe(true)
    expect(isValidSignature(webhook.secret, req!.body, req!.headers)).toBe(false)
  })

  it('a retry is the same delivery and event, with the attempt counted up and a fresh signature', async () => {
    await resetReceiver(1)
    const webhook = await hook('/retry', ['post.created'])

    await draft('retry me')
    const delivery = await settled(webhook.id)
    const [first, second] = await received('/retry')
    const a = JSON.parse(first!.body) as Payload
    const b = JSON.parse(second!.body) as Payload

    expect(delivery).toMatchObject({ status: 'succeeded', attempts: 2, last_status_code: 200 })
    expect([first!.status, second!.status]).toEqual([500, 200])
    expect([a.attempt, b.attempt]).toEqual([1, 2])
    expect(b.id).toBe(a.id)
    expect(b.delivery_id).toBe(a.delivery_id)
    expect(second!.headers['x-staqly-delivery']).toBe(delivery.id)
    expect(isValidSignature(webhook.secret, second!.body, second!.headers)).toBe(true)
  })
})

describe('subscriptions', () => {
  it('matches exact types and globs only', async () => {
    // Every collection: post.scheduled is emitted without a collection, so a collection filter would miss it.
    const webhook = await agent.webhook({
      url: `${RECEIVER_INTERNAL}/patterns`,
      events: ['post.scheduled', 'post.*_requested'],
    })

    cleanup.push(webhook.id)
    const post = await agent.createPost(collection.id, { content: 'patterns', targets: [{ channel_id: channel.id }] })
    const mine = async () =>
      (await received('/patterns')).filter((r) => (JSON.parse(r.body) as Payload).data.post?.id === post.id)

    await agent.patch(`/v1/posts/${post.id}`, { content: 'patterns, edited' }) // post.updated: not subscribed
    await agent.schedule(post.id, new Date(Date.now() + 3600_000).toISOString()) // post.scheduled
    await publishNow(agent, post.id) // post.publish_requested (one target)
    await waitFor(async () => (await mine()).length >= 2)
    await sleep(2500)

    const types = (await mine()).map((r) => (JSON.parse(r.body) as Payload).type)

    expect(types.toSorted()).toEqual(['post.publish_requested', 'post.scheduled'])
    expect((await agent.deliveries({ webhook_id: webhook.id, post_id: post.id })).length).toBe(2)
  })

  it('"*" gets every domain event but never audit or webhook events; naming the family gets audit events', async () => {
    const all = await agent.webhook({ url: `${RECEIVER_INTERNAL}/star`, events: ['*'] })
    const audit = await agent.webhook({ url: `${RECEIVER_INTERNAL}/audit`, events: ['device_link.*'] })

    cleanup.push(all.id, audit.id)
    // Audit events: a device link created; webhook events: an update of the webhook itself.
    const link = (await http(`${API}/v1/device-links`, { method: 'POST' })).json<{ url: string }>()

    expect(link.url).toContain('#')
    await agent.patch(`/v1/webhooks/${all.id}`, { description: 'touched' })
    await draft('domain event')
    await waitFor(async () => (await received('/star')).length >= 1 && (await received('/audit')).length >= 1)
    await sleep(2500)

    const star = (await received('/star')).map((r) => (JSON.parse(r.body) as Payload).type)

    expect(star).toContain('post.created')
    expect(star.filter((t) => /^(api_key|session|recovery_key|device|device_link|webhook)\./.test(t))).toEqual([])
    expect((await received('/audit')).map((r) => (JSON.parse(r.body) as Payload).type)).toEqual(['device_link.created'])
  })

  it('a paused webhook queues nothing; resumed, it delivers new events only', async () => {
    const webhook = await hook('/paused', ['post.created'])

    await agent.patch(`/v1/webhooks/${webhook.id}`, { active: false })
    const during = await draft('while paused')

    await sleep(2500)
    expect(await received('/paused')).toHaveLength(0)
    expect(await agent.deliveries({ webhook_id: webhook.id })).toHaveLength(0)

    await agent.patch(`/v1/webhooks/${webhook.id}`, { active: true })
    const after = await draft('after resume')
    const delivered = await settled(webhook.id)

    expect(delivered.status).toBe('succeeded')
    const ids = (await received('/paused')).map((r) => (JSON.parse(r.body) as Payload).data.post?.id)

    expect(ids).toEqual([after.id])
    expect(ids).not.toContain(during.id)
  })
})

describe('failure modes', () => {
  it('a receiver slower than timeout_ms fails the attempts; once it is fast, a manual retry succeeds', async () => {
    const webhook = await hook('/slow', ['post.created'], { timeout_ms: 1000, max_attempts: 2 })

    await delayReceiver(3000)
    const post = await draft('slow')
    const failed = await settled(webhook.id, post.id)

    expect(failed).toMatchObject({ status: 'failed', attempts: 2, last_status_code: null })
    expect(failed.last_error).toMatch(/deadline|timeout|Timeout/)

    await delayReceiver(0)
    await agent.post(`/v1/webhook-deliveries/${failed.id}/retry`)
    const ok = await waitFor(
      async () => {
        const d = (await agent.deliveries({ webhook_id: webhook.id }))[0]

        return d?.status === 'succeeded' ? d : undefined
      },
      { message: 'manual retry after the slow receiver did not succeed' },
    )

    expect(ok).toMatchObject({ id: failed.id, attempts: 1, last_status_code: 200 })
    // The receiver saw all three attempts (the slow ones were answered too late).
    expect((await received('/slow')).length).toBe(3)
  })

  it('a receiver that is gone: deliveries give up after max_attempts, a manual retry delivers once it is back', async () => {
    const webhook = await hook('/down', ['post.created'], { max_attempts: 3 })

    await stopReceiver()

    try {
      const post = await draft('nobody home')
      const failed = await settled(webhook.id, post.id)

      expect(failed).toMatchObject({ status: 'failed', attempts: 3, last_status_code: null })
      expect(failed.last_error).not.toBe('')
      await startReceiver()
      await agent.post(`/v1/webhook-deliveries/${failed.id}/retry`)
      const ok = await settled(webhook.id, post.id)

      expect(ok.status).toBe('succeeded')
      const got = await received('/down')

      expect(got.map((r) => r.status)).toEqual(Array(got.length).fill(200))
      expect(new Set(got.map((r) => (JSON.parse(r.body) as Payload).id)).size).toBe(1)
    } finally {
      await startReceiver().catch(() => undefined)
    }
  })

  it('the test endpoint sends a signed webhook.test delivery', async () => {
    const webhook = await hook('/ping', ['post.published'])
    const delivery = await agent.post<Delivery>(`/v1/webhooks/${webhook.id}/test`)

    expect(delivery.event_type).toBe('webhook.test')
    const [req] = await waitFor(async () => {
      const got = await received('/ping')

      return got.length > 0 ? got : undefined
    })

    expect(req!.headers['x-staqly-event']).toBe('webhook.test')
    expect(isValidSignature(webhook.secret, req!.body, req!.headers)).toBe(true)
  })

  it('refuses invalid settings (422)', async () => {
    const bad = [
      { url: 'ftp://receiver/x', events: ['post.created'] },
      { url: 'not a url', events: ['post.created'] },
      { url: `${RECEIVER_INTERNAL}/x`, events: [] },
      { url: `${RECEIVER_INTERNAL}/x`, events: ['post.created'], timeout_ms: 10 },
      { url: `${RECEIVER_INTERNAL}/x`, events: ['post.created'], timeout_ms: 600_000 },
      { url: `${RECEIVER_INTERNAL}/x`, events: ['post created'] },
    ]

    for (const json of bad) {
      expect((await http(`${API}/v1/webhooks`, { method: 'POST', json })).status).toBe(422)
    }
  })
})
