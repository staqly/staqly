// The staqly CLI against real servers, local (its access token) and cloud (API keys):
// every command group's happy path, its error paths and exit codes, --json
// output, and that what the CLI does is what the SDK reads back.
import { mkdtempSync, readFileSync, writeFileSync } from 'node:fs'
import { tmpdir } from 'node:os'
import { join } from 'node:path'

import type { Channel, Collection, Post, Schedule } from '@staqly/sdk'
import { afterAll, beforeAll, describe, expect, inject, it } from 'vitest'

import { type CloudWorkspace, cliRunner, receiver, run, sdk, signup, sleep, until } from './lib.js'

const local = inject('local')
const cloud = inject('cloud')
const bin = inject('bin')
const PNG = Buffer.from(
  'iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAYAAAAfFcSJAAAADUlEQVR42mNk+M9QDwADhgGAWjR9awAAAABJRU5ErkJggg==',
  'base64',
)
const dir = mkdtempSync(join(tmpdir(), 'staqly-xc-files-'))
const png = join(dir, 'pixel.png')

writeFileSync(png, PNG)

let ws: CloudWorkspace

beforeAll(async () => {
  ws = await signup(cloud.url)
})

describe('CLI basics (no server needed)', () => {
  const cli = cliRunner(bin, 'http://127.0.0.1:9')

  it('version, help, completion, usage errors', async () => {
    expect((await cli(['version'])).out).toMatch(/^staqly /)
    expect((await cli(['version', '--json'])).json()).toHaveProperty('version')
    expect((await cli(['--help'])).code).toBe(0)
    expect((await cli(['posts', '--help'])).out).toContain('publish')
    expect((await cli(['--help'])).out).not.toMatch(/\bitems\b|\bwatchers\b|\binbox\b/)
    expect((await cli(['completion', 'bash'])).out.length).toBeGreaterThan(100)
    expect((await cli(['completion', 'zsh'])).code).toBe(0)
    expect((await cli(['frobnicate'])).code).toBe(64)
    expect((await cli(['items', 'list'])).code).toBe(64) // the review era is gone
    expect((await cli(['posts', 'get'])).code).toBe(64) // missing <id>
    expect((await cli(['posts', 'list', '--limit', 'many'])).code).toBe(64)
  })

  it('an unreachable server is exit 1 with a readable error', async () => {
    const r = await cli(['status'])

    expect(r.code).toBe(1)
    expect(r.err).toMatch(/connect|reach|refused/i)
  })
})

describe.each(['local', 'cloud'] as const)('%s: CLI commands', (name) => {
  let url: string
  let key: string
  let cli: ReturnType<typeof cliRunner>
  let admin: ReturnType<typeof cliRunner>
  const colName = `CLI ${name} ${Date.now()}`
  let colId: string
  let channel: Channel

  beforeAll(async () => {
    url = name === 'local' ? local.url : cloud.url
    key = name === 'local' ? local.token : ws.agentKey
    cli = cliRunner(bin, url, key)
    admin = name === 'local' ? cli : cliRunner(bin, url, ws.adminKey)
    channel = await sdk(url, key || null).channels.connect({
      provider: 'custom',
      name: `CLI channel ${name} ${Date.now()}`,
    })
  })

  /** Plays the automation: reports the first target of the post after `ms`. */
  const reportLater = (find: () => Promise<Post | undefined>, status: 'published' | 'failed', ms = 1000) => {
    setTimeout(async () => {
      const c = sdk(url, key || null)
      const post = await until(async () => {
        const p = await find()

        return p?.targets[0]?.status === 'publishing' && p.targets[0].attempts > 0 ? p : undefined
      }, 15_000)

      await c.posts.reportResult(
        post.id,
        post.targets[0]!.channel_id,
        status === 'published'
          ? { status, url: 'https://example.com/cli' }
          : { status, error: 'reported failed via SDK' },
      )
    }, ms)
  }

  it('status', async () => {
    const r = await cli(['status', '--json'])

    expect(r).toMatchObject({ code: 0 })
    expect(r.json<{ session: { mode: string } }>().session.mode).toBe(name)
    expect((await cli(['status'])).out).toContain(name)
  })

  it('collections: create, list, get, rename/timeout/archive by name, delete', async () => {
    const created = await cli(['collections', 'create', colName, '--description', 'from the CLI', '--json'])

    expect(created).toMatchObject({ code: 0 })
    colId = created.json<Collection>().id
    expect((await cli(['collections', 'create', colName])).code).toBe(1) // 409 duplicate name
    expect((await cli(['collections', 'list', '--json'])).out).toContain(colId)
    expect((await cli(['collections', 'get', colName, '--json'])).json<Collection>().id).toBe(colId)
    // name-addressed updates (the CLI resolves names first)
    const tmp = `${colName} tmp`

    expect((await cli(['collections', 'create', tmp, '--processing-timeout', '2h'])).code).toBe(0)
    const renamed = await cli(['collections', 'rename', tmp, `${tmp} renamed`])

    expect(renamed).toMatchObject({ code: 0 })
    expect(renamed.out).toContain(`${tmp} renamed`)
    expect((await cli(['collections', 'timeout', `${tmp} renamed`, '30m'])).code).toBe(0)
    expect((await sdk(url, key || null).collections.get(`${tmp} renamed`)).processing_timeout_seconds).toBe(1800)
    expect((await cli(['collections', 'timeout', `${tmp} renamed`, 'default'])).code).toBe(0)
    expect((await sdk(url, key || null).collections.get(`${tmp} renamed`)).processing_timeout_seconds).toBeNull()
    expect((await cli(['collections', 'timeout', `${tmp} renamed`, '30s'])).code).toBe(1) // under a minute
    expect((await cli(['collections', 'archive', `${tmp} renamed`])).code).toBe(0)
    expect((await cli(['collections', 'archive', `${tmp} renamed`])).code).toBe(1) // already archived
    expect((await cli(['collections', 'archive', `${tmp} renamed`, '--undo'])).code).toBe(0)
    expect((await cli(['collections', 'delete', `${tmp} renamed`, '--yes'])).code).toBe(0)
    const gone = await cli(['collections', 'get', `${tmp} renamed`])

    expect(gone.code).toBe(1)
    expect(gone.err).toMatch(/not found/i)
  })

  it('posts create: a draft, --publish-at (needs a channel), --now --wait: exit 0 published, 2 failed, 3 timeout', async () => {
    const c = sdk(url, key || null)
    const draft = await cli([
      'posts',
      'create',
      colName,
      '--title',
      'Post',
      '--content',
      'A draft\nfrom the CLI',
      '--tag',
      'cli,draft',
      '--meta',
      'n=3',
      '--meta',
      'ok=true',
      '--json',
    ])

    expect(draft).toMatchObject({ code: 0 })
    expect(draft.json<Post>()).toMatchObject({
      status: 'draft',
      tags: ['cli', 'draft'],
      metadata: { n: 3, ok: true },
      source: 'cli',
    })
    // A time without a channel is refused before anything is sent.
    expect(
      (await cli(['posts', 'create', colName, '--title', 'Post', '--content', 'x', '--publish-at', '+2h'])).code,
    ).not.toBe(0)
    const scheduled = await cli([
      'posts',
      'create',
      colName,
      '--title',
      'Post',
      '--content',
      'Scheduled from the CLI',
      '--channel',
      channel.name,
      '--publish-at',
      '+2h',
      '--json',
    ])

    expect(scheduled).toMatchObject({ code: 0 })
    expect(scheduled.json<Post>().status).toBe('scheduled')
    expect(Date.parse(scheduled.json<Post>().publish_at!) - Date.now()).toBeGreaterThan(100 * 60_000)
    expect(scheduled.json<Post>().targets.map((t) => t.channel_id)).toEqual([channel.id])
    // --now --wait: published (0)
    const find = (content: string) => async () => (await c.posts.list({ collection_id: colId, q: content })).data[0]

    reportLater(find('nowok'), 'published')
    const ok = await cli([
      'posts',
      'create',
      colName,
      '--title',
      'Post',
      '--content',
      'nowok',
      '--channel',
      channel.id,
      '--now',
      '--wait',
      '--timeout',
      '20s',
    ])

    expect([ok.code, ok.out]).toEqual([0, expect.stringMatching(/Published/)])
    // failed (2) with the error shown
    reportLater(find('nowno'), 'failed')
    const no = await cli([
      'posts',
      'create',
      colName,
      '--title',
      'Post',
      '--content',
      'nowno',
      '--channel',
      channel.id,
      '--now',
      '--wait',
      '--timeout',
      '20s',
    ])

    expect(no.code).toBe(2)
    expect(no.out + no.err).toContain('reported failed via SDK')
    // timeout (3): nobody reports
    const slow = await cli([
      'posts',
      'create',
      colName,
      '--title',
      'Post',
      '--content',
      'nowslow',
      '--channel',
      channel.id,
      '--now',
      '--wait',
      '--timeout',
      '2s',
    ])

    expect(slow.code).toBe(3)
    // --json with --wait prints the settled post
    reportLater(find('nowjson'), 'published')
    const asJson = await cli([
      'posts',
      'create',
      colName,
      '--title',
      'Post',
      '--content',
      'nowjson',
      '--channel',
      channel.id,
      '--now',
      '--wait',
      '--timeout',
      '20s',
      '--json',
    ])

    expect(asJson.code).toBe(0)
    expect(asJson.json<Post>()).toMatchObject({ status: 'published' })
  })

  it('posts create variants: --file, --stdin, --idempotency-key, @file content', async () => {
    const file = await cli([
      'posts',
      'create',
      colName,
      '--title',
      'Post',
      '--content',
      'pixel',
      '--file',
      png,
      '--json',
    ])

    expect(file).toMatchObject({ code: 0 })
    expect(file.json<Post>().media_items[0]!.mime_type).toBe('image/png')
    const stdin = await cli(['posts', 'create', colName, '--title', 'Post', '--stdin', '--json'], {
      input: 'piped body',
    })

    expect(stdin.json<Post>().content).toBe('piped body')
    const md = join(dir, `${name}-post.md`)

    writeFileSync(md, 'From a file\nsecond line')
    expect(
      (await cli(['posts', 'create', colName, '--title', 'Post', '--content', `@${md}`, '--json'])).json<Post>()
        .content,
    ).toBe('From a file\nsecond line')
    const k = `cli-${name}-${Date.now()}`
    const a = await cli([
      'posts',
      'create',
      colName,
      '--title',
      'Post',
      '--content',
      'idem',
      '--idempotency-key',
      k,
      '--json',
    ])
    const b = await cli([
      'posts',
      'create',
      colName,
      '--title',
      'Post',
      '--content',
      'idem',
      '--idempotency-key',
      k,
      '--json',
    ])

    expect(b.json<Post>().id).toBe(a.json<Post>().id)
  })

  it('posts: list (--all over pages, filters), get, update, schedule, cancel, duplicate, preview, history, delete, purge', async () => {
    const c = sdk(url, key || null)
    const tag = `cli-tag-${Date.now()}`
    const ids: string[] = []

    for (let i = 0; i < 5; i++) {
      ids.push(
        (
          await c.posts.create({
            title: 'Post',
            collection: colId,
            content: `cli post ${i}`,
            tags: [tag],
            targets: [{ channel_id: channel.id }],
          })
        ).id,
      )
    }

    const all = await cli(['posts', 'list', '--collection', colName, '--tag', tag, '--limit', '2', '--all', '--json'])

    expect(all).toMatchObject({ code: 0 })
    expect(
      all
        .json<{ data: Post[] }>()
        .data.map((p) => p.id)
        .toSorted(),
    ).toEqual(ids.toSorted())
    const table = await cli(['posts', 'list', '--collection', colName, '--tag', tag])

    expect(table.out.split('\n').filter((l) => l.includes('post_'))).toHaveLength(5)
    expect(
      (await cli(['posts', 'list', '--channel', channel.name, '--status', 'draft', '--json'])).json<{ data: Post[] }>()
        .data.length,
    ).toBeGreaterThanOrEqual(5)
    expect((await cli(['posts', 'list', '--status', 'approved'])).code).toBe(1) // 422
    expect((await cli(['posts', 'get', ids[0]!])).out).toContain('cli post 0')
    expect((await cli(['posts', 'get', 'post_nope00000000000000'])).code).toBe(1)
    const upd = await cli(['posts', 'update', ids[0]!, '--content', 'renamed', '--meta', 'a=1', '--json'])

    expect(upd.json<Post>()).toMatchObject({ content: 'renamed', metadata: { a: 1 }, version: 2 })
    const stale = await cli(['posts', 'update', ids[0]!, '--content', 'x', '--expected-version', '1'])

    expect(stale.code).toBe(1)
    expect(stale.err).toMatch(/version|modified/i)
    // schedule by a relative time, move with update, cancel back to a draft
    const sch = await cli(['posts', 'schedule', ids[1]!, '+3h', '--json'])

    expect(sch.json<Post>().status).toBe('scheduled')
    expect((await cli(['posts', 'update', ids[1]!, '--publish-at', '+4h', '--json'])).json<Post>().status).toBe(
      'scheduled',
    )
    expect((await cli(['posts', 'update', ids[1]!, '--clear-publish-at', '--json'])).json<Post>().status).toBe('draft')
    expect((await cli(['posts', 'schedule', ids[1]!, '+3h'])).code).toBe(0)
    expect((await cli(['posts', 'cancel', ids[1]!, '--json'])).json<Post>().status).toBe('draft')
    expect((await cli(['posts', 'cancel', ids[1]!])).code).toBe(1) // not scheduled
    expect((await cli(['posts', 'schedule', ids[1]!, '2020-01-01T00:00:00Z'])).code).toBe(1) // in the past
    // the schedule as text and JSON
    expect((await cli(['posts', 'schedule', ids[2]!, '+1d'])).code).toBe(0)
    const cal = await cli(['schedule', '--collection', colName, '--json'])

    expect(cal.code).toBe(0)
    expect(cal.json<{ data: Post[] }>().data.map((p) => p.id)).toContain(ids[2])
    expect((await cli(['schedule', '--collection', colName])).out).toContain('cli post 2')
    // duplicate, preview, history
    const copy = await cli(['posts', 'duplicate', ids[0]!, '--json'])

    expect(copy.json<Post>()).toMatchObject({ status: 'draft', content: 'renamed' })
    const preview = await cli(['posts', 'preview', ids[0]!, '--json'])

    expect(preview.json<{ targets: { channel_id: string; ok: boolean }[] }>().targets).toEqual([
      expect.objectContaining({ channel_id: channel.id, text: 'renamed', warnings: [], ok: true }),
    ])
    const hist = await cli(['posts', 'history', ids[0]!, '--json'])

    expect(hist.json<{ data: { type: string }[] }>().data.map((e) => e.type)).toContain('post.updated')
    // --no-channels empties the targets; a post without channels cannot be published
    expect((await cli(['posts', 'update', ids[3]!, '--no-channels', '--json'])).json<Post>().targets).toEqual([])
    expect((await cli(['posts', 'publish', ids[3]!])).code).toBe(1)
    expect((await cli(['posts', 'delete', ids[3]!, ids[4]!])).code).toBe(0)
    expect((await cli(['posts', 'get', ids[3]!])).code).toBe(1)
    const purge = await admin(['posts', 'purge', copy.json<Post>().id, '--yes'])

    expect(purge).toMatchObject({ code: 0 })
    // purge needs admin: the cloud agent key is refused (local mode has no keys)
    expect((await cli(['posts', 'purge', ids[0]!, '--yes'])).code).toBe(name === 'cloud' ? 1 : 0)
    expect((await cli(['posts', 'tags', '--json'])).out).toContain(tag)
  })

  it('publish, result, retry, work: the CLI as the automation', async () => {
    const c = sdk(url, key || null)
    const post = await c.posts.create({
      title: 'Post',
      collection: colId,
      content: 'automation round trip',
      targets: [{ channel_id: channel.id }],
    })
    const published = await cli(['posts', 'publish', post.id, '--json'])

    expect(published).toMatchObject({ code: 0 })
    expect(published.json<Post>().status).toBe('publishing')
    await until(async () => (await c.posts.get(post.id)).targets[0]!.attempts > 0)
    const work = await cli(['work', '--json'])

    expect(
      work
        .json<{ data: { kind: string; post?: { id: string } }[] }>()
        .data.some((w) => w.kind === 'target' && w.post?.id === post.id),
    ).toBe(true)
    expect((await cli(['work'])).out).toContain(post.id)
    // A failure first, by channel name; then a retry and a success with the link.
    const failed = await cli([
      'posts',
      'result',
      post.id,
      channel.name,
      '--status',
      'failed',
      '--error',
      'API 500',
      '--json',
    ])

    expect(failed.json<Post>()).toMatchObject({ status: 'failed' })
    expect((await cli(['posts', 'result', post.id, channel.name, '--status', 'failed'])).code).toBe(1) // not waiting any more
    expect((await cli(['posts', 'result', post.id, channel.name, '--status', 'done'])).code).toBe(64) // checked locally
    reportLater(async () => c.posts.get(post.id), 'published')
    const retried = await cli(['posts', 'retry', post.id, channel.name, '--wait', '--timeout', '20s', '--json'])

    expect(retried.code).toBe(0)
    expect(retried.json<Post>()).toMatchObject({ status: 'published' })
    expect(retried.json<Post>().targets[0]!.external_url).toBe('https://example.com/cli')
    expect((await cli(['posts', 'retry', post.id, channel.name])).code).toBe(1) // only a failed target
  })

  it('channels: providers, connect your own automation, get, update, test, remove', async () => {
    const providers = await cli(['channels', 'providers', '--json'])

    expect(providers).toMatchObject({ code: 0 })
    expect(
      providers.json<{ data: { id: string; status: string }[] }>().data.find((p) => p.id === 'custom')?.status,
    ).toBe('available')
    expect((await cli(['channels', 'providers', 'custom'])).out).toMatch(/own automation/i)
    expect((await cli(['channels', 'providers', 'pigeon'])).code).toBe(1)
    const connected = await cli([
      'channels',
      'connect',
      'custom',
      '--name',
      `cli pipeline ${name} ${Date.now()}`,
      '--json',
    ])

    expect(connected).toMatchObject({ code: 0 })
    const ch = connected.json<Channel>()

    expect(ch).toMatchObject({ provider: 'custom', status: 'connected' })
    expect((await cli(['channels', 'list', '--json'])).out).toContain(ch.id)
    expect((await cli(['channels', 'get', ch.name, '--json'])).json<Channel>().id).toBe(ch.id)
    expect(
      (await cli(['channels', 'update', ch.id, '--name', `${ch.name} renamed`, '--json'])).json<Channel>().name,
    ).toBe(`${ch.name} renamed`)
    expect((await cli(['channels', 'update', ch.id, '--disable', '--json'])).json<Channel>().status).toBe('disabled')
    expect((await cli(['channels', 'update', ch.id, '--enable', '--json'])).json<Channel>().status).toBe('connected')
    expect((await cli(['channels', 'test', ch.id])).code).toBe(0)
    // A platform that is not wired up yet refuses to connect, and a token platform needs its fields.
    const soon = providers
      .json<{ data: { id: string; status: string; auth: string }[] }>()
      .data.find((p) => p.status === 'soon')!

    expect((await cli(['channels', 'connect', soon.id, '--name', 'x'])).code).toBe(1)
    expect((await cli(['channels', 'remove', ch.id])).code).toBe(0)
    expect((await cli(['channels', 'get', ch.id])).code).toBe(1)
  })

  it('media: upload, get to a file, delete (to Deleted, restore, for good)', async () => {
    const up = await cli(['media', 'upload', png, '--json'])

    expect(up).toMatchObject({ code: 0 })
    const id = up.json<{ id: string }>().id
    const out = join(dir, `${name}-down.png`)

    expect((await cli(['media', 'get', id, '-o', out])).code).toBe(0)
    expect(readFileSync(out).equals(PNG)).toBe(true)
    // Delete moves the file to Deleted: still there, listed with --deleted, restorable.
    expect((await cli(['media', 'delete', id])).code).toBe(0)
    expect((await cli(['media', 'get', id, '-o', out])).code).toBe(0)
    expect((await cli(['media', 'list', '--deleted', '--json'])).out).toContain(id)
    expect((await cli(['media', 'restore', id])).code).toBe(0)
    // --permanent deletes it for good.
    expect((await cli(['media', 'delete', id, '--permanent'])).code).toBe(0)
    expect((await cli(['media', 'get', id, '-o', out])).code).toBe(1)
    const bad = join(dir, 'x.html')

    writeFileSync(bad, '<html><script>1</script></html>')
    expect((await cli(['media', 'upload', bad])).code).toBe(1)
  })

  it('events: tail and export (ndjson, csv)', async () => {
    const tail = await cli(['events', 'tail', '--collection', colName, '--limit', '5', '--json'])

    expect(tail).toMatchObject({ code: 0 })
    const nd = await cli(['events', 'export', '--collection', colName, '--type', 'post.*'])

    expect(nd.code).toBe(0)
    const lines = nd.out
      .trim()
      .split('\n')
      .map((l) => JSON.parse(l) as { type: string })

    expect(lines.length).toBeGreaterThan(5)
    expect(lines.every((e) => e.type.startsWith('post.'))).toBe(true)
    const csvFile = join(dir, `${name}.csv`)

    expect((await cli(['events', 'export', '--format', 'csv', '-o', csvFile, '--collection', colName])).code).toBe(0)
    expect(readFileSync(csvFile, 'utf8').split('\n')[0]).toContain('created_at,id,type')
  })

  it('webhooks: create, test, deliveries, retry, update, delete', async () => {
    const rx = await receiver()

    try {
      const created = await cli(['webhooks', 'create', '--url', rx.url, '--events', 'post.*', '--json'])

      expect(created).toMatchObject({ code: 0 })
      const hook = created.json<{ id: string; secret: string }>()

      expect(hook.secret).toMatch(/^whsec_/)
      const test = await cli(['webhooks', 'test', hook.id, '--json'])

      expect(test.code).toBe(0)
      const delivery = test.json<{ id: string }>().id

      await until(() => rx.received.some((r) => r.headers['x-staqly-delivery'] === delivery))
      await sleep(300)
      const list = await cli(['webhooks', 'deliveries', '--webhook', hook.id, '--json'])

      expect(list.out).toContain(delivery)
      expect((await cli(['webhooks', 'retry', delivery])).code).toBe(0)
      expect((await cli(['webhooks', 'get', hook.id, '--json'])).out).not.toContain(hook.secret)
      const rotated = await cli(['webhooks', 'update', hook.id, '--rotate-secret', '--json'])

      expect(rotated).toMatchObject({ code: 0 })
      expect(
        (await cli(['webhooks', 'update', hook.id, '--disable', '--json'])).json<{ active: boolean }>().active,
      ).toBe(false)
      expect((await cli(['webhooks', 'create', '--url', rx.url, '--events', 'item.approved'])).code).toBe(1)
      expect((await cli(['webhooks', 'delete', hook.id])).code).toBe(0)
      expect((await cli(['webhooks', 'get', hook.id])).code).toBe(1)
    } finally {
      rx.close()
    }
  })

  it('alerts: create, get (secrets masked), test, deliveries, update, delete', async () => {
    const rx = await receiver()

    try {
      const created = await cli([
        'alerts',
        'create',
        '--type',
        'ntfy',
        '--name',
        'phone',
        '--config',
        'topic=xc',
        '--config',
        `server_url=${rx.url.replace('/hook', '')}`,
        '--config',
        'access_token=very-secret',
        '--json',
      ])

      expect(created).toMatchObject({ code: 0 })
      const id = created.json<{ id: string }>().id

      expect((await cli(['alerts', 'get', id, '--json'])).out).not.toContain('very-secret')
      expect((await cli(['alerts', 'list', '--json'])).out).toContain(id)
      expect((await cli(['alerts', 'test', id])).code).toBe(0)
      await until(() => rx.received.some((r) => r.headers['authorization'] === 'Bearer very-secret'))
      expect((await cli(['alerts', 'deliveries', id, '--json'])).code).toBe(0)
      expect((await cli(['alerts', 'update', id, '--disable', '--json'])).json<{ enabled: boolean }>().enabled).toBe(
        false,
      )
      expect(
        (await cli(['alerts', 'update', id, '--events', 'post.failed', '--json'])).json<{ events: string[] }>().events,
      ).toEqual(['post.failed'])
      expect((await cli(['alerts', 'create', '--type', 'pigeon', '--name', 'x'])).code).toBe(64) // checked locally
      expect(
        (
          await cli([
            'alerts',
            'create',
            '--type',
            'ntfy',
            '--name',
            'x',
            '--events',
            'item.approved',
            '--config',
            'topic=t',
          ])
        ).code,
      ).toBe(1)
      expect((await cli(['alerts', 'delete', id])).code).toBe(0)
    } finally {
      rx.close()
    }
  })

  it('schedules and jobs: create (times, every, once), get, update, run, list jobs and stats, delete', async () => {
    const c = sdk(url, key || null)
    const created = await cli([
      'schedules',
      'create',
      colName,
      '--name',
      'Mornings',
      '--times',
      '09:00,17:30',
      '--weekdays',
      'mon,tue,wed',
      '--tz',
      'Europe/Warsaw',
      '--channel',
      channel.name,
      '--source',
      'agent',
      '--json',
    ])

    expect(created).toMatchObject({ code: 0 })
    const s = created.json<Schedule>()

    expect(s).toMatchObject({ kind: 'recurring', source: 'agent', channels: [channel.id] })
    expect(s.rule).toMatchObject({ times: ['09:00', '17:30'], weekdays: [1, 2, 3], tz: 'Europe/Warsaw' })
    expect((await cli(['schedules', 'list', '--json'])).out).toContain(s.id)
    expect((await cli(['schedules', 'get', s.id, '--json'])).json<Schedule>().id).toBe(s.id)
    expect((await cli(['schedules', 'update', s.id, '--every', '6h', '--json'])).json<Schedule>().rule).toMatchObject({
      every_minutes: 360,
    })
    expect((await cli(['schedules', 'update', s.id, '--disable', '--json'])).json<Schedule>().enabled).toBe(false)
    expect((await cli(['schedules', 'update', s.id, '--enable', '--json'])).json<Schedule>().enabled).toBe(true)
    const once = await cli([
      'schedules',
      'create',
      colName,
      '--name',
      'Launch',
      '--at',
      '+1d',
      '--pick',
      'all',
      '--json',
    ])

    expect(once.json<Schedule>().kind).toBe('once')
    expect((await cli(['schedules', 'create', colName, '--name', 'bad', '--every', '1m'])).code).not.toBe(0) // under 5 minutes
    expect((await cli(['schedules', 'create', colName, '--name', 'bad', '--at', '2020-01-01'])).code).toBe(1)
    // Run it: a waiting draft goes out on the channel.
    const draft = await c.posts.create({ title: 'Post', collection: colId, content: 'waiting for the schedule' })

    // Older drafts of this collection (earlier tests) would go first with pick: next.
    await c.schedules.update(s.id, { pick: 'selected', post_ids: [draft.id] })
    const ran = await cli(['schedules', 'run', s.id, '--json'])

    expect(ran).toMatchObject({ code: 0 })
    expect(ran.json<Schedule>().last_result).toBe('published 1')
    await until(async () => (await c.posts.get(draft.id)).status === 'publishing')
    const jobs = await cli(['jobs', 'list', '--kind', 'publish_target', '--json'])

    expect(jobs.code).toBe(0)
    expect(
      jobs.json<{ data: { payload: { post_id?: string } }[] }>().data.some((j) => j.payload.post_id === draft.id),
    ).toBe(true)
    expect((await cli(['jobs', 'stats', '--json'])).json<{ done_24h: number }>().done_24h).toBeGreaterThan(0)
    const queued = (await c.jobs.list({ kind: 'run_schedule', status: 'queued' })).data.find(
      (j) => j.payload?.schedule_id === once.json<Schedule>().id,
    )!

    expect((await cli(['jobs', 'get', queued.id, '--json'])).json<{ status: string }>().status).toBe('queued')
    expect((await cli(['jobs', 'cancel', queued.id, '--json'])).json<{ status: string }>().status).toBe('cancelled')
    expect((await cli(['jobs', 'retry', queued.id])).code).toBe(1) // not failed
    expect((await cli(['jobs', 'list', '--status', 'nope'])).code).toBe(1)
    expect((await cli(['schedules', 'delete', s.id])).code).toBe(0)
    expect((await cli(['schedules', 'delete', once.json<Schedule>().id])).code).toBe(0)
    expect((await cli(['schedules', 'get', s.id])).code).toBe(1)
  })

  it('workspace: get, usage, export to a ZIP', async () => {
    expect((await cli(['workspace', 'get', '--json'])).json<{ id: string }>().id).toMatch(/^ws_/)
    const usage = await cli(['workspace', 'usage', '--json'])

    expect(usage.json<{ plan: { id: string } }>().plan.id).toBe(name === 'local' ? 'unlocked' : 'pro')
    const zip = join(dir, `${name}.zip`)
    const exp = await admin(['workspace', 'export', 'start', '--output', zip, '--interval', '250ms'])

    expect(exp).toMatchObject({ code: 0 })
    expect(readFileSync(zip).subarray(0, 2).toString()).toBe('PK')
    // exports need admin: the cloud agent key is refused
    expect((await cli(['workspace', 'export', 'start'])).code).toBe(name === 'cloud' ? 1 : 0)
  })

  it('mcp config prints a client configuration for this server', async () => {
    const r = await cli(['mcp', 'config', 'claude'])

    expect(r).toMatchObject({ code: 0 })
    expect(r.out).toContain(`${url}/mcp`)
    expect((await cli(['mcp', 'config'])).code).toBe(64)
  })
})

describe('cloud-only CLI: keys, plans, sessions, profiles', () => {
  let adminCLI: ReturnType<typeof cliRunner>

  beforeAll(() => {
    adminCLI = cliRunner(bin, cloud.url, ws.adminKey)
  })

  afterAll(async () => {
    // leave nothing behind that other files rely on: the workspace is this file's own
  })

  it('a wrong key is exit 1 with an authentication error', async () => {
    const r = await cliRunner(bin, cloud.url, 'stk_wrongwrongwrong')(['posts', 'list'])

    expect(r.code).toBe(1)
    expect(r.err).toMatch(/401|key|auth/i)
    expect((await cliRunner(bin, cloud.url, '')(['posts', 'list'])).code).toBe(1)
  })

  it('keys: list, create, rotate, revoke (admin); an agent key is refused; scopes apply', async () => {
    expect((await cliRunner(bin, cloud.url, ws.agentKey)(['keys', 'list'])).code).toBe(1)
    const created = await adminCLI([
      'keys',
      'create',
      '--name',
      'ci',
      '--scope',
      'posts:read,collections:read',
      '--json',
    ])

    expect(created).toMatchObject({ code: 0 })
    const k = created.json<{ id: string; key: string }>()

    expect((await adminCLI(['keys', 'list', '--json'])).out).toContain(k.id)
    const rotated = await adminCLI(['keys', 'rotate', k.id, '--json'])

    expect(rotated).toMatchObject({ code: 0 })
    const nk = rotated.json<{ id: string; key: string }>()

    expect((await cliRunner(bin, cloud.url, k.key)(['posts', 'list'])).code).toBe(1) // old key revoked
    expect((await cliRunner(bin, cloud.url, nk.key)(['posts', 'list'])).code).toBe(0)
    expect((await cliRunner(bin, cloud.url, nk.key)(['collections', 'create', 'nope'])).code).toBe(1) // scope
    expect((await adminCLI(['keys', 'revoke', nk.id])).code).toBe(0)
    expect((await adminCLI(['keys', 'create', '--name', 'bad', '--scope', 'items:read'])).code).not.toBe(0)
  })

  it('sessions list and plans list', async () => {
    expect((await adminCLI(['sessions', 'list', '--json'])).code).toBe(0)
    const plans = (await adminCLI(['plans', 'list', '--json'])).json<{ data: { id: string }[] }>().data

    expect(plans.map((p) => p.id)).toEqual(['starter', 'plus', 'pro', 'business'])
    expect((await adminCLI(['plans', 'free', '--yes'])).code).not.toBe(0) // there is no free plan
  })

  it('login stores a profile that later commands use; logout removes it', async () => {
    const home = mkdtempSync(join(tmpdir(), 'staqly-xc-profile-'))
    const noEnv = cliRunner(bin, '', '', home)
    const login = await noEnv(['login', '--url', cloud.url, '--key', ws.agentKey, '--profile', 'xc'])

    expect(login).toMatchObject({ code: 0 })
    expect(readFileSync(join(home, 'config.json'), 'utf8')).toContain('"xc"')
    const profiles = await noEnv(['profiles', 'list', '--json'])

    expect(profiles.out).toContain('xc')
    expect(profiles.out).not.toContain(ws.agentKey) // the key is not printed
    const st = await noEnv(['status', '--profile', 'xc', '--json'])

    expect(st).toMatchObject({ code: 0 })
    expect(st.json<{ session: { mode: string } }>().session.mode).toBe('cloud')
    expect((await noEnv(['login', '--url', cloud.url, '--key', 'stk_bad', '--profile', 'bad'])).code).toBe(1)
    expect((await noEnv(['logout', '--profile', 'xc'])).code).toBe(0)
    expect((await noEnv(['profiles', 'list', '--json'])).out).not.toContain('"xc"')
  })
})

describe('cloud-only CLI: billing without Stripe, operator commands', () => {
  it('billing: status says disabled; checkout is refused', async () => {
    const admin = cliRunner(bin, cloud.url, ws.adminKey)
    const st = await admin(['billing', 'status', '--json'])

    expect(st).toMatchObject({ code: 0 })
    expect(st.json<{ enabled: boolean }>().enabled).toBe(false)
    const co = await admin(['billing', 'checkout'])

    expect(co.code).toBe(1)
    expect(co.err.length).toBeGreaterThan(0)
  })

  it('staqly serve workspaces / stats / set-plan run against a live data directory', async () => {
    const op = (args: string[]) =>
      run(bin, ['serve', '--data', cloud.dataDir, ...args], {
        env: {
          PATH: process.env.PATH,
          HOME: dir,
          STAQLY_MODE: 'cloud',
          AUTH_DEV_SIGNIN: 'true',
          DATABASE_URL: cloud.databaseURL,
          LOG_LEVEL: 'error',
        },
      })
    const list = await op(['workspaces'])

    expect(list).toMatchObject({ code: 0 })
    expect(list.out).toContain(ws.workspaceId)
    expect((await op(['stats'])).code).toBe(0)
    const set = await op(['set-plan', ws.workspaceId, 'pro'])

    expect(set).toMatchObject({ code: 0 })
    expect((await sdk(cloud.url, ws.agentKey).workspace.usage()).plan.id).toBe('pro')
    // the operator command's own flag after `staqly serve`
    const comp = await op(['set-plan', ws.workspaceId, 'pro', '--until', '2099-12-31'])

    expect(comp).toMatchObject({ code: 0 })
    expect((await sdk(cloud.url, ws.agentKey).workspace.usage()).plan_expires_at).toMatch(/^2099-12-31/)
    expect((await op(['set-plan', ws.workspaceId, 'pro'])).code).toBe(0)
    expect((await op(['set-plan', 'ws_nope', 'pro'])).code).not.toBe(0)
  })
})

describe('local-only CLI: the access token and connect', () => {
  it('without a key or a token file the local server refuses, with a hint where the token is', async () => {
    const r = await cliRunner(bin, local.url)(['posts', 'list'])

    expect(r.code).toBe(1)
    expect(r.err).toContain('staqly serve token')
  })

  it("reads the token from `staqly serve`'s data folder by itself, and connect prints a one-time link", async () => {
    const r = await cliRunner(bin, local.url)(['connect'], { env: { STAQLY_DATA_DIR: local.dataDir } })

    expect(r).toMatchObject({ code: 0 })
    expect(r.out).toMatch(/https?:\/\/\S+#\S+/)
  })

  it('`staqly serve token` prints the token of the same data', async () => {
    const r = await run(bin, ['serve', '--data', local.dataDir, 'token'], {
      env: { PATH: process.env.PATH, HOME: mkdtempSync(join(tmpdir(), 'staqly-xc-tok-')), STAQLY_MODE: 'local' },
    })

    expect(r).toMatchObject({ code: 0 })
    expect(r.out.trim()).toBe(local.token)
    expect(r.err).toContain(`#token=${local.token}`)
  })
})
