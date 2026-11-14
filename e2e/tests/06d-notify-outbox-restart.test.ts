// The alert outbox survives a crash (SIGKILL, no graceful shutdown),
// like the webhook outbox in 06c: alerts queued while a send is in
// flight are pushed after the restart, each post exactly once, and the push
// that was in flight when the process died is not lost: it goes out again as
// soon as the server runs (alert rows have no lease to wait out).
//
// One worker pass sends every due channel and waits for all of them, so a
// push held open by a slow receiver keeps another channel's rows queued and
// untouched; then the API container is killed and started again.
import { afterAll, beforeAll, describe, expect, it } from 'vitest'

import { type Alert, api, type Collection, unique } from '../lib/clients.js'
import { delayReceiver, type Received, received, resetReceiver } from '../lib/receiver.js'
import { API, API_CONTAINER, docker, RECEIVER_INTERNAL, waitForHttp } from '../lib/stack.js'
import { waitFor } from '../lib/wait.js'

const agent = api()

interface Push {
  topic: string
  message: string
}

async function pushes(topic: string): Promise<(Received & { push: Push })[]> {
  return (await received('/'))
    .map((r) => ({ ...r, push: JSON.parse(r.body) as Push }))
    .filter((r) => r.push.topic === topic)
}

describe('alert outbox across a crash', () => {
  const run = Date.now()
  const heldTopic = `held${run}`
  const queuedTopic = `queued${run}`
  let heldCol: Collection
  let queuedCol: Collection
  let held: Alert
  let queued: Alert
  const queuedTitles: string[] = []

  beforeAll(async () => {
    await resetReceiver(0)
    heldCol = await agent.collection({ name: unique('Held push') })
    queuedCol = await agent.collection({ name: unique('Queued push') })
    held = await agent.alert({
      name: unique('ntfy held'),
      type: 'ntfy',
      config: { server_url: RECEIVER_INTERNAL, topic: heldTopic },
      collection_ids: [heldCol.id],
      events: ['post.created'],
    })
    queued = await agent.alert({
      name: unique('ntfy queued'),
      type: 'ntfy',
      config: { server_url: RECEIVER_INTERNAL, topic: queuedTopic },
      collection_ids: [queuedCol.id],
      events: ['post.created'],
    })
  })

  afterAll(async () => {
    await delayReceiver(0)
    await agent.del(`/v1/alerts/${held.id}`).catch(() => undefined)
    await agent.del(`/v1/alerts/${queued.id}`).catch(() => undefined)
  })

  it('queues alerts behind a push in flight', async () => {
    // The send gives up after 30 s; everything up to the kill happens well within that.
    await delayReceiver(120_000)
    await agent.createPost(heldCol.id, { content: 'held push' })
    await waitFor(async () => (await pushes(heldTopic)).length === 1, { message: 'held push not in flight' })

    for (let i = 0; i < 5; i++) {
      const title = `queued push ${run}-${i}`

      queuedTitles.push(title)
      await agent.createPost(queuedCol.id, { content: title })
    }

    const rows = await agent.alertDeliveries(queued.id)

    expect(rows).toHaveLength(5)
    expect(rows.every((d) => d.status === 'pending' && d.attempts === 0)).toBe(true)
    const [inFlight] = await agent.alertDeliveries(held.id)

    expect(inFlight).toMatchObject({ status: 'pending', attempts: 0 })
    expect(await pushes(queuedTopic)).toHaveLength(0)
  })

  it('survives SIGKILL and pushes every queued post exactly once', async () => {
    await docker(['kill', '--signal', 'KILL', API_CONTAINER])
    await delayReceiver(0)
    await docker(['start', API_CONTAINER])
    await waitForHttp(`${API}/health`)

    const rows = await waitFor(
      async () => {
        const list = await agent.alertDeliveries(queued.id)

        return list.length === 5 && list.every((d) => d.status === 'sent') ? list : undefined
      },
      { message: 'queued alerts were not sent after the restart', timeout: 60_000 },
    )

    expect(rows.every((d) => d.attempts === 1)).toBe(true)
    const got = await pushes(queuedTopic)

    expect(got.every((r) => r.status === 200)).toBe(true)
    const text = got.map((r) => r.push.message).join('\n')

    // Each queued post exactly once across the pushes (they may come as one digest).
    expect(queuedTitles.map((title) => text.split(title).length - 1)).toEqual(queuedTitles.map(() => 1))
  })

  it('sends the push that was in flight again after the restart (not lost)', async () => {
    const row = await waitFor(
      async () => {
        const [first, ...rest] = await agent.alertDeliveries(held.id)

        return rest.length === 0 && first?.status === 'sent' ? first : undefined
      },
      { message: 'the push in flight at the crash was not sent again', timeout: 60_000 },
    )

    expect(row.attempts).toBe(1)
    // At least once: the receiver saw the cut-off request and the one after the restart, nothing more.
    const got = await pushes(heldTopic)

    expect(got.map((r) => r.status)).toEqual([200, 200])
  })
})
