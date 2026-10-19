// The background queues end to end: the webhook outbox (retries, giving up,
// manual retry, collection filter), publishing schedules (the next waiting
// draft, every draft, chosen posts; asking an agent or an endpoint for posts
// when nothing waits) and the job queue as clients see it (list, stats,
// cancel, retry). Settings: WEBHOOK_RETRY_BASE in e2e/e2e.env.
import { afterAll, beforeAll, describe, expect, it } from 'vitest'

import {
  api,
  type Channel,
  type Collection,
  type Delivery,
  isValidSignature,
  unique,
  type Webhook,
} from '../lib/clients.js'
import { publishThrough, waitForRequested } from '../lib/flow.js'
import { http } from '../lib/http.js'
import { received, resetReceiver } from '../lib/receiver.js'
import { RECEIVER, RECEIVER_INTERNAL } from '../lib/stack.js'
import { sleep, waitFor } from '../lib/wait.js'

const agent = api()

describe('webhook outbox', () => {
  let watched: Collection
  let other: Collection
  let channel: Channel
  let webhook: Webhook

  beforeAll(async () => {
    watched = await agent.collection({ name: unique('Outbox') })
    other = await agent.collection({ name: unique('Outbox other') })
    channel = await agent.customChannel(unique('Outbox channel'))
    webhook = await agent.webhook({
      url: `${RECEIVER_INTERNAL}/outbox`,
      events: ['post.created', 'post.published'],
      collection_ids: [watched.id],
      max_attempts: 3,
    })
  })

  afterAll(async () => {
    await agent.del(`/v1/webhooks/${webhook.id}`)
  })

  it('refuses unknown event patterns (422)', async () => {
    expect(
      (
        await agent.raw('/v1/webhooks', {
          method: 'POST',
          json: { url: `${RECEIVER_INTERNAL}/x`, events: ['post.publised'] },
        })
      ).status,
    ).toBe(422)
    expect(
      (await agent.raw(`/v1/webhooks/${webhook.id}`, { method: 'PATCH', json: { events: ['posts.*'] } })).status,
    ).toBe(422)
    // The old vocabulary is gone too.
    expect(
      (
        await agent.raw('/v1/webhooks', {
          method: 'POST',
          json: { url: `${RECEIVER_INTERNAL}/x`, events: ['item.approved'] },
        })
      ).status,
    ).toBe(422)
  })

  it('gives up after max_attempts and delivers on a manual retry', async () => {
    await resetReceiver(10)
    const post = await agent.createPost(watched.id, { content: 'outbox: retry me' })

    const failed = await waitFor(
      async () => {
        const [latest] = await agent.deliveries({ webhook_id: webhook.id, post_id: post.id })

        return latest?.status === 'failed' ? latest : undefined
      },
      { message: 'delivery did not fail after max_attempts' },
    )

    expect(failed.event_type).toBe('post.created')
    expect(failed.attempts).toBe(3)
    expect((await received('/outbox')).map((r) => r.status)).toEqual([500, 500, 500])

    await http(`${RECEIVER}/fail?n=0`, { method: 'POST' })
    expect((await agent.raw(`/v1/webhook-deliveries/${failed.id}/retry`, { method: 'POST' })).status).toBe(202)

    const ok: Delivery = await waitFor(
      async () => {
        const [latest] = await agent.deliveries({ webhook_id: webhook.id, post_id: post.id })

        return latest?.status === 'succeeded' ? latest : undefined
      },
      { message: 'manual retry did not succeed' },
    )

    expect(ok.id).toBe(failed.id)
    expect((await received('/outbox')).map((r) => r.status)).toEqual([500, 500, 500, 200])
  })

  it('delivers only subscribed events of the subscribed collections', async () => {
    await resetReceiver(0)
    const outside = await agent.createPost(other.id, { content: 'not watched' })
    const inside = await agent.createPost(watched.id, { content: 'watched', targets: [{ channel_id: channel.id }] })

    await agent.patch(`/v1/posts/${inside.id}`, { content: 'watched, edited' }) // post.updated is not subscribed
    await publishThrough(agent, inside.id) // post.publish_requested is not subscribed; post.published is

    const types = await waitFor(
      async () => {
        const got = (await received('/outbox')).map((r) => JSON.parse(r.body) as { type: string })

        return got.length >= 2 ? got.map((e) => e.type) : undefined
      },
      { message: 'expected two deliveries' },
    )

    await sleep(1500) // nothing else may arrive
    expect((await received('/outbox')).length).toBe(2)
    expect(types.toSorted()).toEqual(['post.created', 'post.published'])
    expect(await agent.deliveries({ webhook_id: webhook.id, post_id: outside.id })).toHaveLength(0)
  })
})

describe('publishing schedules', () => {
  let collection: Collection
  let channel: Channel

  beforeAll(async () => {
    collection = await agent.collection({ name: unique('Schedules') })
    channel = await agent.customChannel(unique('Schedule channel'))
  })

  const draft = (content: string, extra: Record<string, unknown> = {}) =>
    agent.createPost(collection.id, { content, ...extra })

  it('validates the rule, the time and the source (422), and names the collection by id or name', async () => {
    const bad: Record<string, unknown>[] = [
      { kind: 'once' }, // no run_at
      { kind: 'once', run_at: '2020-01-01T00:00:00Z' },
      { kind: 'recurring', rule: {} },
      { kind: 'recurring', rule: { times: ['25:00'] } },
      { kind: 'recurring', rule: { every_minutes: 4 } },
      { kind: 'recurring', rule: { times: ['09:00'], weekdays: [7] } },
      { kind: 'recurring', rule: { times: ['09:00'], tz: 'Mars/Olympus' } },
      { kind: 'recurring', rule: { times: ['09:00'] }, source: 'endpoint' }, // no endpoint_url
      { kind: 'recurring', rule: { times: ['09:00'] }, source: 'oracle' },
      { kind: 'recurring', rule: { times: ['09:00'] }, pick: 'random' },
      { kind: 'daily' },
    ]

    for (const json of bad) {
      const res = await agent.raw('/v1/schedules', {
        method: 'POST',
        json: { collection: collection.id, name: 'bad', ...json },
      })

      expect(res.status).toBe(422)
    }

    expect(
      (
        await agent.raw('/v1/schedules', {
          method: 'POST',
          json: { collection: 'col_nope', kind: 'once', run_at: '2099-01-01T00:00:00Z' },
        })
      ).status,
    ).toBe(404)
    const byName = await agent.createSchedule({
      collection: collection.name,
      name: 'Weekday mornings',
      kind: 'recurring',
      rule: { times: ['09:00', '17:30'], weekdays: [1, 2, 3, 4, 5], tz: 'Europe/Warsaw' },
      channels: [channel.id],
    })

    expect(byName).toMatchObject({ collection_id: collection.id, pick: 'next', source: 'none', enabled: true })
    expect(byName.next_run_at).not.toBeNull()
    expect(Date.parse(byName.next_run_at!)).toBeGreaterThan(Date.now())
    // Its run is a queued job for that time.
    const [job] = await agent.jobs({ kind: 'run_schedule', status: 'queued' })

    expect(job).toMatchObject({ kind: 'run_schedule', payload: { schedule_id: byName.id } })
    expect(Date.parse(job!.run_at)).toBe(Date.parse(byName.next_run_at!))
    await agent.del(`/v1/schedules/${byName.id}`)
    // Deleting it takes the job with it.
    expect(
      (await agent.jobs({ kind: 'run_schedule', status: 'queued' })).find((j) => j.payload.schedule_id === byName.id),
    ).toBeUndefined()
  })

  it('"the next waiting post": a run publishes the oldest draft on the schedule\'s channels, later runs the next', async () => {
    const first = await draft('first in line')
    const second = await draft('second in line')
    const timed = await draft('has its own time', {
      targets: [{ channel_id: channel.id }],
      publish_at: new Date(Date.now() + 86400_000).toISOString(),
      status: 'scheduled',
    })
    const schedule = await agent.createSchedule({
      collection: collection.id,
      name: 'Next one',
      kind: 'recurring',
      rule: { every_minutes: 60 },
      channels: [channel.id],
      pick: 'next',
    })
    const ran = await agent.runSchedule(schedule.id)

    expect(ran.last_result).toBe('published 1')
    expect(ran.last_run_at).not.toBeNull()
    expect(ran.next_run_at).toBe(schedule.next_run_at) // a manual run does not move the regular one
    const asked = await waitForRequested(agent, first.id)

    expect(asked.status).toBe('publishing')
    expect(asked.targets.map((t) => t.channel_id)).toEqual([channel.id]) // the schedule's channel became the target
    expect((await agent.getPost(second.id)).status).toBe('draft')
    expect((await agent.getPost(timed.id)).status).toBe('scheduled') // a post with its own time is not "waiting"
    const again = await agent.runSchedule(schedule.id)

    expect(again.last_result).toBe('published 1')
    await waitForRequested(agent, second.id)
    const idle = await agent.runSchedule(schedule.id)

    expect(idle.last_result).toBe('nothing to publish')
    const types = (
      await agent.get<{ data: { type: string }[] }>(`/v1/events?schedule_id=${schedule.id}&limit=20`)
    ).data.map((e) => e.type)

    expect(types).toEqual(['schedule.ran', 'schedule.ran', 'schedule.ran', 'schedule.created'])
    await agent.del(`/v1/schedules/${schedule.id}`)
  })

  it('"every waiting post" and "chosen posts"', async () => {
    const a = await draft('all a')
    const b = await draft('all b')
    const all = await agent.createSchedule({
      collection: collection.id,
      name: 'All',
      kind: 'recurring',
      rule: { every_minutes: 60 },
      channels: [channel.id],
      pick: 'all',
    })

    expect((await agent.runSchedule(all.id)).last_result).toBe('published 2')
    await waitForRequested(agent, a.id)
    await waitForRequested(agent, b.id)
    const c = await draft('chosen c', { targets: [{ channel_id: channel.id, content: 'my own text' }] })
    const d = await draft('not chosen d')
    const chosen = await agent.createSchedule({
      collection: collection.id,
      name: 'Chosen',
      kind: 'recurring',
      rule: { every_minutes: 60 },
      channels: [channel.id],
      pick: 'selected',
      post_ids: [c.id],
    })

    expect(chosen.post_ids).toEqual([c.id])
    expect((await agent.runSchedule(chosen.id)).last_result).toBe('published 1')
    const asked = await waitForRequested(agent, c.id)

    expect(asked.targets[0]!.content).toBe('my own text') // the post's own target wins over the schedule's channels
    expect((await agent.getPost(d.id)).status).toBe('draft')
    await agent.del(`/v1/schedules/${all.id}`)
    await agent.del(`/v1/schedules/${chosen.id}`)
  })

  it('a schedule without channels cannot publish a draft that has no targets', async () => {
    const empty = await agent.collection({ name: unique('No channels') })
    const p = await agent.createPost(empty.id, { content: 'nowhere' })
    const s = await agent.createSchedule({
      collection: empty.id,
      name: 'Nowhere',
      kind: 'recurring',
      rule: { every_minutes: 60 },
    })
    const ran = await agent.runSchedule(s.id)

    // The draft was found but had nowhere to go: nothing published, nothing recorded as a failure.
    expect(ran.last_result).not.toMatch(/^published/)
    expect((await agent.getPost(p.id)).status).toBe('draft')
  })

  it('source agent: nothing waiting asks the agent (schedule.posts_requested, GET /v1/work) until a draft arrives', async () => {
    const empty = await agent.collection({ name: unique('Ask agent') })
    const s = await agent.createSchedule({
      collection: empty.id,
      name: 'Ask',
      kind: 'recurring',
      rule: { times: ['09:00'] },
      channels: [channel.id],
      source: 'agent',
    })
    const ran = await agent.runSchedule(s.id)

    expect(ran.last_result).toBe('asked for posts')
    const events = (
      await agent.get<{ data: { type: string; data: { channels: string[] } }[] }>(
        `/v1/events?schedule_id=${s.id}&type=schedule.posts_requested`,
      )
    ).data

    expect(events).toHaveLength(1)
    expect(events[0]!.data.channels).toEqual([channel.id])
    const work = (
      await agent.get<{ data: { kind: string; schedule?: { id: string }; collection: { id: string } }[] }>('/v1/work')
    ).data

    expect(work.find((w) => w.kind === 'posts' && w.schedule?.id === s.id)?.collection.id).toBe(empty.id)
    // The agent answers with a draft: the request is satisfied, the next run publishes it.
    const reply = await agent.createPost(empty.id, { content: 'written by the agent', source: 'claude-code' })

    expect(
      (await agent.get<{ data: { kind: string; schedule?: { id: string } }[] }>('/v1/work')).data.find(
        (w) => w.schedule?.id === s.id,
      ),
    ).toBeUndefined()
    expect((await agent.runSchedule(s.id)).last_result).toBe('published 1')
    await waitForRequested(agent, reply.id)
  })

  it('source endpoint: nothing waiting fetches posts from a signed endpoint and publishes them', async () => {
    await resetReceiver(0)
    const empty = await agent.collection({ name: unique('Ask endpoint') })

    await http(`${RECEIVER}/posts`, {
      method: 'PUT',
      json: {
        posts: [
          { content: 'From the endpoint, one', tags: ['auto'] },
          { content: 'From the endpoint, two', targets: [{ channel_id: channel.id, content: 'short two' }] },
        ],
      },
    })
    const s = await agent.createSchedule({
      collection: empty.id,
      name: 'Fetch',
      kind: 'recurring',
      rule: { every_minutes: 30 },
      channels: [channel.id],
      source: 'endpoint',
      endpoint_url: `${RECEIVER_INTERNAL}/posts`,
      endpoint_secret: 'ep_secret_for_the_test',
    })

    expect(s.endpoint_url).toBe(`${RECEIVER_INTERNAL}/posts`)
    expect(JSON.stringify(s)).not.toContain('ep_secret_for_the_test')
    const ran = await agent.runSchedule(s.id)

    expect(ran.last_result).toBe('published 2')
    const [req] = await received('/posts')
    const body = JSON.parse(req!.body) as {
      schedule: { id: string }
      collection_id: string
      channels: string[]
      slot: string
    }

    expect(req!.headers['user-agent']).toBe('Staqly-Schedules/1.0')
    expect(req!.headers['content-type']).toBe('application/json')
    expect(isValidSignature('ep_secret_for_the_test', req!.body, req!.headers)).toBe(true)
    expect(body).toMatchObject({ schedule: { id: s.id }, collection_id: empty.id, channels: [channel.id] })
    const posts = await agent.listPosts({ collection_id: empty.id, sort: 'created_asc' })

    expect(posts.map((p) => p.content)).toEqual(['From the endpoint, one', 'From the endpoint, two'])
    expect(posts[0]).toMatchObject({ source: 'schedule:Fetch', tags: ['auto'] })
    expect(posts[1]!.targets[0]!.content).toBe('short two')

    for (const p of posts) {
      await waitForRequested(agent, p.id)
    }

    // An endpoint that fails is recorded on the schedule and as schedule.failed; nothing is created.
    await resetReceiver(5)
    const broken = await agent.createSchedule({
      collection: empty.id,
      name: 'Broken',
      kind: 'recurring',
      rule: { every_minutes: 30 },
      channels: [channel.id],
      source: 'endpoint',
      endpoint_url: `${RECEIVER_INTERNAL}/broken`,
    })
    const failed = await agent.runSchedule(broken.id)

    expect(failed.last_result).toMatch(/^endpoint failed: endpoint responded 500/)
    expect(
      (await agent.get<{ data: { type: string }[] }>(`/v1/events?schedule_id=${broken.id}&limit=5`)).data[0]!.type,
    ).toBe('schedule.failed')
    expect(await agent.listPosts({ collection_id: empty.id })).toHaveLength(2)
    await resetReceiver(0)
  })

  it('a disabled schedule is not run by the clock, a once schedule disables itself after running', async () => {
    const s = await agent.createSchedule({
      collection: collection.id,
      name: 'Paused',
      kind: 'recurring',
      rule: { every_minutes: 60 },
      channels: [channel.id],
      enabled: false,
    })

    expect(s).toMatchObject({ enabled: false, next_run_at: null })
    expect(
      (await agent.jobs({ kind: 'run_schedule', status: 'queued' })).find((j) => j.payload.schedule_id === s.id),
    ).toBeUndefined()
    const on = await agent.patch<{ enabled: boolean; next_run_at: string | null }>(`/v1/schedules/${s.id}`, {
      enabled: true,
    })

    expect(on.enabled).toBe(true)
    expect(on.next_run_at).not.toBeNull()
    const once = await agent.createSchedule({
      collection: collection.id,
      name: 'Once',
      kind: 'once',
      run_at: new Date(Date.now() + 3600_000).toISOString(),
      channels: [channel.id],
    })

    expect(once.next_run_at).toBe(once.run_at)
    // Running it by hand leaves the real run in place (the clock would disable it; see 03f).
    const ran = await agent.runSchedule(once.id)

    expect(ran).toMatchObject({ enabled: true, next_run_at: once.run_at })
  })
})

describe('the job queue', () => {
  it('lists, counts, cancels queued jobs and refuses what cannot be retried or cancelled', async () => {
    const collection = await agent.collection({ name: unique('Jobs') })
    const channel = await agent.customChannel(unique('Jobs channel'))
    const far = new Date(Date.now() + 2 * 86400_000).toISOString()
    const post = await agent.createPost(collection.id, {
      content: 'far ahead',
      targets: [{ channel_id: channel.id }],
      publish_at: far,
      status: 'scheduled',
    })
    const queued = await waitFor(
      async () =>
        (await agent.jobs({ kind: 'publish_post', status: 'queued' })).find((j) => j.payload.post_id === post.id),
      { message: 'no publish_post job for the scheduled post' },
    )

    expect(queued).toMatchObject({ key: `publish_post:${post.id}`, attempts: 0, max_attempts: 1 })
    expect(Date.parse(queued.run_at)).toBe(Date.parse(far))
    expect(await agent.get(`/v1/jobs/${queued.id}`)).toMatchObject({ id: queued.id })
    const stats = await agent.get<{
      queued: number
      running: number
      failed: number
      done_24h: number
      by_kind: Record<string, { queued: number }>
    }>('/v1/jobs/stats')

    expect(stats.queued).toBeGreaterThanOrEqual(1)
    expect(stats.by_kind.publish_post!.queued).toBeGreaterThanOrEqual(1)
    expect(stats.done_24h).toBeGreaterThan(0) // the schedule runs and publish_target jobs above
    // Only failed jobs can be retried.
    expect((await agent.raw(`/v1/jobs/${queued.id}/retry`, { method: 'POST' })).status).toBe(409)
    const cancelled = await agent.raw(`/v1/jobs/${queued.id}/cancel`, { method: 'POST' })

    expect(cancelled.status).toBe(200)
    expect(cancelled.json<{ status: string }>().status).toBe('cancelled')
    expect((await agent.raw(`/v1/jobs/${queued.id}/cancel`, { method: 'POST' })).status).toBe(409)
    expect((await agent.raw('/v1/jobs/job_nope', { method: 'GET' })).status).toBe(404)
    expect((await agent.raw('/v1/jobs?status=nope')).status).toBe(422)
    // An unknown kind matches nothing (it is not refused).
    expect((await agent.raw('/v1/jobs?kind=nope')).json<{ data: unknown[] }>().data).toEqual([])
    // Re-scheduling the post queues its job again (the row under the unique key is reused).
    await agent.schedule(post.id, new Date(Date.now() + 3 * 86400_000).toISOString())
    const fresh = (await agent.jobs({ kind: 'publish_post', status: 'queued' })).find(
      (j) => j.payload.post_id === post.id,
    )

    expect(fresh).toMatchObject({ status: 'queued' })
    // A done job: the publish_target of an earlier post.
    const [done] = await agent.jobs({ kind: 'publish_target', status: 'done' })

    expect(done).toBeDefined()
    expect((await agent.raw(`/v1/jobs/${done!.id}/cancel`, { method: 'POST' })).status).toBe(409)
    expect((await agent.raw(`/v1/jobs/${done!.id}/retry`, { method: 'POST' })).status).toBe(409)
  })
})
