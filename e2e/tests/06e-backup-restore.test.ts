// scripts/backup.sh and scripts/restore.sh, run as they are, from one install into a fresh, empty one.
// Both installs are containers of the e2e image with their own volumes and their own keys (no
// STAQLY_LOCAL_TOKEN, so the access token is the access_token file a real install makes). Afterwards
// the restored install has the posts, collection, media bytes, webhook and schedule, accepts the old
// install's access token, and its sealed secrets still open: the custom channel's endpoint header
// (secret.key) reaches the receiver on a publish, the webhook delivers, and the ntfy alert's access token (notify_secret.key)
// on a test push. A third install restored from the database alone (no keys) says so: the publish fails at once
// asking to reconnect, the channel turns needs_reconnect, and the alert's test is a 422 asking for its secret again.
import { execFile } from 'node:child_process'
import { copyFileSync, mkdtempSync, rmSync } from 'node:fs'
import { join } from 'node:path'
import { promisify } from 'node:util'

import { afterAll, beforeAll, describe, expect, it } from 'vitest'

import {
  Api,
  type Alert,
  type ApiError,
  type Channel,
  fixture,
  type Media,
  type Schedule,
  type Webhook,
  unique,
} from '../lib/clients.js'
import { waitForPost } from '../lib/flow.js'
import { RECEIVER_CONTAINER, received, resetReceiver } from '../lib/receiver.js'
import { docker, ROOT, waitForHttp } from '../lib/stack.js'
import { waitFor } from '../lib/wait.js'

const run = promisify(execFile)
const AVATAR = fixture('avatar1.png')
const id = Date.now().toString(36)
const SRC = `backup-e2e-src-${id}`
const DST = `backup-e2e-dst-${id}`
const NOKEY = `backup-e2e-nokey-${id}`
const HEADER_SECRET = `hdr-${id}`
const NTFY_SECRET = `ntfy-${id}`

/** Starts an install of the e2e image with its own volume, on the e2e network, the API on a free local port. */
async function install(name: string, network: string): Promise<Api> {
  await docker([
    'run',
    '-d',
    '--name',
    name,
    '--network',
    network,
    '-v',
    `${name}:/data`,
    '-p',
    '127.0.0.1::3333',
    '-e',
    'STAQLY_MODE=local',
    '-e',
    'NOTIFY_ALLOW_PRIVATE=true',
    'staqly-e2e:test',
  ])

  return new Api(await apiUrl(name), await token(name))
}

async function apiUrl(name: string): Promise<string> {
  const url = `http://${(await docker(['port', name, '3333/tcp'])).trim().split('\n')[0]}`

  await waitForHttp(`${url}/health`)

  return url
}

const token = async (name: string) => (await docker(['exec', name, 'server', 'token'])).trim()

/** Runs a script from scripts/ against one install; returns its stdout. */
async function script(file: string, args: string[], name: string): Promise<string> {
  const { stdout } = await run(join(ROOT, 'scripts', file), args, {
    env: { ...process.env, STAQLY_CONTAINER: name, STAQLY_VOLUME: name, FORCE: '1' },
  })

  return stdout
}

describe('backup.sh and restore.sh', () => {
  let src: Api
  let dst: Api
  let nokey: Api
  let network = ''
  let backupDir = ''
  let dbOnlyDir = ''
  let collectionId: string
  let posts: string[]
  let image: Media
  let channel: Channel
  let webhook: Webhook
  let alert: Alert
  let schedule: Schedule

  beforeAll(async () => {
    network = (
      await docker([
        'inspect',
        RECEIVER_CONTAINER,
        '--format',
        '{{range $k, $v := .NetworkSettings.Networks}}{{$k}}{{end}}',
      ])
    ).trim()

    ;[src, dst] = await Promise.all([install(SRC, network), install(DST, network)])
    await resetReceiver(0)

    const collection = await src.collection({ name: unique('Backed up') })

    collectionId = collection.id
    image = await src.upload(AVATAR, 'backed-up.png')
    channel = await src.post<Channel>('/v1/channels', {
      provider: 'custom',
      name: unique('Endpoint'),
      fields: {
        delivery: 'endpoint',
        url: 'http://receiver:9000/backup-e2e',
        headers: `X-Backup-Secret: ${HEADER_SECRET}`,
      },
    })
    webhook = await src.webhook({ url: 'http://receiver:9000/backup-e2e-hook', events: ['post.created'] })
    alert = await src.alert({
      name: unique('ntfy'),
      type: 'ntfy',
      config: { server_url: 'http://receiver:9000', topic: `backup${id}`, access_token: NTFY_SECRET },
    })
    schedule = await src.createSchedule({
      collection: collectionId,
      name: 'Kept',
      kind: 'recurring',
      rule: { every_minutes: 120 },
      channels: [channel.id],
    })
    const first = await src.createPost(collectionId, {
      content: 'Backed up',
      media: [image.id],
      targets: [{ channel_id: channel.id }],
    })
    const second = await src.createPost(collectionId, { content: 'Backed up too' })

    posts = [first.id, second.id].toSorted()
  })

  afterAll(async () => {
    await docker(['rm', '-f', SRC, DST, NOKEY]).catch(() => '')
    await docker(['volume', 'rm', '-f', SRC, DST, NOKEY]).catch(() => '')

    for (const dir of [backupDir, dbOnlyDir]) {
      if (dir) {
        rmSync(dir, { recursive: true, force: true })
      }
    }
  })

  it('backs up a stopped install and restores it into a fresh one', async () => {
    const srcToken = await token(SRC)

    // Like the docs: a full backup needs every writer stopped (the script refuses a running one).
    await expect(script('backup.sh', [], SRC)).rejects.toThrow(/Stop backup-e2e-src/)
    await docker(['stop', '-t', '30', SRC])
    const out = await script('backup.sh', [], SRC)

    backupDir = join(ROOT, out.match(/Backup written to (\S+)/)![1]!)
    // The fresh install made its own keys and token; the restore replaces them.
    expect(await token(DST)).not.toBe(srcToken)
    await script('restore.sh', [backupDir], DST)
    dst = new Api(await apiUrl(DST), srcToken)
    expect(await token(DST)).toBe(srcToken)
  })

  it('brings back posts, the collection, media bytes, the webhook and the schedule', async () => {
    const restored = await dst.listPosts({ collection_id: collectionId })

    expect(restored.map((p) => p.id).toSorted()).toEqual(posts)
    expect((await dst.raw(`/v1/media/${image.id}`)).body.equals(AVATAR)).toBe(true)
    expect(await dst.get<Webhook>(`/v1/webhooks/${webhook.id}`)).toMatchObject({ url: webhook.url, active: true })
    expect(await dst.get<Schedule>(`/v1/schedules/${schedule.id}`)).toMatchObject({
      enabled: true,
      next_run_at: schedule.next_run_at,
    })
    expect(await dst.get<Channel>(`/v1/channels/${channel.id}`)).toMatchObject({ status: 'connected' })
  })

  it('opens the channel credentials with the restored secret.key', async () => {
    const post = await dst.createPost(collectionId, {
      content: 'After the restore',
      targets: [{ channel_id: channel.id }],
    })

    await dst.publish(post.id)
    await waitForPost(dst, post.id, 'published')
    const [req] = await received('/backup-e2e')

    expect(req!.headers['x-backup-secret']).toBe(HEADER_SECRET)
    // The restored webhook announced the new post too.
    await waitFor(async () => (await received('/backup-e2e-hook')).find((r) => r.body.includes(post.id)))
  })

  it('opens the alert secret with the restored notify_secret.key', async () => {
    expect(await dst.post<{ ok: boolean }>(`/v1/alerts/${alert.id}/test`)).toMatchObject({ ok: true })
    const push = await waitFor(async () =>
      (await received('/')).find((r) => r.body.includes(`backup${id}`) && r.status === 200),
    )

    expect(push.headers.authorization).toBe(`Bearer ${NTFY_SECRET}`)
  })

  it('says what to do when restored without the keys', async () => {
    // The database alone: the fresh install keeps its own secret.key and notify_secret.key (and access token).
    dbOnlyDir = mkdtempSync(join(ROOT, 'backups', 'nokey-'))
    copyFileSync(join(backupDir, 'workspace.db'), join(dbOnlyDir, 'workspace.db'))
    nokey = await install(NOKEY, network)
    await script('restore.sh', [dbOnlyDir], NOKEY)
    nokey = new Api(await apiUrl(NOKEY), await token(NOKEY))

    const post = await nokey.createPost(collectionId, { content: 'No keys', targets: [{ channel_id: channel.id }] })

    await nokey.publish(post.id)
    const failed = await waitForPost(nokey, post.id, 'failed')

    expect(failed.targets[0]!.error).toMatch(/cannot be read.*reconnect the channel/)
    expect(await nokey.get<Channel>(`/v1/channels/${channel.id}`)).toMatchObject({ status: 'needs_reconnect' })
    await expect(nokey.post(`/v1/alerts/${alert.id}/test`)).rejects.toMatchObject<Partial<ApiError>>({
      status: 422,
      code: 'secrets_unreadable',
    })
  })
})
