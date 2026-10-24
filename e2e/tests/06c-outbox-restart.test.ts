// The webhook outbox survives a crash (SIGKILL, no graceful shutdown):
// deliveries queued behind a slow receiver are delivered after the restart
// without loss or duplicates, and the deliveries that were in flight when the
// process died stay pending under their lease instead of being lost, and go
// out once it runs out (WEBHOOK_LEASE, 65 s in e2e.env).
//
// The worker sends at most two requests per workspace at once, so two
// deliveries held open by a slow receiver keep five others queued and
// untouched; then the API container is killed and started again.
import { afterAll, beforeAll, describe, expect, it } from 'vitest'

import { api, type Collection, type Delivery, unique, type Webhook } from '../lib/clients.js'
import { delayReceiver, received, resetReceiver } from '../lib/receiver.js'
import { API, API_CONTAINER, docker, RECEIVER_INTERNAL, waitForHttp } from '../lib/stack.js'
import { waitFor } from '../lib/wait.js'

const agent = api()

describe('webhook outbox across a crash', () => {
  let slowCol: Collection
  let queuedCol: Collection
  let slow: Webhook
  let queued: Webhook
  let killedAt: number
  const queuedPosts: string[] = []

  beforeAll(async () => {
    await resetReceiver(0)
    slowCol = await agent.collection({ name: unique('Held') })
    queuedCol = await agent.collection({ name: unique('Queued') })
    slow = await agent.webhook({
      url: `${RECEIVER_INTERNAL}/held`,
      events: ['post.created'],
      collection_ids: [slowCol.id],
      timeout_ms: 60_000,
    })
  })

  afterAll(async () => {
    await delayReceiver(0)
    await agent.del(`/v1/webhooks/${slow.id}`).catch(() => undefined)
    await agent.del(`/v1/webhooks/${queued.id}`).catch(() => undefined)
  })

  it('queues deliveries behind two held ones', async () => {
    await delayReceiver(120_000)
    await agent.createPost(slowCol.id, { content: 'held 1' })
    await agent.createPost(slowCol.id, { content: 'held 2' })
    await waitFor(async () => (await received('/held')).length === 2, { message: 'held deliveries not in flight' })

    queued = await agent.webhook({
      url: `${RECEIVER_INTERNAL}/queued`,
      events: ['post.created'],
      collection_ids: [queuedCol.id],
    })

    for (let i = 0; i < 5; i++) {
      queuedPosts.push((await agent.createPost(queuedCol.id, { content: `queued ${i}` })).id)
    }

    const pending = await agent.deliveries({ webhook_id: queued.id })

    expect(pending).toHaveLength(5)
    expect(pending.every((d) => d.status === 'pending' && d.attempts === 0)).toBe(true)
  })

  it('survives SIGKILL and delivers every queued event exactly once', async () => {
    await docker(['kill', '--signal', 'KILL', API_CONTAINER])
    killedAt = Date.now()
    await delayReceiver(0)
    await docker(['start', API_CONTAINER])
    await waitForHttp(`${API}/health`)

    const done: Delivery[] = await waitFor(
      async () => {
        const list = await agent.deliveries({ webhook_id: queued.id })

        return list.length === 5 && list.every((d) => d.status === 'succeeded') ? list : undefined
      },
      { message: 'queued deliveries were not delivered after the restart', timeout: 60_000 },
    )

    expect(done.every((d) => d.attempts === 1)).toBe(true)
    const got = (await received('/queued')).map(
      (r) => (JSON.parse(r.body) as { data: { post: { id: string } } }).data.post.id,
    )

    expect(got.toSorted()).toEqual(queuedPosts.toSorted())
  })

  it('keeps the deliveries that were in flight pending under their lease (not lost)', async () => {
    const held = await agent.deliveries({ webhook_id: slow.id })

    expect(held).toHaveLength(2)

    for (const d of held) {
      expect(d.status).toBe('pending')
      expect(d.attempts).toBe(1)
      // Retried when the lease of the dead process (65 s) runs out.
      const next = Date.parse(d.next_attempt_at!)

      expect(next).toBeGreaterThan(Date.now())
      expect(next).toBeLessThanOrEqual(killedAt + 65_000 + 5_000)
    }
  })

  it('redelivers the in-flight deliveries once their lease runs out', { timeout: 180_000 }, async () => {
    const done = await waitFor(
      async () => {
        const list = await agent.deliveries({ webhook_id: slow.id })

        return list.length === 2 && list.every((d) => d.status === 'succeeded') ? list : undefined
      },
      { message: 'in-flight deliveries were not sent again after their lease', timeout: 170_000, interval: 2_000 },
    )

    expect(done.every((d) => d.attempts === 2)).toBe(true)
  })
})
