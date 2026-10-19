// Live integration tests against a real server. Skipped unless STAQLY_LIVE=1.
//
//   STAQLY_LIVE=1 STAQLY_API_URL=http://127.0.0.1:3333 pnpm test            # local mode (keyless)
//   STAQLY_LIVE=1 STAQLY_API_URL=http://127.0.0.1:3334 pnpm test            # cloud mode: a workspace is created
//   STAQLY_LIVE=1 STAQLY_API_URL=https://... STAQLY_API_KEY=stk_... pnpm test   # cloud mode with your key
//
// Publishing goes through a channel of provider `custom` (your own automation), so no
// platform credentials are needed: the test reports the result itself.
import { createServer, type Server } from 'node:http'
import type { AddressInfo } from 'node:net'

import { afterAll, beforeAll, describe, expect, it } from 'vitest'

import {
  ConflictError,
  NotFoundError,
  PermissionDeniedError,
  PlanLimitError,
  type Post,
  SCOPES,
  Staqly,
  verifyWebhook,
  type WebhookEvent,
} from '../src/index.js'

const live = process.env.STAQLY_LIVE === '1'
const PNG = Uint8Array.from(
  Buffer.from(
    'iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAYAAAAfFcSJAAAADUlEQVR42mNk+M9QDwADhgGAWjR9awAAAABJRU5ErkJggg==',
    'base64',
  ),
)

describe.skipIf(!live)('live', () => {
  let c: Staqly
  let admin: Staqly | undefined // cloud: the signup browser session (all scopes)
  let mode: 'local' | 'cloud'
  const name = `SDK-TS ${Date.now()}`
  let colId = ''
  let channelId = ''
  const received: { headers: Record<string, string | string[] | undefined>; body: string }[] = []
  let receiver: Server
  let receiverURL = ''

  beforeAll(async () => {
    const probe = new Staqly({ apiKey: null })
    const setup = await probe.http.request<{ mode: 'local' | 'cloud' }>({ method: 'GET', path: '/v1/setup' })

    mode = setup.mode
    let apiKey = process.env.STAQLY_API_KEY

    if (mode === 'cloud' && !apiKey) {
      const res = await probe.http.request<Response>({
        method: 'POST',
        path: '/v1/workspaces',
        body: { name: `sdk-ts-${Date.now()}` },
        noAuth: true,
        raw: true,
      })
      const ws = (await res.json()) as { api_key: { key: string }; csrf_token: string }

      expect(ws.api_key.key).toMatch(/^stk_/)
      const cookie = res.headers
        .getSetCookie()
        .map((v) => v.split(';')[0])
        .join('; ')

      admin = new Staqly({ apiKey: null, defaultHeaders: { Cookie: cookie, 'X-CSRF-Token': ws.csrf_token } })
      const key = await admin.apiKeys.create({ name: 'sdk live', scopes: SCOPES.filter((s) => s !== 'admin') })

      apiKey = key.key
    }

    c = new Staqly({ apiKey: apiKey ?? null })
    receiver = createServer((req, res) => {
      const chunks: Buffer[] = []

      req.on('data', (d) => chunks.push(d))
      req.on('end', () => {
        received.push({ headers: req.headers, body: Buffer.concat(chunks).toString('utf8') })
        res.writeHead(200).end('ok')
      })
    })
    await new Promise<void>((r) => receiver.listen(0, '127.0.0.1', r))
    receiverURL = `http://127.0.0.1:${(receiver.address() as AddressInfo).port}/hook`
  })

  afterAll(async () => {
    receiver?.close()

    if (channelId) {
      await c.channels.remove(channelId).catch(() => undefined)
    }

    if (colId) {
      await c.collections.delete(colId, name).catch(() => undefined)
    }
  })

  it('health, whoami, workspace, usage, plans', async () => {
    expect((await c.health()).status).toBe('ok')
    expect((await c.whoami()).mode).toBe(mode)
    expect((await c.workspace.get()).id).toMatch(/^ws_/)
    const usage = await c.workspace.usage()

    if (mode === 'local') {
      expect(usage.plan.id).toBe('unlocked')
    }

    expect((await c.plans.list()).map((p) => p.id)).toContain('free')
    expect((await c.live()).status).toBe('ok')
    expect(await c.openapi()).toContain('openapi:')
  })

  it('collections and a channel on your own automation', async () => {
    const col = await c.collections.create({ name, description: 'SDK live test', processing_timeout_seconds: 600 })

    colId = col.id
    expect((await c.collections.get(name)).id).toBe(colId)
    expect((await c.collections.update(colId, { color: '#ff0000' })).color).toBe('#ff0000')
    await expect(c.collections.get('no such collection 123')).rejects.toBeInstanceOf(NotFoundError)

    const providers = await c.providers.list()

    expect(providers.map((p) => p.id)).toContain('custom')
    const ch = await c.channels.connect({ provider: 'custom', name })

    channelId = ch.id
    expect(ch.status).toBe('connected')
    expect((await c.channels.list()).some((x) => x.id === channelId)).toBe(true)
    expect((await c.channels.test(channelId)).ok).toBe(true)
  })

  it('draft → schedule → cancel, then publish and report the result', async () => {
    const media = await c.media.upload(PNG, { filename: 'a.png', contentType: 'image/png' })
    const draft = await c.posts.create({
      collection: name,
      content: 'Hello from the SDK',
      media: [media.id],
      targets: [{ channel_id: channelId }],
      tags: ['sdk'],
      source: 'sdk-ts',
    })

    expect(draft.status).toBe('draft')
    expect(draft.media_items[0]!.id).toBe(media.id)
    const when = new Date(Date.now() + 86_400_000)
    const scheduled = await c.posts.schedule(draft.id, when)

    expect(scheduled.status).toBe('scheduled')
    expect((await c.posts.list({ collection_id: colId, status: 'scheduled' })).data.map((p) => p.id)).toContain(
      draft.id,
    )
    expect((await c.posts.cancel(draft.id)).status).toBe('draft')

    const publishing = await c.posts.publish(draft.id)

    expect(publishing.status).toBe('publishing')
    // our own automation picks the work up and reports where the post went
    let work = await c.work.list()

    for (let i = 0; i < 40 && !work.some((w) => w.post?.id === draft.id); i++) {
      await new Promise((r) => setTimeout(r, 250))
      work = await c.work.list()
    }

    expect(work.some((w) => w.kind === 'target' && w.post?.id === draft.id)).toBe(true)
    const done = await c.posts.reportResult(draft.id, channelId, {
      status: 'published',
      url: 'https://example.com/p/1',
    })

    expect(done.status).toBe('published')
    expect(done.targets[0]!.external_url).toBe('https://example.com/p/1')
    expect((await c.posts.waitForPublished(draft.id, { timeout: 5_000 })).status).toBe('published')
    expect((await c.posts.history(draft.id)).data.some((e) => e.type === 'post.published')).toBe(true)
    await expect(c.posts.update(draft.id, { content: 'late edit' })).rejects.toBeInstanceOf(ConflictError)
    const copy = await c.posts.duplicate(draft.id)

    expect(copy.status).toBe('draft')
    await c.posts.delete(copy.id)
    await expect(c.posts.get(copy.id)).rejects.toBeInstanceOf(NotFoundError)
  })

  it('failed target, retry, iterate and tags', async () => {
    const p = await c.posts.create({
      collection: colId,
      content: 'will fail',
      targets: [{ channel_id: channelId }],
      publish_now: true,
      tags: ['sdk', 'fail'],
    })
    const failed = await c.posts.reportResult(p.id, channelId, { status: 'failed', error: 'nope' })

    expect(failed.status).toBe('failed')
    expect((await c.posts.retryTarget(p.id, channelId)).status).toBe('publishing')
    await c.posts.reportResult(p.id, channelId, { status: 'published' })
    const all: Post[] = []

    for await (const x of c.posts.iterate({ collection_id: colId, tag: 'sdk', limit: 1 })) {
      all.push(x)
    }

    expect(all.length).toBeGreaterThanOrEqual(2)
    expect((await c.posts.tags()).some((t) => t.name === 'sdk')).toBe(true)
    expect((await c.posts.preview(p.id)).targets[0]!.ok).toBe(true)
  })

  it('schedules and jobs', async () => {
    const s = await c.schedules.create({
      collection: colId,
      name: 'SDK mornings',
      kind: 'recurring',
      rule: { times: ['09:00'], weekdays: [1, 2, 3, 4, 5], tz: 'Europe/Warsaw' },
      channels: [channelId],
      pick: 'next',
      source: 'agent',
    })

    expect(s.next_run_at).toBeTruthy()
    expect((await c.schedules.list({ collection_id: colId })).some((x) => x.id === s.id)).toBe(true)
    expect((await c.schedules.disable(s.id)).enabled).toBe(false)
    expect((await c.schedules.update(s.id, { rule: { every_minutes: 360 } })).rule.every_minutes).toBe(360)
    await c.schedules.run(s.id)
    const stats = await c.jobs.stats()

    expect(typeof stats.queued).toBe('number')
    const jobs = await c.jobs.list({ kind: 'run_schedule', limit: 5 })

    expect(jobs.data.length).toBeGreaterThanOrEqual(1)
    await c.schedules.delete(s.id)
    await expect(c.schedules.get(s.id)).rejects.toBeInstanceOf(NotFoundError)
  })

  it('events: list and iterate', async () => {
    const page = await c.events.list({ collection_id: colId, type: 'post.created', limit: 2 })

    expect(page.data.length).toBe(2)
    let n = 0

    for await (const e of c.events.iterate({ collection_id: colId, type_prefix: 'post.', limit: 3 })) {
      expect(e.type.startsWith('post.')).toBe(true)
      n++
    }

    expect(n).toBeGreaterThanOrEqual(4)
  })

  it('webhooks: create, test, deliveries, verify signature end to end', async () => {
    const hook = await c.webhooks.create({ url: receiverURL, events: ['*'], description: 'sdk-ts live' })

    expect(hook.secret).toMatch(/^whsec_/)
    const delivery = await c.webhooks.test(hook.id)

    expect(delivery.event_type).toBe('webhook.test')
    let status = 'pending'

    for (let i = 0; i < 40 && status === 'pending'; i++) {
      await new Promise((r) => setTimeout(r, 250))
      const list = await c.webhooks.deliveries.list({ webhook_id: hook.id })

      status = list.find((d) => d.id === delivery.id)?.status ?? 'pending'
    }

    if (status === 'succeeded') {
      const got = received.find((r) => r.headers['x-staqly-delivery'] === delivery.id)!
      const ev: WebhookEvent = await verifyWebhook(hook.secret, got.body, got.headers)

      expect(ev.type).toBe('webhook.test')
      await expect(verifyWebhook('whsec_wrong', got.body, got.headers)).rejects.toThrow()
    } else {
      console.warn(`webhook delivery ${status}: server does not deliver to loopback; signature check skipped`)
    }

    expect((await c.webhooks.rotateSecret(hook.id)).secret).not.toBe(hook.secret)
    await c.webhooks.delete(hook.id)
  })

  it('alerts: CRUD, test, deliveries', async () => {
    const a = await c.alerts.create({
      name: 'sdk phone',
      type: 'ntfy',
      config: {
        topic: `sdk-${Date.now()}`,
        server_url: receiverURL.replace('/hook', ''),
        access_token: 'secret-token',
      },
      events: ['post.published'],
      collection_ids: [colId],
    })

    expect(JSON.stringify(a.config)).not.toContain('secret-token') // secrets come back masked
    expect((await c.alerts.get(a.id)).name).toBe('sdk phone')
    expect(typeof (await c.alerts.test(a.id)).ok).toBe('boolean')
    expect(Array.isArray(await c.alerts.deliveries(a.id, { limit: 10 }))).toBe(true)
    expect((await c.alerts.disable(a.id)).enabled).toBe(false)
    await c.alerts.delete(a.id)
    await expect(c.alerts.get(a.id)).rejects.toBeInstanceOf(NotFoundError)
  })

  it('exports: stream events, export the workspace, purge a post', async () => {
    const events = []

    for await (const e of c.exports.events({ collection_id: colId, type: 'post.created' })) {
      events.push(e)
    }

    expect(events.length).toBeGreaterThanOrEqual(2)
    const owner = admin ?? c // workspace export + purge need admin
    const job = await owner.exports.exportWorkspace({ timeout: 60_000, pollInterval: 250 })

    expect(job.status).toBe('ready')
    const zip = await owner.exports.downloadWorkspaceExport(job.id)

    expect([zip[0], zip[1]]).toEqual([0x50, 0x4b]) // "PK"
    const doomed = await c.posts.create({ collection: colId, content: 'to purge' })
    const purged = await owner.posts.purge(doomed.id)

    expect(purged.post_id).toBe(doomed.id)
    await expect(c.posts.get(doomed.id)).rejects.toBeInstanceOf(NotFoundError)
    expect((await c.ready()).status).toBe('ok')
  })

  it('cloud: scopes and plan limits', async () => {
    if (mode !== 'cloud') {
      return
    }

    await expect(c.apiKeys.list()).rejects.toBeInstanceOf(PermissionDeniedError)

    if (admin) {
      const extra = await admin.apiKeys.create({ name: 'temp', scopes: ['posts:read'], expires_at: '2099-01-01' })
      const ro = new Staqly({ apiKey: extra.key! })

      await expect(ro.posts.create({ collection: colId, content: 'nope' })).rejects.toBeInstanceOf(
        PermissionDeniedError,
      )
      await admin.apiKeys.revoke(extra.id)
      expect((await admin.workspace.setPlan('free')).plan.id).toBe('free')
    }

    if ((await c.workspace.usage()).plan.id !== 'free') {
      return
    }

    const max = (await c.workspace.usage()).plan.limits.webhooks
    const hooks = []

    try {
      for (let i = (await c.webhooks.list()).length; i < max; i++) {
        hooks.push(await c.webhooks.create({ url: 'https://example.com/hook' }))
      }

      const err = (await c.webhooks.create({ url: 'https://example.com/hook' }).catch((e) => e)) as PlanLimitError

      expect(err).toBeInstanceOf(PlanLimitError)
      expect(err.limit).toBe('webhooks')
    } finally {
      for (const h of hooks) {
        await c.webhooks.delete(h.id)
      }
    }
  })
})
