#!/usr/bin/env node
// Seeds a large local-mode workspace through the public API, for the
// dashboard performance run (e2e/perf/README in e2e/README.md).
//
//   node e2e/perf/seed.mjs --api http://127.0.0.1:63334 [--collections 200 --posts 10000 --updates 7]
//
// Defaults: 200 collections, one channel on your own automation, 10k posts
// (50 per collection, drafts, scheduled across the coming weeks and some with a
// picture-less long text), every draft updated --updates times (about 50k events
// in all), 20 posts with a 100-version history, and one webhook to a closed
// port on the first 10 collections (about 1k failed deliveries). Needs a fresh
// local-mode server (no API key); safe to run only against a throwaway data dir.
import { parseArgs } from 'node:util'

const { values: opt } = parseArgs({
  options: {
    api: { type: 'string', default: 'http://127.0.0.1:63334' },
    collections: { type: 'string', default: '200' },
    posts: { type: 'string', default: '10000' },
    updates: { type: 'string', default: '7' },
    concurrency: { type: 'string', default: '16' },
  },
})

const api = opt.api.replace(/\/$/, '')
const nCollections = Number(opt.collections)
const nPosts = Number(opt.posts)
const nUpdates = Number(opt.updates)
const concurrency = Number(opt.concurrency)

async function call(method, path, body) {
  for (let attempt = 0; ; attempt++) {
    const init = { method, headers: { 'Content-Type': 'application/json', 'User-Agent': 'staqly-perf-seed' } }

    if (body !== undefined) {
      init.body = JSON.stringify(body)
    }

    const res = await fetch(api + path, init)

    if (res.status === 429 && attempt < 20) {
      await new Promise((r) => setTimeout(r, 200 * (attempt + 1)))
      continue
    }

    const text = await res.text()

    if (!res.ok) {
      throw new Error(`${method} ${path}: ${res.status} ${text.slice(0, 300)}`)
    }

    return text ? JSON.parse(text) : null
  }
}

async function pool(n, fn) {
  let next = 0
  let done = 0
  const started = Date.now()

  async function worker() {
    while (next < n) {
      const i = next++

      await fn(i)
      done++

      if (done % 2000 === 0) {
        console.log(`  ${done}/${n} (${((Date.now() - started) / 1000).toFixed(0)} s)`)
      }
    }
  }

  await Promise.all(Array.from({ length: concurrency }, worker))
}

const words =
  'agent draft review summary caption research launch pricing outline thread newsletter landing avatar campaign release notes'.split(
    ' ',
  )
const pick = (i, k) => words[(i * 7 + k * 13) % words.length]
const lorem = (i) =>
  Array.from({ length: 60 }, (_, k) => pick(i, k)).join(' ') + `\n\nParagraph two of post ${i}, with more text to read.`

const t0 = Date.now()
const run = t0.toString(36).slice(-4) // collection names are unique per workspace

console.log(`seeding ${api}: ${nCollections} collections, ${nPosts} posts`)

const collections = []

await pool(nCollections, async (i) => {
  const c = await call('POST', '/v1/collections', {
    name: `Perf ${String(i).padStart(3, '0')} ${pick(i, 1)} ${run}`,
    description: `Generated collection ${i} for the performance run`,
  })

  collections[i] = c.id
})

await call('POST', '/v1/webhooks', {
  url: 'http://127.0.0.1:9/perf-sink',
  description: 'perf: closed port, every delivery fails',
  events: ['*'],
  collection_ids: collections.slice(0, 10),
  max_attempts: 1,
  timeout_ms: 1000,
})

const channel = await call('POST', '/v1/channels', { provider: 'custom', name: `Perf channel ${run}` })
const posts = []
// 30 % scheduled across the coming three weeks (a full calendar), the rest are drafts.
const nScheduled = Math.floor(nPosts * 0.3)
const slot = (i) => new Date(Date.now() + 3600_000 * (1 + (i % (21 * 24)))).toISOString()

await pool(nPosts, async (i) => {
  const col = collections[i % nCollections]
  const scheduled = i < nScheduled
  const p = await call('POST', '/v1/posts', {
    collection: col,
    content: `${pick(i, 5)} ${pick(i, 6)} #${i}\n${lorem(i)}`,
    tags: [pick(i, 7), pick(i, 8)],
    metadata: { model: `model-${i % 4}`, prompt: `prompt ${i}`, seed: i },
    source: `agent-${i % 12}`,
    ...(scheduled && { targets: [{ channel_id: channel.id }], publish_at: slot(i), status: 'scheduled' }),
  })

  posts[i] = p.id
})
console.log(`posts done (${((Date.now() - t0) / 1000).toFixed(0)} s)`)

// Long histories: 20 drafts with 100 versions each.
const long = posts.slice(nPosts - 20)

await pool(long.length * 100, async (k) => {
  const id = long[k % long.length]

  await call('PATCH', `/v1/posts/${id}`, { content: `Long history ${k % long.length} v${Math.floor(k / long.length)}` })
})

// Bulk of the event log: every other draft updated nUpdates times.
const drafts = posts.slice(nScheduled, nPosts - 20)

await pool(drafts.length * nUpdates, async (k) => {
  const id = drafts[k % drafts.length]

  await call('PATCH', `/v1/posts/${id}`, { metadata: { revision: Math.floor(k / drafts.length) + 1 } })
})

const events = await call('GET', '/v1/events?limit=1')

console.log(`done in ${((Date.now() - t0) / 1000).toFixed(0)} s; latest event ${events?.data?.[0]?.id ?? '?'}`)
