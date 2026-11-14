// Realistic, layout-hostile data for the responsive check: long names, content and URLs without
// spaces, unicode, many tags, posts with media, every status we can reach through the API, 30+
// collections, channels, schedules, webhooks with deliveries, alerts and (cloud) API keys.
// Everything goes through the public REST API, like an agent would.
import { readFileSync } from 'node:fs'
import { join } from 'node:path'

export interface SeedTarget {
  url: string
  key?: string
}

export interface SeedResult {
  collectionId: string
  postIds: Record<string, string>
  channelIds: string[]
  /** a preview link of the long post, with a long comment on it */
  previewToken: string
  /** a connect link with a long note */
  connectLinkToken: string
}

const LONG_WORD = 'Supercalifragilisticexpialidocious-ExtraordinarilyLongIdentifierWithoutAnySpaces-0123456789'
const LONG_URL = `https://example.com/a/very/deeply/nested/path/that/keeps/going/and/going/${'segment-'.repeat(12)}end?utm_source=staqly&utm_medium=responsive&utm_campaign=${'x'.repeat(60)}`

async function call<T>(t: SeedTarget, method: string, path: string, body?: unknown, form?: FormData): Promise<T> {
  const headers: Record<string, string> = { 'user-agent': 'staqly-ui-responsive-seed' }

  if (t.key) {
    headers.authorization = `Bearer ${t.key}`
  }

  if (body !== undefined) {
    headers['content-type'] = 'application/json'
  }

  const res = await fetch(t.url + path, {
    method,
    headers,
    body: form ?? (body === undefined ? undefined : JSON.stringify(body)),
  })
  const text = await res.text()

  if (!res.ok) {
    throw new Error(`${method} ${path}: ${res.status} ${text}`)
  }

  return (text ? JSON.parse(text) : {}) as T
}

interface Post {
  id: string
  status: string
  targets: { channel_id: string; status: string; attempts: number }[]
}

export interface SeedMedia {
  /** PNG files (any aspect ratio) */
  images: string[]
  /** a WebM or MP4 file, optional */
  video?: string
  /** a PDF file, optional */
  pdf?: string
}

/** `days` ahead at `hour`:00 local time, as RFC 3339. */
function at(days: number, hour: number): string {
  const d = new Date()

  d.setDate(d.getDate() + days)
  d.setHours(hour, 0, 0, 0)

  return d.toISOString()
}

export async function seed(t: SeedTarget, media: SeedMedia): Promise<SeedResult> {
  const post = <T>(path: string, body?: unknown) => call<T>(t, 'POST', path, body ?? {})
  const upload = async (file: string, mime: string) => {
    const form = new FormData()

    form.append('file', new Blob([new Uint8Array(readFileSync(file))], { type: mime }), file.split('/').pop())

    return (await call<{ id: string }>(t, 'POST', '/v1/media', undefined, form)).id
  }

  // channels on "your own automation", one with a very long name, one disabled
  const channelNames = [
    `Company blog with an unreasonably long channel name ${LONG_WORD}`.slice(0, 100),
    'Brand social',
    'Weekly newsletter — ünïcödé 日本語',
    'Old account',
  ]
  const channelIds: string[] = []

  for (const name of channelNames) {
    channelIds.push((await post<{ id: string }>('/v1/channels', { provider: 'custom', name })).id)
  }

  await call(t, 'PATCH', `/v1/channels/${channelIds[3]}`, { status: 'disabled' })

  // 34 collections: long names, unicode, emoji
  const names = [
    `Instagram Avatar posts awaiting the brand team ${LONG_WORD}`,
    'Résumés & Lebensläufe (Zürich) — ünïcödé',
    '日本語のコレクション名前はとても長いです',
    '🚀 Launch notes',
    'AI Research',
    'Automation Results',
  ]

  for (let i = names.length; i < 34; i++) {
    names.push(
      `Collection ${String(i + 1).padStart(2, '0')} ${i % 3 === 0 ? 'with a rather long descriptive name' : ''}`.trim(),
    )
  }

  const collections: { id: string; name: string }[] = []

  for (const [i, name] of names.entries()) {
    collections.push(
      await post<{ id: string; name: string }>('/v1/collections', {
        name: name.slice(0, 100),
        // the API caps a description at 180 characters
        description: (i === 0
          ? `A very long description that goes on and on to see how the header wraps on small phones. ${LONG_WORD} ${LONG_URL}`
          : `Description of ${name}`
        ).slice(0, 180),
        color: ['#e11d48', '#2563eb', '#16a34a', '#f59e0b', '#7c3aed'][i % 5],
      }),
    )
  }

  const main = collections[0]!
  const postIds: Record<string, string> = {}
  const tags = [
    'urgent',
    'needs-legal-review',
    'q4-campaign',
    'ünïcödé',
    'very-long-tag-name-that-is-forty-chars!',
    'social',
    'draft-v2',
    'brand',
    'en',
    'de',
    'jp',
    'a',
  ]
  const targets = (n = 2) => channelIds.slice(0, n).map((channel_id) => ({ channel_id }))
  const common = (i: number) => ({
    source: i % 2 ? 'research-agent-with-a-very-long-source-name' : 'claude-code',
    tags: tags.slice(0, (i % tags.length) + 1),
    metadata: {
      prompt: `photorealistic avatar, scene ${i}, ${LONG_WORD}`,
      model: 'mock-model-v1',
      seed: 1000 + i,
      url: LONG_URL,
      nested: { deep: { deeper: ['a', 'b', { c: LONG_WORD }] } },
    },
  })
  const create = async (key: string, body: Record<string, unknown>) => {
    const p = await post<Post>('/v1/posts', { collection: main.id, title: 'Post', ...body })

    postIds[key] = p.id

    return p
  }
  const requested = async (id: string) => {
    for (let i = 0; i < 60; i++) {
      const p = await call<Post>(t, 'GET', `/v1/posts/${id}`)

      if (p.targets.every((x) => x.status === 'publishing' && x.attempts > 0)) {
        return p
      }

      await new Promise((r) => setTimeout(r, 300))
    }

    throw new Error(`post ${id} was not handed to the automation`)
  }

  // posts with every kind of content
  const images = await Promise.all(media.images.map((img) => upload(img, 'image/png')))

  await create('image', {
    ...common(0),
    content: `Avatar post with an extremely long first line that will need to wrap ${LONG_WORD}\nGolden hour on the rooftop ☀️ #aiavatar #sunset #citylife ${LONG_URL}`,
    media: images,
    targets: targets(3),
  })

  if (media.video) {
    await create('video', {
      ...common(3),
      content: 'Product teaser video 🎬\nA short clip for the launch.',
      media: [await upload(media.video, 'video/webm')],
      targets: targets(1),
    })
  }

  await create('long', {
    ...common(5),
    content: `SQLite WAL mode in production — ünïcödé 日本語\n\nWAL allows concurrent readers with one writer.\n\n${'Lorem ipsum dolor sit amet, consectetur adipiscing elit. '.repeat(30)}\n\n${LONG_WORD}\n\n${LONG_URL}`,
    targets: targets(2),
  })
  await create('tags', { ...common(11), content: 'Every tag there is\nShort body.', tags, targets: targets(2) })
  await create('overrides', {
    ...common(7),
    content: 'Same text everywhere\nUnless a channel has its own.',
    targets: [
      { channel_id: channelIds[0]!, content: `A much longer text for the blog ${LONG_WORD} ${LONG_URL}` },
      { channel_id: channelIds[1]!, content: 'Short for social' },
      { channel_id: channelIds[2]! },
    ],
  })
  await create('draft', { ...common(8), content: 'A draft without channels\nStill being written.' })
  await create('scheduled', {
    ...common(9),
    content: `Scheduled for tomorrow ${LONG_WORD}\nAt ten.`,
    targets: targets(2),
    publish_at: at(1, 10),
    status: 'scheduled',
  })

  for (let i = 0; i < 12; i++) {
    await create(`scheduled${i}`, {
      ...common(i),
      content: `Scheduled post ${i} ${i % 4 === 0 ? LONG_WORD : ''}\nBody ${i}`,
      targets: targets((i % 3) + 1),
      publish_at: at((i % 6) + 1, 8 + (i % 10)),
      status: 'scheduled',
    })
  }

  const publishing = await create('publishing', {
    ...common(9),
    content: 'Publishing right now\nThe automation is at work.',
    targets: targets(2),
  })

  await post(`/v1/posts/${publishing.id}/publish`)
  await requested(publishing.id)
  const published = await create('published', {
    ...common(10),
    content: 'Published and done\nOut on every channel.',
    targets: targets(2),
  })

  await post(`/v1/posts/${published.id}/publish`)
  await requested(published.id)

  for (const ch of channelIds.slice(0, 2)) {
    await post(`/v1/posts/${published.id}/targets/${ch}/result`, { status: 'published', url: LONG_URL })
  }

  const failed = await create('failed', {
    ...common(11),
    content: 'Publish failed\nOne channel refused it.',
    targets: targets(2),
  })

  await post(`/v1/posts/${failed.id}/publish`)
  await requested(failed.id)
  await post(`/v1/posts/${failed.id}/targets/${channelIds[0]}/result`, { status: 'published', url: LONG_URL })
  await post(`/v1/posts/${failed.id}/targets/${channelIds[1]}/result`, {
    status: 'failed',
    error: `Upstream returned 502 Bad Gateway after 3 attempts: ${LONG_WORD} ${LONG_URL}`,
  })

  // a long list: more drafts, spread over collections
  for (let i = 0; i < 40; i++) {
    const c = collections[i % 8]!

    await post('/v1/posts', {
      collection: c.id,
      title: 'Post',
      ...common(i),
      content:
        i % 5 === 0
          ? `Draft ${i} ${LONG_WORD}\nA post about something.`
          : `Draft ${i}: a post about something\nBody of draft ${i}`,
      targets: targets((i % 3) + 1),
    })
  }

  // schedules: recurring with many times, once, every few hours, one asking an endpoint, one paused
  await post('/v1/schedules', {
    collection: main.id,
    name: `Weekday mornings with a long schedule name ${LONG_WORD}`.slice(0, 100),
    kind: 'recurring',
    rule: {
      times: ['06:00', '09:00', '12:30', '15:00', '17:30', '21:00'],
      weekdays: [1, 2, 3, 4, 5],
      tz: 'Europe/Warsaw',
    },
    channels: channelIds.slice(0, 3),
    pick: 'next',
    source: 'agent',
  })
  await post('/v1/schedules', {
    collection: main.id,
    name: 'Every four hours',
    kind: 'recurring',
    rule: { every_minutes: 240 },
    channels: channelIds.slice(0, 1),
    pick: 'all',
    source: 'endpoint',
    endpoint_url: `http://127.0.0.1:9/${LONG_WORD}`,
  })
  await post('/v1/schedules', {
    collection: main.id,
    name: 'Launch day',
    kind: 'once',
    run_at: at(3, 11),
    channels: channelIds.slice(1, 2),
  })
  await post('/v1/schedules', {
    collection: collections[1]!.id,
    name: 'Paused',
    kind: 'recurring',
    rule: { times: ['09:00'] },
    channels: channelIds.slice(0, 1),
    enabled: false,
  })

  // webhooks (deliveries fail: nothing listens) and alerts
  for (let i = 0; i < 8; i++) {
    await post('/v1/webhooks', {
      url: i === 0 ? `http://127.0.0.1:9/${'hook/'.repeat(20)}${LONG_WORD}` : `http://127.0.0.1:9/hook-${i}`,
      description: i === 0 ? `Webhook with a long description ${LONG_WORD}` : `Webhook ${i}`,
      events:
        i % 2 ? ['*'] : ['post.created', 'post.scheduled', 'post.publish_requested', 'post.published', 'post.failed'],
      collection_ids: i % 3 ? [] : collections.slice(0, 5).map((c) => c.id),
      max_attempts: 1,
      timeout_ms: 1000,
    })
  }

  const alerts: [string, Record<string, unknown>][] = [
    ['telegram', { bot_token: '123456:ABCDEFGHIJKLMNOPQRSTUVWXYZ012345', chat_id: '-1001234567890' }],
    ['slack', { webhook_url: 'https://hooks.slack.com/services/T000/B000/XXXXXXXXXXXXXXXX' }],
    ['discord', { webhook_url: 'https://discord.com/api/webhooks/123/abc' }],
    ['ntfy', { topic: `staqly-${LONG_WORD.slice(0, 40)}` }],
    ['email', { host: 'smtp.example.com', from: 'staqly@example.com', to: ['reviewers@example.com'] }],
  ]

  for (const [i, [type, config]] of alerts.entries()) {
    await post('/v1/alerts', {
      name: (i === 0 ? `Team chat with a very long alert name ${LONG_WORD}` : `${type} alert`).slice(0, 100),
      type,
      config,
      events: ['post.published', 'post.failed'],
      collection_ids: i % 2 ? [] : [main.id],
    })
  }

  // cloud: scoped API keys (a local install has none: its access token is the key)
  if (t.key && !t.key.startsWith('stk_local_')) {
    const scopes = [
      'posts:read',
      'posts:write',
      'posts:publish',
      'collections:read',
      'channels:read',
      'webhooks:manage',
    ]

    for (let i = 0; i < 5; i++) {
      await post('/v1/api-keys', {
        name: (i === 0 ? `Agent key with a very long name ${LONG_WORD}` : `Agent key ${i}`).slice(0, 100),
        scopes: scopes.slice(0, i + 1),
        // Keys last at most a year: half a year out.
        ...(i === 2 ? { expires_at: new Date(Date.now() + 182 * 864e5).toISOString().slice(0, 10) } : {}),
      })
    }
  }

  const preview = await post<{ token: string }>(`/v1/posts/${postIds.long}/previews`, { expires_in_days: 7 })

  await call(t, 'POST', `/v1/previews/${preview.token}/comments`, {
    name: `Reviewer with a long name ${LONG_WORD}`.slice(0, 80),
    body: `A long comment ${LONG_WORD} ${LONG_URL}`,
    quote: LONG_WORD,
  })

  const connect = await post<{ token: string }>('/v1/channel-links', {
    note: `For ${LONG_WORD}`.slice(0, 300),
    max_uses: 3,
  })

  return { collectionId: main.id, postIds, channelIds, previewToken: preview.token, connectLinkToken: connect.token }
}

export function fixture(name: string): string {
  return join(import.meta.dirname, '..', 'fixtures', name)
}
