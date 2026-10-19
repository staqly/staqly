// Background workers against the clock (local mode). The shortest allowed
// processing timeout is one minute, so the slow checks share one wait: a post
// scheduled a few seconds ahead is published by the scheduler, a once
// schedule runs at its time and disables itself, and a custom target nobody
// reports on fails after the collection's processing timeout (a late result
// is 409). Then the limits of the settings involved.
import { beforeAll, describe, expect, it } from 'vitest'

import { api, type Channel, type Collection, unique } from '../lib/clients.js'
import { waitForPost, waitForRequested } from '../lib/flow.js'
import { waitFor } from '../lib/wait.js'

const agent = api()
let collection: Collection
let channel: Channel

beforeAll(async () => {
  collection = await agent.collection({ name: unique('Workers') })
  channel = await agent.customChannel(unique('Workers channel'))
})

describe('the clock (one shared minute)', () => {
  it('publishes a scheduled post at its time, runs a once schedule, fails a target nobody reported on', async () => {
    const timed = await agent.collection({ name: unique('Timeout'), processing_timeout_seconds: 60 })

    expect(timed.processing_timeout_seconds).toBe(60)
    // 1. A post scheduled 6 s ahead.
    const at = new Date(Date.now() + 6000).toISOString()
    const scheduled = await agent.createPost(collection.id, {
      content: 'scheduled post',
      targets: [{ channel_id: channel.id }],
      publish_at: at,
      status: 'scheduled',
    })

    expect(scheduled.status).toBe('scheduled')
    // 2. A once schedule 6 s ahead with a waiting draft.
    const waiting = await agent.createPost(collection.id, { content: 'picked by the once schedule' })
    const once = await agent.createSchedule({
      collection: collection.id,
      name: 'Once soon',
      kind: 'once',
      run_at: new Date(Date.now() + 6000).toISOString(),
      channels: [channel.id],
    })
    // 3. A custom target in the collection with the one-minute timeout.
    const stuck = await agent.createPost(timed.id, {
      content: 'stuck with the automation',
      targets: [{ channel_id: channel.id }],
    })

    await agent.publish(stuck.id)
    const requestedAt = Date.now()

    // The calendar: posts by publish time.
    const day = at.slice(0, 10)
    const listed = await agent.listPosts({ publish_after: day, sort: 'publish_asc', collection_id: collection.id })

    expect(listed.map((p) => p.id)).toContain(scheduled.id)

    const started = await waitForRequested(agent, scheduled.id)

    expect(started.status).toBe('publishing')
    expect(Date.now()).toBeGreaterThanOrEqual(new Date(at).getTime() - 1000)
    const history = await agent.history(scheduled.id)

    expect(history.find((e) => e.type === 'post.publish_requested')?.actor).toMatchObject({
      type: 'system',
      id: 'publisher',
    })
    // Once the run started the post is locked: no moving, no cancelling, no editing.
    expect((await agent.raw(`/v1/posts/${scheduled.id}/cancel`, { method: 'POST' })).status).toBe(409)
    expect((await agent.raw(`/v1/posts/${scheduled.id}`, { method: 'PATCH', json: { content: 'x' } })).status).toBe(409)
    expect(
      (
        await agent.raw(`/v1/posts/${scheduled.id}/schedule`, {
          method: 'POST',
          json: { publish_at: new Date(Date.now() + 3600_000).toISOString() },
        })
      ).status,
    ).toBe(409)
    // The queue records the run as done a moment after the request goes out.
    await expect
      .poll(
        async () =>
          (await agent.jobs({ kind: 'publish_post' })).find((j) => j.payload.post_id === scheduled.id)?.status,
        { timeout: 10_000 },
      )
      .toBe('done')

    const picked = await waitForRequested(agent, waiting.id, 1, 30_000)

    expect(picked.targets.map((t) => t.channel_id)).toEqual([channel.id])
    const ran = await waitFor(
      async () => {
        const s = await agent.get<{ enabled: boolean; next_run_at: string | null; last_result: string }>(
          `/v1/schedules/${once.id}`,
        )

        return s.enabled ? undefined : s
      },
      { message: 'the once schedule did not disable itself' },
    )

    expect(ran).toMatchObject({ enabled: false, next_run_at: null, last_result: 'published 1' })
    expect((await agent.history(waiting.id)).find((e) => e.type === 'post.publish_requested')).toBeDefined()
    const scheduleEvents = (
      await agent.get<{ data: { type: string; actor: { id: string } }[] }>(`/v1/events?schedule_id=${once.id}&limit=5`)
    ).data

    expect(scheduleEvents[0]).toMatchObject({ type: 'schedule.ran', actor: { id: 'scheduler' } })

    const failed = await waitForPost(agent, stuck.id, 'failed', 90_000)

    expect(Date.now() - requestedAt).toBeGreaterThanOrEqual(55_000)
    expect(failed.targets[0]).toMatchObject({ status: 'failed', error: 'No result reported in time' })
    expect((await agent.history(stuck.id))[0]).toMatchObject({ type: 'post.failed' })
    // A result arriving after the timeout is refused (the target is no longer waiting).
    const late = await agent.raw(`/v1/posts/${stuck.id}/targets/${channel.id}/result`, {
      method: 'POST',
      json: { status: 'published', url: 'https://late.example' },
    })

    expect(late.status).toBe(409)
    // The timeout job is done; a target settled in time takes its queued timeout job out of the queue instead.
    const timeoutJob = (await agent.jobs({ kind: 'target_timeout' })).find((j) => j.payload.post_id === stuck.id)

    expect(timeoutJob?.status).toBe('done')
    const quick = await agent.createPost(timed.id, {
      content: 'answered in time',
      targets: [{ channel_id: channel.id }],
    })

    await agent.publish(quick.id)
    await waitForRequested(agent, quick.id)
    await agent.result(quick.id, channel.id, { status: 'published', url: 'https://example.com/quick' })
    const dropped = (await agent.jobs({ kind: 'target_timeout' })).find((j) => j.payload.post_id === quick.id)

    expect(dropped).toBeUndefined()
  }, 180_000)

  it('refuses timeouts outside 1 minute and 24 hours, times in the past or over a year ahead', async () => {
    for (const processing_timeout_seconds of [59, 86401, -5, 'soon']) {
      const res = await agent.raw(`/v1/collections/${collection.id}`, {
        method: 'PATCH',
        json: { processing_timeout_seconds },
      })

      expect(res.status).toBe(422)
    }

    const back = await agent.patch<Collection>(`/v1/collections/${collection.id}`, { processing_timeout_seconds: null })

    expect(back.processing_timeout_seconds).toBeNull()

    for (const publish_at of [
      '2020-01-01T00:00:00Z',
      new Date(Date.now() + 400 * 86400_000).toISOString(),
      'tomorrow',
      '',
    ]) {
      const res = await agent.raw(`/v1/posts`, {
        method: 'POST',
        json: {
          collection: collection.id,
          content: 'x',
          targets: [{ channel_id: channel.id }],
          publish_at,
          status: 'scheduled',
        },
      })

      expect(res.status).toBe(422)
    }

    expect(
      (
        await agent.raw(`/v1/schedules`, {
          method: 'POST',
          json: { collection: collection.id, name: 'past', kind: 'once', run_at: '2020-01-01T00:00:00Z' },
        })
      ).status,
    ).toBe(422)
  })

  it('the session reports the server default for the processing timeout', async () => {
    const session = await agent.get<{ defaults: { processing_timeout_seconds: number } }>('/v1/session')

    expect(session.defaults.processing_timeout_seconds).toBe(3600)
  })
})
