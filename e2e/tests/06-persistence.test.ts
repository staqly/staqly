// Data lives in the staqly-data volume: posts, media, channels and schedules
// survive a restart, the re-creation of the containers, and a restore of a
// backup taken with `server backup` (the database plus secret.key, which
// unseals the channels' credentials: scripts/backup.sh and restore.sh).
import { mkdtempSync } from 'node:fs'
import { tmpdir } from 'node:os'
import { join } from 'node:path'

import { beforeAll, describe, expect, it } from 'vitest'

import {
  api,
  type ApiError,
  type Channel,
  fixture,
  type Media,
  type Post,
  type Schedule,
  unique,
} from '../lib/clients.js'
import { publishNow, waitForRequested } from '../lib/flow.js'
import { API, API_CONTAINER, compose, docker, server, waitForHttp, waitForStack } from '../lib/stack.js'

const AVATAR = fixture('avatar1.png')

async function allPostIds(): Promise<string[]> {
  const ids: string[] = []
  let cursor: string | undefined

  do {
    const page = await api().get<{ data: Post[]; next_cursor?: string }>(
      `/v1/posts?limit=100${cursor ? `&cursor=${encodeURIComponent(cursor)}` : ''}`,
    )

    ids.push(...page.data.map((p) => p.id))
    cursor = page.next_cursor
  } while (cursor)

  return ids.toSorted()
}

describe('persistence', () => {
  const agent = api()
  let before: string[]
  let image: Media
  let post: Post
  let channel: Channel
  let schedule: Schedule

  beforeAll(async () => {
    const collection = await agent.collection({ name: unique('Persistence') })

    image = await agent.upload(AVATAR, 'keep.png')
    channel = await agent.customChannel(unique('Kept channel'))
    post = await agent.createPost(collection.id, {
      content: 'Keep me',
      media: [image.id],
      targets: [{ channel_id: channel.id }],
    })
    schedule = await agent.createSchedule({
      collection: collection.id,
      name: 'Kept',
      kind: 'recurring',
      rule: { every_minutes: 120 },
      channels: [channel.id],
    })
    before = await allPostIds()
  })

  const stillThere = async () => {
    expect(await allPostIds()).toEqual(before)
    expect((await agent.raw(`/v1/media/${image.id}`)).body.equals(AVATAR)).toBe(true)
    expect((await agent.getPost(post.id)).targets[0]!.channel_id).toBe(channel.id)
    expect((await agent.get<Channel>(`/v1/channels/${channel.id}`)).status).toBe('connected')
    const s = await agent.get<Schedule>(`/v1/schedules/${schedule.id}`)

    expect(s).toMatchObject({ enabled: true, next_run_at: schedule.next_run_at })
    // The schedule's job is back in the queue too.
    expect(
      (await agent.jobs({ kind: 'run_schedule', status: 'queued' })).some((j) => j.payload.schedule_id === schedule.id),
    ).toBe(true)
  }

  it('keeps posts, media, channels and schedules across a restart', async () => {
    expect(before.length).toBeGreaterThan(0)
    await compose(['restart', 'staqly'])
    await waitForHttp(`${API}/health`)
    await stillThere()
  })

  it('keeps them when the containers are re-created', async () => {
    await compose(['up', '-d', '--wait', '--no-build', '--force-recreate', 'staqly'])
    await waitForStack()
    expect(before).toContain(post.id)
    await stillThere()
  })

  it('restores a backup: the database and secret.key go back, channels still work', async () => {
    // Like scripts/backup.sh: a consistent snapshot while the server runs, copied out with the key.
    const dir = mkdtempSync(join(tmpdir(), 'staqly-e2e-backup-'))

    await server('backup', '/data/backups/e2e.db')
    await docker(['cp', `${API_CONTAINER}:/data/backups/e2e.db`, join(dir, 'workspace.db')])
    await docker(['cp', `${API_CONTAINER}:/data/secret.key`, join(dir, 'secret.key')])
    await docker(['exec', API_CONTAINER, 'rm', '-f', '/data/backups/e2e.db'])
    // Written after the backup: gone after the restore.
    const later = await agent.createPost(post.collection_id, { content: 'After the backup' })
    const volume = (
      await docker([
        'inspect',
        API_CONTAINER,
        '--format',
        '{{range .Mounts}}{{if eq .Destination "/data"}}{{.Name}}{{end}}{{end}}',
      ])
    ).trim()

    expect(volume).not.toBe('')
    // Like scripts/restore.sh: swap the data while the server is stopped.
    await compose(['stop', 'staqly'])
    await docker([
      'run',
      '--rm',
      '-v',
      `${volume}:/data`,
      '-v',
      `${dir}:/backup:ro`,
      'alpine:3.24',
      'sh',
      '-ec',
      'rm -f /data/workspace.db /data/workspace.db-wal /data/workspace.db-shm /data/secret.key && cp /backup/workspace.db /data/workspace.db && cp /backup/secret.key /data/secret.key && chown -R 10001:10001 /data && chmod 600 /data/workspace.db /data/secret.key',
    ])
    await compose(['start', 'staqly'])
    await waitForHttp(`${API}/health`)
    await stillThere()
    await expect(agent.getPost(later.id)).rejects.toMatchObject<Partial<ApiError>>({ status: 404 })
    // The restored key unseals the channel: it answers a test and takes a post.
    expect((await agent.raw(`/v1/channels/${channel.id}/test`, { method: 'POST' })).status).toBe(200)
    const again = await agent.createPost(post.collection_id, {
      content: 'After the restore',
      targets: [{ channel_id: channel.id }],
    })

    await publishNow(agent, again.id)
    expect((await waitForRequested(agent, again.id)).targets[0]).toMatchObject({ status: 'publishing', attempts: 1 })
  })
})
