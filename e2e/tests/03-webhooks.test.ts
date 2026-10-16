// Webhooks are delivered at least once and signed. The receiver service
// (e2e/receiver/server.mjs) fails the first delivery, so the retry runs too
// (WEBHOOK_RETRY_BASE=1s in e2e/e2e.env).
import { type Delivery, type Item, verifyWebhook, type Webhook } from '@staqly/sdk'
import { afterAll, beforeAll, describe, expect, it } from 'vitest'

import { sdk, unique } from '../lib/clients.js'
import { http } from '../lib/http.js'
import { RECEIVER, RECEIVER_INTERNAL } from '../lib/stack.js'
import { waitFor } from '../lib/wait.js'

interface Received {
  path: string
  status: number
  headers: Record<string, string>
  body: string
}

describe('webhooks', () => {
  const agent = sdk()
  let webhook: Webhook
  let item: Item
  let delivery: Delivery

  beforeAll(async () => {
    await http(`${RECEIVER}/reset`, { method: 'POST' })
    const collection = await agent.collections.create({ name: unique('Webhooks') })

    webhook = await agent.webhooks.create({
      url: `${RECEIVER_INTERNAL}/hook`,
      events: ['item.approved'],
      collection_ids: [collection.id],
    })
    item = await agent.items.create(collection.id, { title: 'Ship me', content: 'hello' })
  })

  afterAll(async () => {
    await agent.webhooks.delete(webhook.id)
  })

  it('returns the signing secret once, on creation', () => {
    expect(webhook.secret).toMatch(/^whsec_/)
  })

  it('delivers item.approved after one failed attempt', async () => {
    await agent.items.approve(item.id)

    delivery = await waitFor(
      async () => {
        const [latest] = await agent.webhooks.deliveries.list({ webhook_id: webhook.id })

        return latest?.status === 'succeeded' ? latest : undefined
      },
      { message: 'webhook delivery did not succeed' },
    )

    expect(delivery.event_type).toBe('item.approved')
    expect(delivery.attempts).toBe(2)
  })

  it('signs the request so the receiver can verify it', async () => {
    const received = (await http(`${RECEIVER}/requests`)).json<Received[]>()

    expect(received.map((r) => r.status)).toEqual([500, 200])

    const last = received[1]!
    const event = await verifyWebhook<{ item: Item }>(webhook.secret, last.body, last.headers)

    expect(last.path).toBe('/hook')
    expect(event.type).toBe('item.approved')
    expect(event.data.item).toMatchObject({ id: item.id, status: 'approved' })
  })
})
