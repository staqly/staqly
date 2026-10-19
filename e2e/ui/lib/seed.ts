// Realistic workspace data, created through the public REST API so events,
// webhooks and alerts fire as they would for real agents.
import type { Api } from './api.js'

export interface Seed {
  collections: Record<string, string>
  channels: Record<string, string>
  posts: Record<string, string>
  schedules: Record<string, string>
  webhooks: Record<string, string>
}

const sleep = (ms: number) => new Promise<void>((r) => setTimeout(r, ms))

/** `offset` days ahead at `hour`:00 local time, as RFC 3339. */
function at(offset: number, hour: number, minute = 0): string {
  const d = new Date()

  d.setDate(d.getDate() + offset)
  d.setHours(hour, minute, 0, 0)

  return d.toISOString()
}

const longPost = `Why WAL mode matters for small teams

WAL lets readers keep reading while one writer writes. Checkpoints move the log back into the database.

- Use VACUUM INTO for online backups
- Keep transactions short
- PRAGMA busy_timeout = 5000`

/** Seeds one workspace. `receiver` is the loopback HTTP server for webhooks and alerts. */
export async function seedWorkspace(api: Api, receiver: string, opts: { small?: boolean } = {}): Promise<Seed> {
  const c: Record<string, string> = {}
  const ch: Record<string, string> = {}
  const p: Record<string, string> = {}
  const s: Record<string, string> = {}
  const wh: Record<string, string> = {}

  // ── Channels: three on "your own automation" ───────────────────────────────
  ch.blog = (await api.channel('Company blog')).id
  ch.social = (await api.channel('Brand social')).id
  ch.newsletter = (await api.channel('Weekly newsletter')).id
  const disabled = await api.channel('Old account')

  await api.patch(`/v1/channels/${disabled.id}`, { status: 'disabled' })
  ch.disabled = disabled.id

  // ── Collections ────────────────────────────────────────────────────────────
  c.instagram = (
    await api.collection({
      name: 'Instagram posts',
      description: 'AI avatar posts for the brand account',
      color: '#ec4899',
    })
  ).id
  c.research = (
    await api.collection({ name: 'Research notes', description: 'Summaries and findings from research agents' })
  ).id
  c.automation = (
    await api.collection({ name: 'Automation results', description: 'Posts written by n8n workflows' })
  ).id
  c.newsletter = (await api.collection({ name: 'Newsletter drafts', description: 'Weekly newsletter sections' })).id

  if (!opts.small) {
    for (const name of [
      'Support replies',
      'Product photos',
      'Blog ideas',
      'Tweets',
      'Release notes',
      'Video clips',
      'Docs fixes',
      'Sales leads',
      'Podcast notes',
    ]) {
      c[name] = (await api.collection({ name, description: `${name} from agents` })).id
    }

    c.archived = (await api.collection({ name: 'Old campaigns', description: 'Archived' })).id
    await api.patch(`/v1/collections/${c.archived}`, { archived: true })
  }

  // ── Instagram: posts with pictures in every status ─────────────────────────
  const avatars = await Promise.all([0, 1, 2, 3].map((i) => api.upload(`avatar${i}.png`, `avatar${i}.png`)))
  const captions = [
    'Golden hour on the rooftop',
    'Neon nights in the city',
    'Slow morning, good coffee',
    'Sunset state of mind',
  ]
  const two = [{ channel_id: ch.blog }, { channel_id: ch.social, content: 'Short version for social' }]

  for (const [i, caption] of captions.entries()) {
    p[`avatar${i}`] = (
      await api.createPost(c.instagram, {
        content: `${caption}\nAvatar post #${i + 1} for the brand account.`,
        media: [avatars[i]!],
        targets: two,
        source: 'higgsfield-agent',
        tags: ['avatar', i % 2 ? 'night' : 'day'],
        metadata: { prompt: `photorealistic avatar, scene ${i}`, model: 'mock-higgsfield-v1', seed: 1000 + i },
      })
    ).id
  }

  // Scheduled in the coming days (the calendar's week view shows them).
  p.scheduledTomorrow = (
    await api.createPost(c.instagram, {
      content: 'Beach day\nTomorrow at ten.',
      media: [avatars[0]!],
      targets: two,
      publish_at: at(1, 10),
      status: 'scheduled',
      source: 'higgsfield-agent',
    })
  ).id
  p.scheduledLater = (
    await api.createPost(c.instagram, {
      content: 'Weekend teaser\nIn three days.',
      media: [avatars[1]!],
      targets: [{ channel_id: ch.social }],
      publish_at: at(3, 15),
      status: 'scheduled',
      source: 'higgsfield-agent',
    })
  ).id
  // Publishing right now: handed to the automation, no result yet.
  const publishing = await api.createPost(c.instagram, {
    content: 'Publishing now\nThe automation is at work.',
    media: [avatars[2]!],
    targets: two,
    source: 'higgsfield-agent',
  })

  await api.publish(publishing.id)
  await api.waitRequested(publishing.id)
  p.publishing = publishing.id
  // Published yesterday, with a link.
  const published = await api.createPost(c.instagram, {
    content: 'Posted yesterday\nAlready out.',
    media: [avatars[3]!],
    targets: two,
    source: 'higgsfield-agent',
  })

  await api.publishThrough(published.id, 'https://instagram.example/p/abc')
  p.published = published.id
  // Failed on one channel, published on the other.
  const failed = await api.createPost(c.instagram, {
    content: 'Upload failed\nOne channel refused it.',
    media: [avatars[0]!],
    targets: two,
    source: 'higgsfield-agent',
  })

  await api.publish(failed.id)
  await api.waitRequested(failed.id)
  await api.result(failed.id, ch.blog, { status: 'published', url: 'https://blog.example.com/upload' })
  await api.result(failed.id, ch.social, { status: 'failed', error: 'Instagram API: 503 Service Unavailable' })
  p.failed = failed.id
  p.draft = (
    await api.createPost(c.instagram, {
      content: 'Draft carousel\nStill being written.',
      media: [avatars[1]!],
      source: 'higgsfield-agent',
    })
  ).id

  // ── Research: text posts, one long, tags ──────────────────────────────────
  p.text = (
    await api.createPost(c.research, {
      content: longPost,
      targets: [{ channel_id: ch.blog }],
      source: 'research-bot',
      tags: ['sqlite', 'databases'],
      metadata: { agent: 'research-bot', confidence: 0.9 },
    })
  ).id
  p.link = (
    await api.createPost(c.research, {
      content: 'The Twelve-Factor App\nhttps://12factor.net/ A methodology for building SaaS apps',
      targets: [{ channel_id: ch.newsletter }],
      source: 'research-bot',
      tags: ['architecture'],
    })
  ).id
  const clip = await api.upload('clip.mp4', 'teaser.mp4')

  p.video = (
    await api.createPost(c.research, {
      content: 'Product teaser\nA short clip.',
      media: [clip],
      targets: [{ channel_id: ch.social }],
      source: 'video-agent',
    })
  ).id
  p.scheduledResearch = (
    await api.createPost(c.research, {
      content: 'Weekly digest\nGoes out in two days.',
      targets: [{ channel_id: ch.newsletter }],
      publish_at: at(2, 9, 30),
      status: 'scheduled',
      source: 'research-bot',
    })
  ).id

  // ── Automation results: posts from n8n ─────────────────────────────────────
  for (let i = 1; i <= (opts.small ? 3 : 8); i++) {
    const post = await api.createPost(c.automation, {
      content: `Workflow run ${i}\nLead enrichment found ${i * 3} leads.`,
      source: 'n8n',
      tags: ['n8n'],
      targets: [{ channel_id: ch.blog }],
    })

    p[`run${i}`] = post.id

    if (i % 3 === 0) {
      await api.publishThrough(post.id, `https://blog.example.com/run-${i}`)
    }
  }

  // ── Newsletter: an edited post with history ────────────────────────────────
  const news = await api.createPost(c.newsletter, {
    content: 'Week 42: what shipped\nFirst draft of the intro.',
    targets: [{ channel_id: ch.newsletter }],
    source: 'writer-agent',
  })

  await api.patch(`/v1/posts/${news.id}`, {
    content: 'Week 42: what shipped\nSecond draft: activity export, OpenCode 2 configs and a faster calendar.',
  })
  p.versioned = news.id

  // ── Schedules ──────────────────────────────────────────────────────────────
  s.mornings = (
    await api.post<{ id: string }>('/v1/schedules', {
      collection: c.instagram,
      name: 'Weekday mornings',
      kind: 'recurring',
      rule: { times: ['09:00', '17:30'], weekdays: [1, 2, 3, 4, 5], tz: 'UTC' },
      channels: [ch.blog, ch.social],
      pick: 'next',
    })
  ).id
  s.digest = (
    await api.post<{ id: string }>('/v1/schedules', {
      collection: c.research,
      name: 'Ask the agent on Fridays',
      kind: 'recurring',
      rule: { times: ['12:00'], weekdays: [5], tz: 'UTC' },
      channels: [ch.newsletter],
      pick: 'all',
      source: 'agent',
    })
  ).id
  s.once = (
    await api.post<{ id: string }>('/v1/schedules', {
      collection: c.automation,
      name: 'Launch day',
      kind: 'once',
      run_at: at(4, 11),
      channels: [ch.blog],
      pick: 'all',
    })
  ).id
  s.paused = (
    await api.post<{ id: string }>('/v1/schedules', {
      collection: c.newsletter,
      name: 'Paused one',
      kind: 'recurring',
      rule: { every_minutes: 360 },
      channels: [ch.newsletter],
      enabled: false,
    })
  ).id

  // ── Webhooks: succeeded, failed, pending, paused ───────────────────────────
  wh.ok = (
    await api.post<{ id: string }>('/v1/webhooks', {
      url: `${receiver}/ok`,
      description: 'Publisher',
      events: ['post.publish_requested', 'post.published'],
    })
  ).id
  wh.fail = (
    await api.post<{ id: string }>('/v1/webhooks', {
      url: `${receiver}/fail`,
      description: 'Broken receiver',
      events: ['post.*'],
      max_attempts: 1,
    })
  ).id
  wh.pending = (
    await api.post<{ id: string }>('/v1/webhooks', {
      url: `${receiver}/slow`,
      description: 'Slow receiver',
      events: ['post.created'],
      timeout_ms: 1000,
      max_attempts: 20,
    })
  ).id
  wh.paused = (
    await api.post<{ id: string }>('/v1/webhooks', {
      url: `${receiver}/ok?paused`,
      description: 'Paused hook',
      events: ['*'],
      active: false,
    })
  ).id
  // Deliveries for every hook state: one publication after the hooks exist.
  const trigger = await api.createPost(c.instagram, {
    content: 'Webhook trigger\nFires the hooks.',
    targets: [{ channel_id: ch.blog }],
    source: 'higgsfield-agent',
  })

  await api.publishThrough(trigger.id, 'https://blog.example.com/trigger')
  p.trigger = trigger.id

  // ── Alerts ────────────────────────────────────────────────────────────────
  await api.post('/v1/alerts', {
    name: 'Phone (ntfy)',
    type: 'ntfy',
    config: { server_url: receiver, topic: 'staqly-ui' },
    events: ['post.published', 'post.failed'],
  })
  await api.post('/v1/alerts', {
    name: 'Team email',
    type: 'email',
    config: { host: '127.0.0.1', port: 9, from: 'staqly@example.com', to: ['team@example.com'], tls: 'none' },
    events: ['post.failed', 'channel.needs_reconnect'],
    digest_window_seconds: 300,
  })

  // Let the webhook worker try every hook once.
  await sleep(1500)

  return { collections: c, channels: ch, posts: p, schedules: s, webhooks: wh }
}
