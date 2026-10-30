// The SDK methods the other files do not exercise, called live through the
// TypeScript SDK and the Python SDK (sync and async): both must succeed the
// same way, or fail with the same typed error.
import {
  AuthenticationError,
  ConflictError,
  GoneError,
  NotFoundError,
  PermissionDeniedError,
  type Staqly,
  ValidationError,
} from '@staqly/sdk'
import { afterAll, beforeAll, describe, expect, inject, it } from 'vitest'

import { type PyClient, PythonSDK, sdk, signupRaw } from './lib.js'

const local = inject('local')
const cloud = inject('cloud')
const MODES = ['sync', 'async'] as const

let py: PythonSDK

beforeAll(async () => {
  py = new PythonSDK()
  await py.start()
})

afterAll(() => py?.stop())

async function errorOf(p: Promise<unknown>): Promise<Error & { status?: number; code?: string }> {
  try {
    await p
  } catch (err) {
    return err as Error & { status?: number; code?: string }
  }

  throw new Error('expected the call to fail')
}

/** Signs a cloud workspace up and keeps everything signup returns. */
const freshWorkspace = () => signupRaw(cloud.url, `sweep-${Date.now()}`)

describe.each(['local', 'cloud'] as const)('%s: method sweep', (name) => {
  let c: Staqly
  let pyc: PyClient

  beforeAll(async () => {
    if (name === 'local') {
      c = sdk(local.url, local.token)
      pyc = { base_url: local.url, api_key: local.token }
    } else {
      const adminKey = (await freshWorkspace()).api_key.key

      c = sdk(cloud.url, adminKey)
      pyc = { base_url: cloud.url, api_key: adminKey }
    }
  })

  it('server info: health, ready, live, setup, openapi, whoami, metrics', async () => {
    expect((await c.health()).status).toBe('ok')
    expect((await c.ready()).status).toBe('ok')
    expect((await c.live()).status).toBe('ok')
    expect((await c.setup()).mode).toBe(name)
    expect(await c.openapi()).toContain('openapi:')
    const me = await c.whoami()

    for (const mode of MODES) {
      expect((await py.call<{ status: string }>(mode, pyc, 'health')).status).toBe('ok')
      expect((await py.call<{ status: string }>(mode, pyc, 'ready')).status).toBe('ok')
      expect((await py.call<{ status: string }>(mode, pyc, 'live')).status).toBe('ok')
      expect((await py.call<{ mode: string }>(mode, pyc, 'setup')).mode).toBe(name)
      expect(await py.call<string>(mode, pyc, 'openapi')).toContain('openapi:')
      expect(await py.call(mode, pyc, 'whoami')).toEqual(me)
    }

    // /metrics answers this machine without a token in both modes (METRICS_TOKEN is for other hosts)
    const metrics = await c.metrics().catch((e: Error & { status?: number }) => `status ${e.status}`)
    const pyMetrics = await py
      .call<string>('sync', pyc, 'metrics')
      .catch((e: { error: { status?: number } }) => `status ${e.error.status}`)

    expect(metrics).toContain('staqly_')
    expect(pyMetrics).toContain('staqly_')
  })

  it('collections: list, listPosts, the processing timeout, archive, unarchive, update, delete', async () => {
    const col = await c.collections.create({ name: `sweep ${Date.now()}`, processing_timeout_seconds: 7200 })

    expect(col.processing_timeout_seconds).toBe(7200)
    await c.posts.create({ title: 'Post', collection: col.id, content: 'one' })
    expect((await c.collections.listPosts(col.name)).data).toHaveLength(1)
    // Python sets and clears the processing timeout the same way; 30 s is refused.
    expect(
      (
        await py.call<{ processing_timeout_seconds: number | null }>('sync', pyc, 'collections.update', [col.id], {
          processing_timeout_seconds: 3600,
        })
      ).processing_timeout_seconds,
    ).toBe(3600)
    expect(
      (
        await py.call<{ processing_timeout_seconds: number | null }>('async', pyc, 'collections.update', [col.id], {
          clear_processing_timeout: true,
        })
      ).processing_timeout_seconds,
    ).toBeNull()
    expect(
      (await py.fail('sync', pyc, 'collections.create', [`bad ${Date.now()}`], { processing_timeout_seconds: 30 }))
        .status,
    ).toBe(422)

    for (const mode of MODES) {
      expect((await py.call<{ data: unknown[] }>(mode, pyc, 'collections.list_posts', [col.name])).data).toHaveLength(1)
    }

    expect((await c.collections.archive(col.id)).archived).toBe(true)
    expect((await errorOf(c.collections.archive(col.id))).status).toBe(409)
    expect((await py.fail('sync', pyc, 'collections.archive', [col.id])).type).toBe('ConflictError')
    // An archived collection takes no new posts.
    expect(await errorOf(c.posts.create({ title: 'Post', collection: col.id, content: 'x' }))).toBeInstanceOf(
      ConflictError,
    )
    expect((await py.call<{ archived: boolean }>('async', pyc, 'collections.unarchive', [col.id])).archived).toBe(false)
    expect(
      (await py.call<{ description: string }>('sync', pyc, 'collections.update', [col.id], { description: 'py' }))
        .description,
    ).toBe('py')
    const wrong = await errorOf(c.collections.delete(col.id, 'not the name'))

    expect(wrong.status).toBe(422)
    expect((await py.fail('async', pyc, 'collections.delete', [col.id], { confirm: 'nope' })).status).toBe(422)
    await py.call('sync', pyc, 'collections.delete', [col.id], { confirm: col.name })
    expect(await errorOf(c.collections.get(col.id))).toBeInstanceOf(NotFoundError)
  })

  it('posts: search, tags, preview warnings, moving between collections, purge', async () => {
    const a = await c.collections.create({ name: `sweep posts a ${Date.now()}` })
    const b = await c.collections.create({ name: `sweep posts b ${Date.now()}` })
    const channel = await c.channels.connect({ provider: 'custom', name: `sweep channel ${Date.now()}` })
    const post = await c.posts.create({
      title: 'Post',
      collection: a.id,
      content: 'Quokka avatar smiling',
      tags: ['sweep', 'quokka'],
      targets: [{ channel_id: channel.id, content: 'short' }],
    })

    expect((await c.posts.search('quokk', { collection_id: a.id })).data.map((p) => p.id)).toEqual([post.id])
    expect(
      (
        await py.call<{ data: { id: string }[] }>('sync', pyc, 'posts.search', ['quokk'], { collection_id: a.id })
      ).data.map((p) => p.id),
    ).toEqual([post.id])
    expect((await c.posts.tags()).find((t) => t.name === 'quokka')?.count).toBeGreaterThanOrEqual(1)
    expect((await py.call<{ name: string }[]>('async', pyc, 'posts.tags')).some((t) => t.name === 'quokka')).toBe(true)
    const preview = await c.posts.preview(post.id)

    expect(preview.targets).toEqual([
      expect.objectContaining({ channel_id: channel.id, text: 'short', warnings: [], ok: true }),
    ])
    expect(await py.call('sync', pyc, 'posts.preview', [post.id])).toEqual(preview)
    const moved = await py.call<{ collection_id: string }>('async', pyc, 'posts.update', [post.id], {
      collection: b.name,
    })

    expect(moved.collection_id).toBe(b.id)
    expect((await c.collections.listPosts(a.id)).data).toHaveLength(0)
    // Purging needs admin (local mode and the cloud admin key have it); only post.purged remains.
    const purged = await c.posts.purge(post.id)

    expect(purged).toMatchObject({})
    expect(await errorOf(c.posts.get(post.id))).toBeInstanceOf(NotFoundError)
    expect((await py.fail('sync', pyc, 'posts.purge', [post.id])).type).toBe('NotFoundError')
  })

  it('channels and providers: the catalog, a platform on the way, test, disable, enable, remove', async () => {
    const providers = await c.providers.list()
    const custom = providers.find((p) => p.id === 'custom')!

    expect(custom).toMatchObject({ status: 'available', auth: 'none' })
    expect(custom.limits.max_media).toBeGreaterThan(0)
    // The Python models fill optional fields with their defaults (a field's placeholder: None): compare what matters.
    expect(brief((await py.call('sync', pyc, 'providers.list')) as typeof providers)).toEqual(brief(providers))
    // A token platform on the way (an OAuth one listed as soon only lacks its app keys on this server).
    const soon = providers.find((p) => p.status === 'soon' && p.auth !== 'oauth2')!
    const refused = await errorOf(c.channels.connect({ provider: soon.id, fields: { token: 'x' } }))

    // A platform on the way is a 409 provider_soon, the same in both SDKs.
    expect(refused).toBeInstanceOf(ConflictError)
    expect(String(refused)).toMatch(/409 provider_soon/)
    expect((await py.fail('async', pyc, 'channels.connect', [soon.id], { fields: { token: 'x' } })).type).toBe(
      'ConflictError',
    )
    expect(await errorOf(c.channels.connect({ provider: 'pigeon' }))).toBeInstanceOf(ValidationError)
    const ch = await py.call<{ id: string; name: string; status: string }>(
      'sync',
      pyc,
      'channels.connect',
      ['custom'],
      { name: `sweep ch ${Date.now()}` },
    )

    expect(ch.status).toBe('connected')
    expect((await c.channels.test(ch.id)).ok).toBe(true)
    expect((await py.call<{ ok: boolean }>('async', pyc, 'channels.test', [ch.id])).ok).toBe(true)
    expect((await c.channels.disable(ch.id)).status).toBe('disabled')
    expect((await py.call<{ status: string }>('sync', pyc, 'channels.enable', [ch.id])).status).toBe('connected')
    expect((await py.call<{ name: string }>('async', pyc, 'channels.update', [ch.id], { name: 'renamed' })).name).toBe(
      'renamed',
    )
    expect((await c.channels.list()).some((x) => x.id === ch.id)).toBe(true)
    await c.channels.remove(ch.id)
    expect((await py.fail('sync', pyc, 'channels.get', [ch.id])).type).toBe('NotFoundError')
    // OAuth platforms are on the way: starting one is refused the same way in both SDKs.
    const oauth = providers.find((p) => p.auth === 'oauth2')!
    const ts = await errorOf(c.channels.oauthStart(oauth.id))
    const pe = await py.fail('sync', pyc, 'channels.oauth_start', [oauth.id])

    expect([pe.type, pe.status]).toEqual([ts.constructor.name, ts.status])
  })

  it('alerts: create, enable, update config (masked secrets), test against nothing, deliveries, delete', async () => {
    const alert = await c.alerts.create({
      name: 'sweep',
      type: 'ntfy',
      config: { topic: 'x', server_url: 'https://ntfy.invalid', access_token: 'tk_secret' },
      enabled: false,
    })

    expect(JSON.stringify(alert.config)).not.toContain('tk_secret')
    expect((await py.call<{ enabled: boolean }>('sync', pyc, 'alerts.enable', [alert.id])).enabled).toBe(true)
    expect(
      (await py.call<{ events: string[] }>('async', pyc, 'alerts.update', [alert.id], { events: ['post.failed'] }))
        .events,
    ).toEqual(['post.failed'])
    const test = await c.alerts.test(alert.id)

    expect(test.ok).toBe(false) // nothing listens at ntfy.invalid
    expect((await py.call<{ ok: boolean }>('sync', pyc, 'alerts.test', [alert.id])).ok).toBe(false)
    expect(Array.isArray(await c.alerts.deliveries(alert.id))).toBe(true)
    expect(
      await errorOf(c.alerts.create({ name: 'bad', type: 'ntfy', config: {}, events: ['item.approved'] })),
    ).toBeInstanceOf(ValidationError)
    await c.alerts.delete(alert.id)
    expect((await py.fail('async', pyc, 'alerts.get', [alert.id])).type).toBe('NotFoundError')
  })

  it('workspace: get, agents, forgetAgent, rename; exports: list and get', async () => {
    const wsTs = await c.workspace.get()

    for (const mode of MODES) {
      expect((await py.call<{ id: string }>(mode, pyc, 'workspace.get')).id).toBe(wsTs.id)
    }

    const agents = await c.workspace.agents()

    expect(agents.some((a) => /staqly-sdk/.test(a.name ?? '') || /staqly-sdk/.test(JSON.stringify(a)))).toBe(true)
    expect(await errorOf(c.workspace.forgetAgent('agt_nope'))).toBeInstanceOf(NotFoundError)
    expect((await py.fail('sync', pyc, 'workspace.forget_agent', ['agt_nope'])).type).toBe('NotFoundError')
    const renamed = await py.call<{ name: string }>('async', pyc, 'workspace.rename', [wsTs.name])

    expect(renamed.name).toBe(wsTs.name)
    const job = await c.exports.exportWorkspace({ timeout: 60_000, pollInterval: 250 })

    expect((await c.exports.listWorkspaceExports()).map((j) => j.id)).toContain(job.id)

    for (const mode of MODES) {
      expect((await py.call<{ status: string }>(mode, pyc, 'exports.get_workspace_export', [job.id])).status).toBe(
        'ready',
      )
    }
  })
})

const brief = (list: { id: string; status: string; auth: string }[]) => list.map((p) => [p.id, p.status, p.auth])

describe('cloud: sessions, device links and recovery', () => {
  let ws: Awaited<ReturnType<typeof freshWorkspace>>
  let admin: Staqly
  let pyAdmin: PyClient

  beforeAll(async () => {
    ws = await freshWorkspace()
    admin = sdk(cloud.url, ws.api_key.key)
    pyAdmin = { base_url: cloud.url, api_key: ws.api_key.key }
  })

  it('signInWithApiKey: admin key gets a session, an agent key is refused, a bad key is 401', async () => {
    const agent = await admin.apiKeys.create({ name: 'agent', scopes: ['posts:read'] })

    expect((await sdk(cloud.url, null).sessions.signInWithApiKey(ws.api_key.key)).csrf_token).toBeTruthy()
    expect(await errorOf(sdk(cloud.url, null).sessions.signInWithApiKey(agent.key!))).toBeInstanceOf(
      PermissionDeniedError,
    )
    expect(await errorOf(sdk(cloud.url, null).sessions.signInWithApiKey('stk_bad'))).toBeInstanceOf(AuthenticationError)

    for (const mode of MODES) {
      const anon = { base_url: cloud.url, api_key: '' }

      expect(
        (await py.call<{ csrf_token: string }>(mode, anon, 'sessions.sign_in_with_api_key', [ws.api_key.key]))
          .csrf_token,
      ).toBeTruthy()
      expect((await py.fail(mode, anon, 'sessions.sign_in_with_api_key', [agent.key!])).type).toBe(
        'PermissionDeniedError',
      )
    }

    expect(await errorOf(admin.workspace.rotateRecoveryKey())).toBeInstanceOf(PermissionDeniedError) // sessions only
    expect((await py.fail('sync', pyAdmin, 'workspace.rotate_recovery_key')).type).toBe('PermissionDeniedError')
    expect(await errorOf(admin.workspace.delete(ws.workspace.name))).toBeInstanceOf(PermissionDeniedError)
  })

  it('device links: created from a dashboard session, redeemed once (then 410 in both SDKs)', async () => {
    // API keys may not create them (403); a browser session signed in with the admin key may
    expect(await errorOf(admin.deviceLinks.create())).toBeInstanceOf(PermissionDeniedError)
    const res = await fetch(`${cloud.url}/v1/sessions/api-key`, {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ api_key: ws.api_key.key }),
    })
    const { csrf_token: csrf } = (await res.json()) as { csrf_token: string }
    const cookie = res.headers
      .getSetCookie()
      .map((v) => v.split(';')[0])
      .join('; ')
    const browser = { Cookie: cookie, 'X-CSRF-Token': csrf }
    const link = await sdk(cloud.url, null, { defaultHeaders: browser }).deviceLinks.create()
    const token = link.url.slice(link.url.indexOf('#') + 1)

    expect((await sdk(cloud.url, null).deviceLinks.redeem(token)).csrf_token).toBeTruthy()
    expect(await errorOf(sdk(cloud.url, null).deviceLinks.redeem(token))).toBeInstanceOf(GoneError)

    for (const mode of MODES) {
      expect((await py.fail(mode, { base_url: cloud.url, api_key: '' }, 'device_links.redeem', [token])).type).toBe(
        'GoneError',
      )
    }

    const link2 = await py.call<{ url: string }>(
      'async',
      { ...pyAdmin, api_key: '', default_headers: browser },
      'device_links.create',
    )

    expect(link2.url).toContain('#')
  })

  it('billing without Stripe: get says disabled; checkout and portal fail the same way', async () => {
    expect((await admin.billing.get()).enabled).toBe(false)
    const ts = await errorOf(admin.billing.checkout())

    for (const mode of MODES) {
      expect((await py.call<{ enabled: boolean }>(mode, pyAdmin, 'billing.get')).enabled).toBe(false)
      const e = await py.fail(mode, pyAdmin, 'billing.checkout')

      expect([e.status, e.code]).toEqual([ts.status, ts.code])
      expect((await py.fail(mode, pyAdmin, 'billing.portal')).status).toBe(
        (await errorOf(admin.billing.portal())).status,
      )
    }
  })

  it('recover: the recovery key gives a new admin key and works once', async () => {
    const anon = sdk(cloud.url, null)
    const rec = await anon.sessions.recover(ws.recovery_key)

    expect(rec.api_key.key).toMatch(/^stk_/)
    expect(rec.recovery_key).not.toBe(ws.recovery_key)
    expect(await errorOf(anon.sessions.recover(ws.recovery_key))).toBeInstanceOf(AuthenticationError)
    const next = await py.call<{ recovery_key: string }>(
      'sync',
      { base_url: cloud.url, api_key: '' },
      'sessions.recover',
      [rec.recovery_key],
    )

    expect(next.recovery_key).toMatch(/\S/)
    expect(
      (await py.fail('async', { base_url: cloud.url, api_key: '' }, 'sessions.recover', [rec.recovery_key])).type,
    ).toBe('AuthenticationError')
  })
})
