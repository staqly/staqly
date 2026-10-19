// Publishing schedules, the job queue and the requested work across clients: the Python SDK creates
// a collection with drafts, the CLI creates a recurring schedule, curl runs it, the TS SDK watches the
// post reach the automation and reports the result, MCP sees the schedule ask for posts, every client
// reads the same queue. Nothing waits for the clock: schedules are run by hand.
import type { Job, Post, Schedule } from '@staqly/sdk'
import { afterAll, beforeAll, describe, expect, inject, it } from 'vitest'

import { type CloudWorkspace, cliRunner, curl, mcp, type PyClient, PythonSDK, sdk, signup, until } from './lib.js'

const local = inject('local')
const cloud = inject('cloud')
const bin = inject('bin')

let py: PythonSDK
let ws: CloudWorkspace

beforeAll(async () => {
  py = new PythonSDK()
  await py.start()
  ws = await signup(cloud.url)
})

afterAll(() => py?.stop())

describe.each(['local', 'cloud'] as const)('%s: schedules, jobs and requested work', (name) => {
  let url: string
  let key: string

  beforeAll(() => {
    url = name === 'local' ? local.url : cloud.url
    key = name === 'local' ? local.token : ws.agentKey
  })

  it('Python drafts → CLI schedule → curl runs it → TS sees the request and reports → MCP and the CLI read the queue', async () => {
    const c = sdk(url, key || null)
    const cli = cliRunner(bin, url, key)
    const pyc: PyClient = { base_url: url, api_key: key }
    const colName = `Schedules ${name} ${Date.now()}`
    const col = await py.call<{ id: string }>('sync', pyc, 'collections.create', [colName])
    const channel = await py.call<{ id: string; name: string }>('async', pyc, 'channels.connect', ['custom'], {
      name: `pipeline ${name} ${Date.now()}`,
    })
    const first = await py.call<Post>('sync', pyc, 'posts.create', [col.id], { content: 'first in line' })
    const second = await py.call<Post>('async', pyc, 'posts.create', [col.id], { content: 'second in line' })

    // The CLI creates the schedule: weekday mornings, the next waiting post, on the channel.
    const created = await cli([
      'schedules',
      'create',
      colName,
      '--name',
      'Mornings',
      '--times',
      '09:00,17:30',
      '--weekdays',
      'mon,tue,wed,thu,fri',
      '--tz',
      'Europe/Warsaw',
      '--channel',
      channel.name,
      '--pick',
      'next',
      '--json',
    ])

    expect(created).toMatchObject({ code: 0 })
    const schedule = created.json<Schedule>()

    expect(schedule).toMatchObject({
      collection_id: col.id,
      kind: 'recurring',
      pick: 'next',
      channels: [channel.id],
      enabled: true,
    })
    expect(schedule.rule).toMatchObject({ times: ['09:00', '17:30'], weekdays: [1, 2, 3, 4, 5], tz: 'Europe/Warsaw' })
    expect(schedule.next_run_at).toBeTruthy()
    // Every client lists it the same way.
    expect((await c.schedules.list({ collection_id: col.id })).map((s) => s.id)).toEqual([schedule.id])
    expect(
      (await py.call<Schedule[]>('sync', pyc, 'schedules.list', [], { collection_id: col.id })).map((s) => s.id),
    ).toEqual([schedule.id])
    expect(
      (await mcp<{ schedules: Schedule[] }>(url, key, 'list_schedules', { collection: colName })).schedules.map(
        (s) => s.id,
      ),
    ).toEqual([schedule.id])
    // Its run is a queued job for the next time.
    const queued = await c.jobs.list({ kind: 'run_schedule', status: 'queued' })

    expect(queued.data.find((j) => j.payload?.schedule_id === schedule.id)).toBeDefined()

    // curl runs it now: the oldest draft is handed to the automation on the schedule's channel.
    const ran = await curl(`${url}/v1/schedules/${schedule.id}/run`, { key, method: 'POST' })

    expect(ran.status).toBe(202)
    expect(ran.json<Schedule>().last_result).toBe('published 1')
    const asked = await until(async () => {
      const p = await c.posts.get(first.id)

      return p.targets[0]?.status === 'publishing' && p.targets[0].attempts > 0 ? p : undefined
    })

    expect(asked.status).toBe('publishing')
    expect(asked.targets.map((t) => t.channel_id)).toEqual([channel.id])
    expect((await c.posts.get(second.id)).status).toBe('draft')
    // The work list names it for the automation (TS, Python, CLI, MCP agree).
    const work = await c.work.list()
    const mine = work.find((w) => w.kind === 'target' && w.post?.id === first.id)

    expect(mine?.channel?.id).toBe(channel.id)
    expect(
      (await py.call<{ kind: string; post?: { id: string } }[]>('sync', pyc, 'work.list')).some(
        (w) => w.post?.id === first.id,
      ),
    ).toBe(true)
    expect(
      (await cli(['work', '--json']))
        .json<{ data: { post?: { id: string } }[] }>()
        .data.some((w) => w.post?.id === first.id),
    ).toBe(true)
    expect(
      (await mcp<{ work: { post?: { id: string } }[] }>(url, key, 'list_requested_work', {})).work.some(
        (w) => w.post?.id === first.id,
      ),
    ).toBe(true)

    // The TS SDK waits for the result while curl reports it.
    setTimeout(() => {
      void curl(`${url}/v1/posts/${first.id}/targets/${channel.id}/result`, {
        key,
        json: { status: 'published', url: 'https://example.com/xc/1' },
      })
    }, 800)
    const published = await c.posts.waitForPublished(first.id, { timeout: 20_000, pollInterval: 300 })

    expect(published.status).toBe('published')
    expect(published.targets[0]).toMatchObject({ status: 'published', external_url: 'https://example.com/xc/1' })
    // The queue: the publish_target job is done; stats count it; the CLI and MCP read the same list.
    const jobs = await c.jobs.list({ kind: 'publish_target' })
    const job = jobs.data.find((j) => j.payload?.post_id === first.id)

    expect(job?.status).toBe('done')
    expect((await c.jobs.stats()).done_24h).toBeGreaterThan(0)
    expect(
      (await py.call<{ data: Job[] }>('async', pyc, 'jobs.list', [], { kind: 'publish_target' })).data.some(
        (j) => j.id === job!.id,
      ),
    ).toBe(true)
    expect(
      (await cli(['jobs', 'list', '--kind', 'publish_target', '--json']))
        .json<{ data: Job[] }>()
        .data.some((j) => j.id === job!.id),
    ).toBe(true)
    expect(
      (await mcp<{ data: Job[] }>(url, key, 'list_jobs', { kind: 'publish_target' })).data.some(
        (j) => j.id === job!.id,
      ),
    ).toBe(true)
    expect((await cli(['jobs', 'stats', '--json'])).json<{ done_24h: number }>().done_24h).toBeGreaterThan(0)
    // A done job can be neither retried nor cancelled, in any client.
    expect((await cli(['jobs', 'retry', job!.id])).code).toBe(1)
    expect((await curl(`${url}/v1/jobs/${job!.id}/cancel`, { key, method: 'POST' })).status).toBe(409)
    expect((await py.fail('sync', pyc, 'jobs.cancel', [job!.id])).type).toBe('ConflictError')

    // The CLI turns the schedule off and on, Python runs it (the second draft goes out), MCP deletes it.
    expect((await cli(['schedules', 'update', schedule.id, '--disable', '--json'])).json<Schedule>()).toMatchObject({
      enabled: false,
      next_run_at: null,
    })
    expect(
      (await c.jobs.list({ kind: 'run_schedule', status: 'queued' })).data.find(
        (j) => j.payload?.schedule_id === schedule.id,
      ),
    ).toBeUndefined()
    expect((await py.call<Schedule>('sync', pyc, 'schedules.enable', [schedule.id])).enabled).toBe(true)
    expect((await py.call<Schedule>('async', pyc, 'schedules.run', [schedule.id])).last_result).toBe('published 1')
    await until(async () => (await c.posts.get(second.id)).status === 'publishing')
    await mcp(url, key, 'delete_schedule', { schedule_id: schedule.id })
    expect((await cli(['schedules', 'get', schedule.id])).code).toBe(1)
    expect(await c.schedules.list({ collection_id: col.id })).toEqual([])
  })

  it('a schedule that asks the agent: the request is listed until a draft arrives; the CLI schedules a once run', async () => {
    const c = sdk(url, key || null)
    const cli = cliRunner(bin, url, key)
    const colName = `Agent source ${name} ${Date.now()}`
    const col = await c.collections.create({ name: colName })
    const channel = await c.channels.connect({ provider: 'custom', name: `agent channel ${name} ${Date.now()}` })
    const schedule = await c.schedules.create({
      collection: col.id,
      name: 'Ask',
      kind: 'recurring',
      rule: { every_minutes: 120 },
      channels: [channel.id],
      source: 'agent',
    })
    const ran = await c.schedules.run(schedule.id)

    expect(ran.last_result).toBe('asked for posts')
    const events = (await c.events.list({ schedule_id: schedule.id, type: 'schedule.posts_requested' })).data

    expect(events).toHaveLength(1)
    const work = await mcp<{ work: { kind: string; schedule?: { id: string }; collection: { id: string } }[] }>(
      url,
      key,
      'list_requested_work',
      {},
    )

    expect(work.work.find((w) => w.kind === 'posts' && w.schedule?.id === schedule.id)?.collection.id).toBe(col.id)
    // The agent answers over MCP; the next run publishes it.
    const reply = await mcp<Post>(url, key, 'create_post', { collection: colName, content: 'written on request' })

    expect((await c.work.list()).find((w) => w.kind === 'posts' && w.schedule?.id === schedule.id)).toBeUndefined()
    expect((await mcp<Schedule>(url, key, 'run_schedule', { schedule_id: schedule.id })).last_result).toBe(
      'published 1',
    )
    await until(async () => (await c.posts.get(reply.id)).status === 'publishing')

    // A once schedule from the CLI, with a relative time; its run is queued for then.
    const once = await cli([
      'schedules',
      'create',
      colName,
      '--name',
      'Launch day',
      '--at',
      '+2h',
      '--channel',
      channel.id,
      '--pick',
      'all',
      '--json',
    ])

    expect(once).toMatchObject({ code: 0 })
    const o = once.json<Schedule>()

    expect(o.kind).toBe('once')
    expect(Date.parse(o.run_at!) - Date.now()).toBeGreaterThan(100 * 60_000)
    expect(o.next_run_at).toBe(o.run_at)
    // Validation is the same everywhere: a past time, a bad rule.
    expect((await cli(['schedules', 'create', colName, '--name', 'past', '--at', '2020-01-01T00:00:00Z'])).code).toBe(1)
    expect(
      (
        await py.fail('sync', { base_url: url, api_key: key }, 'schedules.create', [col.id], {
          name: 'bad',
          kind: 'recurring',
          rule: { every_minutes: 4 },
        })
      ).type,
    ).toBe('ValidationError')
    expect(
      (
        await curl(`${url}/v1/schedules`, {
          key,
          json: { collection: col.id, name: 'bad', kind: 'recurring', rule: { times: ['25:00'] } },
        })
      ).status,
    ).toBe(422)
  })
})
