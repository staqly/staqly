// The workspace export ZIP (local mode, all features): its files, that the
// data in it matches the API, media bytes intact, secrets redacted, one export
// at a time, and unknown export IDs.
import { createHash } from 'node:crypto'

import { beforeAll, describe, expect, it } from 'vitest'

import { api, fixture, type Post, unique } from '../lib/clients.js'
import { publishThrough } from '../lib/flow.js'
import { http } from '../lib/http.js'
import { API, RECEIVER_INTERNAL } from '../lib/stack.js'
import { waitFor } from '../lib/wait.js'
import { ndjson, unzip } from '../lib/zip.js'

const agent = api()
const png = fixture('avatar1.png')
const sha = (b: Buffer) => createHash('sha256').update(b).digest('hex')

describe('workspace export', () => {
  let files: Map<string, Buffer>
  let collectionId: string
  let channelId: string
  let scheduleId: string
  let ids: { draft: string; image: string; published: string }
  let mediaId: string
  let webhookSecret: string
  let exportId: string

  beforeAll(async () => {
    const collection = await agent.collection({ name: unique('Export') })

    collectionId = collection.id
    channelId = (await agent.customChannel(unique('Export channel'))).id
    const media = await agent.upload(png, 'avatar ünï.png')

    mediaId = media.id
    const draft = await agent.createPost(collection.id, { content: 'Exported draft ✓', tags: ['e2e'] })
    const image = await agent.createPost(collection.id, { content: 'Exported image', media: [media.id] })
    const published = await agent.createPost(collection.id, {
      content: 'Exported publication',
      targets: [{ channel_id: channelId }],
    })

    await publishThrough(agent, published.id, 'https://example.com/exported')
    ids = { draft: draft.id, image: image.id, published: published.id }
    scheduleId = (
      await agent.createSchedule({
        collection: collection.id,
        name: 'Exported schedule',
        kind: 'recurring',
        rule: { every_minutes: 60 },
        channels: [channelId],
        source: 'endpoint',
        endpoint_url: `${RECEIVER_INTERNAL}/posts`,
        endpoint_secret: 'export_endpoint_secret_value',
      })
    ).id
    webhookSecret = (await agent.webhook({ url: `${RECEIVER_INTERNAL}/export`, events: ['post.purged'] })).secret
  })

  it('allows one export at a time', async () => {
    const [a, b] = await Promise.all([1, 2].map(() => http(`${API}/v1/workspace/export`, { method: 'POST' })))
    const statuses = [a!.status, b!.status].toSorted()

    expect(statuses[0]).toBe(202)

    // The second one either finds the first still running (409 with its ID) or starts after it finished.
    if (statuses[1] === 409) {
      const conflict = [a!, b!].find((r) => r.status === 409)!

      expect(conflict.json<{ error: { details: { export_id: string } } }>().error.details.export_id).toMatch(/^exp_/)
    } else {
      expect(statuses[1]).toBe(202)
    }

    exportId = [a!, b!].find((r) => r.status === 202)!.json<{ id: string }>().id
  })

  it('builds a ZIP with every part of the workspace', async () => {
    const job = await waitFor(
      async () => {
        const j = await agent.get<{ status: string; includes_media: boolean }>(`/v1/workspace/exports/${exportId}`)

        return j.status === 'ready' || j.status === 'failed' ? j : undefined
      },
      { message: 'export not ready', timeout: 90_000 },
    )

    expect(job).toMatchObject({ status: 'ready', includes_media: true })
    const res = await http(`${API}/v1/workspace/exports/${exportId}/download`)

    expect(res.status).toBe(200)
    expect(res.headers['content-type']).toBe('application/zip')
    expect(res.headers['content-disposition']).toMatch(/^attachment; filename="?staqly-export-\d{8}-\d{6}\.zip"?$/)
    files = unzip(res.body)

    for (const name of [
      'workspace.json',
      'README.txt',
      'collections.ndjson',
      'posts.ndjson',
      'events.ndjson',
      'media.ndjson',
      'webhooks.json',
      'channels.json',
      'schedules.json',
    ]) {
      expect(files.has(name)).toBe(true)
    }

    const meta = JSON.parse(files.get('workspace.json')!.toString('utf8')) as {
      format: string
      includes_media: boolean
      counts: Record<string, number>
    }

    expect(meta).toMatchObject({ format: 'staqly-export', includes_media: true })
    expect(meta.counts.posts).toBe(ndjson(files, 'posts.ndjson').length)
    expect(meta.counts.channels).toBeGreaterThanOrEqual(1)
    // No path tricks in entry names.
    expect([...files.keys()].filter((n) => n.startsWith('/') || n.includes('..') || n.includes('\\'))).toEqual([])
  })

  it('the posts match the API, with their targets, events and media', async () => {
    const posts = ndjson<Post & { media_files: string[] }>(files, 'posts.ndjson')
    const mine = new Map(posts.filter((p) => p.collection_id === collectionId).map((p) => [p.id, p]))

    expect(mine.get(ids.draft)).toMatchObject({ status: 'draft', content: 'Exported draft ✓', tags: ['e2e'] })
    expect(mine.get(ids.published)).toMatchObject({ status: 'published' })
    expect(mine.get(ids.published)!.targets[0]).toMatchObject({
      channel_id: channelId,
      status: 'published',
      external_url: 'https://example.com/exported',
    })
    expect(mine.get(ids.image)!.media_files[0]).toMatch(/^media\/avatar/)
    expect(ndjson<{ id: string }>(files, 'collections.ndjson').map((c) => c.id)).toContain(collectionId)
    const events = ndjson<{ type: string; post_id: string }>(files, 'events.ndjson')

    expect(events.some((e) => e.type === 'post.published' && e.post_id === ids.published)).toBe(true)
    const media = ndjson<{ id: string; sha256: string; file?: string }>(files, 'media.ndjson').find(
      (m) => m.id === mediaId,
    )

    expect(media?.sha256).toBe(sha(png))
    expect(media?.file).toMatch(/^media\/avatar/)
    expect(sha(files.get(media!.file!)!)).toBe(sha(png))
    const channels = JSON.parse(files.get('channels.json')!.toString('utf8')) as { id: string; provider: string }[]

    expect(channels.find((c) => c.id === channelId)?.provider).toBe('custom')
    const schedules = JSON.parse(files.get('schedules.json')!.toString('utf8')) as {
      id: string
      endpoint_url: string
    }[]

    expect(schedules.find((s) => s.id === scheduleId)?.endpoint_url).toBe(`${RECEIVER_INTERNAL}/posts`)
  })

  it('carries no secrets', async () => {
    const hooks = JSON.parse(files.get('webhooks.json')!.toString('utf8')) as { url: string; secret: string }[]

    expect(hooks.find((h) => h.url.endsWith('/export'))?.secret).toBe('[redacted]')
    expect(JSON.stringify(JSON.parse(files.get('channels.json')!.toString('utf8')))).not.toMatch(/credential/)

    for (const data of files.values()) {
      expect(data.includes(Buffer.from(webhookSecret))).toBe(false)
      expect(data.includes(Buffer.from('export_endpoint_secret_value'))).toBe(false)
    }
  })

  it('unknown export IDs are 404', async () => {
    expect((await http(`${API}/v1/workspace/exports/exp_nope`)).status).toBe(404)
    expect((await http(`${API}/v1/workspace/exports/exp_nope/download`)).status).toBe(404)
    expect((await http(`${API}/v1/workspace/exports/..%2F..%2Fworkspace.db/download`)).status).toBe(404)
  })
})
