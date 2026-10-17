// Real-agent MCP suite: each scenario runs `claude -p` against the live staqly
// MCP server of the dev stack (`make dev`: local mode, mailpit) and then checks
// the resulting state over REST. Costs about $2.5 a run. See e2e/README.md.
import { spawn } from 'node:child_process'
import { mkdirSync, writeFileSync } from 'node:fs'
import { createServer } from 'node:http'
import { tmpdir } from 'node:os'

const API = 'http://localhost:3333'
const RUN = 'T' + Date.now().toString(36).slice(-5)
const DIR = new URL('.', import.meta.url).pathname
const OUT = `${tmpdir()}/staqly-agent-e2e/${RUN}/`
const ONLY = process.argv.slice(2)

mkdirSync(OUT, { recursive: true })

// --- webhook receiver + RSS feed on the host (container reaches it via host.docker.internal)
const hooks = []
let extraPost = false
const recv = createServer((req, res) => {
  let b = ''

  req.on('data', (c) => (b += c))
  req.on('end', () => {
    if (req.url.startsWith('/feed.xml')) {
      res.setHeader('content-type', 'application/rss+xml')
      res.end(`<?xml version="1.0"?><rss version="2.0"><channel><title>Feed ${RUN}</title><link>http://x</link><description>d</description>
<item><title>${RUN} feed post one</title><link>http://example.com/${RUN}/1</link><guid>${RUN}-1</guid><description>first</description></item>
<item><title>${RUN} feed post two</title><link>http://example.com/${RUN}/2</link><guid>${RUN}-2</guid><description>second</description></item>
${extraPost ? `<item><title>${RUN} feed post three</title><link>http://example.com/${RUN}/3</link><guid>${RUN}-3</guid><description>third</description></item>` : ''}
</channel></rss>`)

      return
    }

    let body = null

    try {
      body = JSON.parse(b)
    } catch {}

    hooks.push({ path: req.url, headers: req.headers, body })
    res.end('ok')
  })
})

await new Promise((r) => recv.listen(8799, '0.0.0.0', r))
const HOST = 'http://host.docker.internal:8799'

// --- helpers
async function api(method, path, body) {
  const init = { method }

  if (body) {
    init.headers = { 'content-type': 'application/json' }
    init.body = JSON.stringify(body)
  }

  const r = await fetch(API + path, init)
  const t = await r.text()
  let j = null

  try {
    j = JSON.parse(t)
  } catch {}

  return { status: r.status, body: j ?? t }
}

const get = async (p) => (await api('GET', p)).body
const hooksFor = (run) => hooks.filter((h) => h.path === `/hook/${run}`)
const sleep = (ms) => new Promise((r) => setTimeout(r, ms))

async function until(fn, ms = 20000) {
  const end = Date.now() + ms
  let v

  while (Date.now() < end) {
    if ((v = await fn())) {
      return v
    }

    await sleep(500)
  }

  return v
}

const col = async (name) => ((await get('/v1/collections?limit=200')).data ?? []).find((c) => c.name === name)
const items = async (cid) => [
  ...((await get(`/v1/items?collection_id=${cid}&limit=100`)).data ?? []),
  ...((await get(`/v1/items?collection_id=${cid}&limit=100&status=archived`)).data ?? []),
]
const events = async (q) => (await get('/v1/events?limit=200&' + q)).data ?? []

function claude(name, prompt, maxTurns = 25) {
  return new Promise((resolve) => {
    const p = spawn(
      'claude',
      [
        '-p',
        prompt,
        '--mcp-config',
        DIR + 'mcp.json',
        '--strict-mcp-config',
        '--allowedTools',
        'mcp__staqly__*',
        '--output-format',
        'stream-json',
        '--verbose',
        '--max-turns',
        String(maxTurns),
        '--model',
        'sonnet',
      ],
      { cwd: DIR, stdio: ['ignore', 'pipe', 'pipe'] },
    )
    let out = ''

    p.stdout.on('data', (d) => (out += d))
    p.stderr.on('data', (d) => (out += d))
    p.on('close', () => {
      writeFileSync(`${OUT}${name}.jsonl`, out)
      const tools = [],
        errors = []
      let final = '',
        cost = 0

      for (const line of out.split('\n')) {
        let m

        try {
          m = JSON.parse(line)
        } catch {
          continue
        }

        for (const c of m.message?.content ?? []) {
          if (c.type === 'tool_use') {
            tools.push(c.name.replace('mcp__staqly__', ''))
          }

          if (c.type === 'tool_result' && c.is_error) {
            errors.push(JSON.stringify(c.content).slice(0, 300))
          }
        }

        if (m.type === 'result') {
          final = m.result ?? ''
          cost = m.total_cost_usd ?? 0
        }
      }

      resolve({ tools, errors, final, cost })
    })
  })
}

function check(fails, cond, msg) {
  if (!cond) {
    fails.push(msg)
  }
}

// --- scenarios: { name, prompt, verify(run, fails), before?, during? }
const N = (s) => `${RUN} ${s}`
const S = []

S.push({
  name: 'collection-flow',
  prompt: `Create a staqly collection named "${N('Blog')}" for blog drafts. Its flow: regenerate allowed from rejected only, publish allowed from approved and it must start automatically after approval, and retry enabled. Then read it back and tell me its actions.`,
  async verify(r, f) {
    const c = await col(N('Blog'))

    check(f, c, 'collection missing')
    const a = Object.fromEntries((c?.actions ?? []).map((x) => [x.name, x]))

    check(
      f,
      a.regenerate && JSON.stringify(a.regenerate.allowed_statuses) === '["rejected"]',
      'regenerate statuses ' + JSON.stringify(a.regenerate),
    )
    check(f, a.publish?.auto_run === true, 'publish auto_run')
    check(f, a.retry, 'retry missing')
    check(f, a.archive, 'archive missing (always on)')
  },
})

S.push({
  name: 'invalid-custom-action',
  prompt: `Create a staqly collection "${N('Images')}" and give it a custom action named "upscale" with the label "Upscale 4x". If staqly refuses, do NOT work around it; just tell me exactly what the error said.`,
  async verify(r, f) {
    const c = await col(N('Images'))

    check(f, !c || !(c.actions ?? []).some((x) => x.name === 'upscale'), 'custom action stored')
    check(
      f,
      r.errors.length > 0 || /catalog|unknown|not allowed|invalid|only/i.test(r.final),
      'no refusal surfaced: ' + r.final.slice(0, 200),
    )
  },
})

S.push({
  name: 'items-types',
  prompt: `In staqly create a collection "${N('Mixed')}" and add: (1) a text item titled "Hello post" with content "Hello world", (2) a link item for https://example.com/article titled "Ref link", (3) an image item titled "No media" with NO url and no media (expect this to fail, report the error), (4) an image item titled "Cdn image" with url https://picsum.photos/200. Tag the first item with "greeting" and "draft". Report which succeeded.`,
  async verify(r, f) {
    const c = await col(N('Mixed'))
    const it = c ? await items(c.id) : []
    const by = Object.fromEntries(it.map((x) => [x.title, x]))

    check(f, by['Hello post']?.type === 'text', 'text item')
    check(f, by['Ref link']?.type === 'link', 'link item')
    check(f, !by['No media'], 'image without media was created')
    check(f, by['Cdn image']?.type === 'image', 'image with url')
    check(f, (by['Hello post']?.tags ?? []).includes('greeting'), 'tags')
  },
})

S.push({
  name: 'review-approve-reject',
  async before() {
    const c = (await api('POST', '/v1/collections', { name: N('Review') })).body

    for (const t of ['Good caption', 'Bad caption', 'Typo caption']) {
      await api('POST', `/v1/collections/${c.id}/items`, { title: t, content: t + ' text' })
    }
  },
  prompt: `You are the reviewer in staqly. In the collection "${N('Review')}", look at the pending approvals: approve "Good caption", reject "Bad caption" with the feedback "Too generic, mention the product", and approve "Typo caption" but edit its title to "Fixed caption" while approving.`,
  async verify(r, f) {
    const it = await items((await col(N('Review'))).id)
    const by = Object.fromEntries(it.map((x) => [x.title, x]))

    check(f, by['Good caption']?.status === 'approved', 'good approved')
    check(f, by['Bad caption']?.status === 'rejected', 'bad rejected')
    check(
      f,
      by['Fixed caption']?.status === 'approved',
      'typo edited+approved: ' + it.map((x) => x.title + ':' + x.status).join(','),
    )
    const ev = await events(`type=item.rejected&item_id=${by['Bad caption']?.id}`)

    check(f, /generic/i.test(ev[0]?.data?.feedback ?? ''), 'reject feedback ' + JSON.stringify(ev[0]?.data))
  },
})

S.push({
  name: 'autorun-publish-report',
  async before() {
    const c = (
      await api('POST', '/v1/collections', { name: N('Auto'), actions: [{ name: 'publish', auto_run: true }] })
    ).body

    await api('POST', `/v1/collections/${c.id}/items`, { title: 'Ship me', content: 'post body' })
  },
  prompt: `In staqly, approve the item "Ship me" in the collection "${N('Auto')}". Then check its status. You are also the publisher: find the requested work, pretend you published it to https://blog.example.com/ship-me and report the result as completed with that url.`,
  async verify(r, f) {
    const it = (await items((await col(N('Auto'))).id))[0]

    check(f, it?.status === 'completed', 'status ' + it?.status)
    const ev = await events(`item_id=${it?.id}`)
    const pub = ev.find((e) => e.type === 'item.publish_requested')

    check(f, pub?.data?.auto_run === true, 'publish_requested auto_run')
    check(
      f,
      ev.some((e) => e.type === 'item.completed'),
      'completed event',
    )
    check(f, JSON.stringify(ev).includes('blog.example.com/ship-me'), 'result url')
  },
})

S.push({
  name: 'regenerate-fail-retry',
  async before() {
    const c = (
      await api('POST', '/v1/collections', { name: N('Regen'), actions: [{ name: 'regenerate' }, { name: 'retry' }] })
    ).body
    const i = (await api('POST', `/v1/collections/${c.id}/items`, { title: 'Draft A', content: 'meh' })).body

    await api('POST', `/v1/items/${i.id}/actions/reject`, { feedback: 'boring' })
  },
  prompt: `In staqly collection "${N('Regen')}" the item "Draft A" was rejected. Run the regenerate action on it with feedback "Make it punchier". Then, acting as the generator, report the result as FAILED with error "model timeout". Then run retry on it. Tell me the final status.`,
  async verify(r, f) {
    const it = (await items((await col(N('Regen'))).id))[0]

    check(f, it?.status === 'processing', 'final status ' + it?.status)
    const types = (await events(`item_id=${it?.id}`)).map((e) => e.type)

    for (const t of ['item.regenerate_requested', 'item.failed', 'item.retry_requested']) {
      check(f, types.includes(t), 'missing ' + t)
    }
  },
})

S.push({
  name: 'action-not-allowed',
  async before() {
    const c = (await api('POST', '/v1/collections', { name: N('Strict') })).body

    await api('POST', `/v1/collections/${c.id}/items`, { title: 'Pending one', content: 'x' })
  },
  prompt: `In staqly collection "${N('Strict')}", run the "publish" action on the pending item "Pending one" right now without approving it. If staqly refuses, don't approve it; just tell me which actions are available for it.`,
  async verify(r, f) {
    const it = (await items((await col(N('Strict'))).id))[0]

    check(f, it?.status === 'pending', 'status changed to ' + it?.status)
  },
})

S.push({
  name: 'stale-version',
  async before() {
    const c = (await api('POST', '/v1/collections', { name: N('Version') })).body
    const i = (await api('POST', `/v1/collections/${c.id}/items`, { title: 'v1 title', content: 'x' })).body

    await api('PATCH', `/v1/items/${i.id}`, { title: 'v2 title' })
  },
  prompt: `In staqly, approve the item in collection "${N('Version')}" but pass expected_version 1 (I reviewed version 1). If it conflicts, do NOT approve it anyway; tell me what happened.`,
  async verify(r, f) {
    const it = (await items((await col(N('Version'))).id))[0]

    check(f, it?.status === 'pending', 'approved despite stale version: ' + it?.status)
  },
})

S.push({
  name: 'archive-unarchive-history',
  async before() {
    const c = (await api('POST', '/v1/collections', { name: N('Arch') })).body

    await api('POST', `/v1/collections/${c.id}/items`, { title: 'Old note', content: 'first' })
  },
  prompt: `In staqly collection "${N('Arch')}": update the content of "Old note" to "second version", then archive it, then unarchive it. Finally show me its version history and event history (how many versions, which events).`,
  async verify(r, f) {
    const it = (await items((await col(N('Arch'))).id))[0]

    check(f, it && it.status !== 'archived', 'still archived')
    check(f, it?.version >= 2, 'version ' + it?.version)
    const types = (await events(`item_id=${it?.id}`)).map((e) => e.type)

    check(f, types.includes('item.archived') && types.includes('item.unarchived'), 'events ' + types)
    check(
      f,
      r.tools.some((t) => /versions|history/.test(t)),
      'no history tool used',
    )
  },
})

S.push({
  name: 'search-tags-bulk',
  async before() {
    const c = (await api('POST', '/v1/collections', { name: N('Bulk') })).body

    for (let i = 1; i <= 4; i++) {
      await api('POST', `/v1/collections/${c.id}/items`, {
        title: `Bulk tweet ${i}`,
        content: i % 2 ? 'about cats' : 'about dogs',
        tags: [i % 2 ? 'cats' : 'dogs'],
      })
    }
  },
  prompt: `In staqly, list the tags used in collection "${N('Bulk')}", search that collection for items about cats, and approve all the cat items in ONE bulk action. Leave dog items pending.`,
  async verify(r, f) {
    const it = await items((await col(N('Bulk'))).id)

    for (const x of it) {
      const want = x.tags.includes('cats') ? 'approved' : 'pending'

      check(f, x.status === want, `${x.title} ${x.status}`)
    }

    check(f, r.tools.includes('execute_bulk_action'), 'bulk tool not used: ' + r.tools)
  },
})

S.push({
  name: 'webhook-star',
  prompt: `In staqly create a webhook to ${HOST}/hook/${RUN} subscribed to all events ("*"). Send a test to it. Then create a device link (connect a device). Then create a collection "${N('Hooked')}" with one text item "Hook me". Finally list the webhook's deliveries and tell me their statuses.`,
  async verify(r, f) {
    await until(() => hooksFor(RUN).some((h) => h.body?.type === 'item.created'))
    const types = hooksFor(RUN).map((h) => h.body?.type)

    check(f, types.includes('webhook.test'), 'no test delivery: ' + types)
    check(
      f,
      types.includes('item.created') && types.includes('collection.created'),
      'no item/collection events: ' + types,
    )
    check(
      f,
      !types.some((t) => /^(device|device_link|session|api_key)\./.test(t)),
      'audit event leaked to "*": ' + types,
    )
    check(
      f,
      hooksFor(RUN).every((h) => h.headers['x-staqly-signature']),
      'missing signature header',
    )
  },
})

S.push({
  name: 'webhook-bad-pattern',
  prompt: `Create a staqly webhook to ${HOST}/hook/bad-${RUN} for the event "item.aproved" exactly as I typed it. If staqly rejects it, do not fix the spelling; just tell me the error.`,
  async verify(r, f) {
    const wh = (await get('/v1/webhooks')).data ?? []

    check(f, !wh.some((w) => w.url.includes('bad-' + RUN)), 'typo webhook was created')
  },
})

S.push({
  name: 'watcher-feed',
  prompt: `In staqly create a collection "${N('Feed')}" and a watcher on the RSS feed ${HOST}/feed.xml that puts new posts into it. Run the watcher now and tell me what items it created.`,
  async verify(r, f) {
    // The first run is a baseline (existing posts are only marked seen); a new post becomes an item.
    check(f, r.tools.includes('create_watcher') && r.tools.includes('run_watcher'), 'tools ' + r.tools)
    const c = await col(N('Feed'))
    const w = ((await get('/v1/watchers')).data ?? []).find(
      (x) => x.url?.includes('feed.xml') && (x.collection_ids ?? [x.collection_id]).includes(c?.id),
    )

    check(f, w, 'watcher not found')
    extraPost = true

    if (w) {
      await api('POST', `/v1/watchers/${w.id}/run`)
    }

    const it = c ? await until(async () => ((await items(c.id)).length ? items(c.id) : null)) : null

    check(f, (it ?? []).length === 1 && /three/.test(it[0].title), 'new-post items: ' + (it ?? []).map((x) => x.title))
  },
})

S.push({
  name: 'email-channel-decide',
  prompt: `In staqly create a collection "${N('Mailed')}". Then create an email notification channel named "${N('Mail')}" using SMTP host "mailpit", port 1025, TLS "none", from "staqly@example.com", to "reviewer-${RUN.toLowerCase()}@example.com", for new pending items in that collection only, sending each notification right away (no digest). Send a test notification. Then add a text item "Approve by mail" to the collection.`,
  async verify(r, f) {
    const to = `reviewer-${RUN.toLowerCase()}@example.com`
    const msgs = await until(async () => {
      const m = (await (await fetch(`http://localhost:8025/api/v1/search?query=to:${to}`)).json()).messages ?? []

      return m.length >= 2 ? m : null
    }, 30000)

    check(f, (msgs ?? []).length >= 2, 'emails received: ' + (msgs ?? []).length)
    // Approve through the email's decision link.
    const m = (msgs ?? []).find((x) => /Approve by mail/.test(x.Subject))

    if (!m) {
      return check(f, false, 'no item email')
    }

    const full = await (await fetch(`http://localhost:8025/api/v1/message/${m.ID}`)).json()
    const tok = (full.Text.match(/decide\/([A-Za-z0-9_-]+)\?a(?:ction)?=approve/) ||
      full.Text.match(/decide\/([A-Za-z0-9_.-]+)/) ||
      [])[1]

    check(f, tok, 'no decide link in email')

    if (tok) {
      const link = full.Text.match(/https?:\/\/\S*decide\/\S+/g)
      const approve = (link ?? []).find((l) => /approve/i.test(l)) ?? link?.[0]
      const u = new URL(approve.replace(/[>)\]]+$/, ''))
      const res = await fetch(u, { method: 'POST' })

      check(f, res.ok, 'decide POST ' + res.status + ' ' + u.pathname + u.search)
      const it = (await items((await col(N('Mailed'))).id))[0]

      check(f, it?.status === 'approved', 'mail approval status ' + it?.status)
    }
  },
})

S.push({
  name: 'await-decision',
  async during() {
    const c = await until(() => col(N('Wait')), 120000)
    const it = c && (await until(async () => (await items(c.id))[0], 60000))

    if (it) {
      await sleep(4000)
      await api('POST', `/v1/items/${it.id}/actions/reject`, { feedback: 'Use a shorter headline' })
    }
  },
  prompt: `In staqly create a collection "${N('Wait')}" and submit a text item titled "Headline v1" with content "A very long and winding headline". Then wait (await_decision) for a human to decide, up to 60 seconds. If it is rejected, read the feedback and submit an improved item "Headline v2" that follows it. Tell me what happened.`,
  async verify(r, f) {
    check(f, r.tools.includes('await_decision'), 'await_decision not used')
    const it = await items((await col(N('Wait')))?.id)

    check(
      f,
      it.some((x) => x.title === 'Headline v2'),
      'no v2 after feedback: ' + it.map((x) => x.title),
    )
  },
})

S.push({
  name: 'media-upload',
  prompt: `In staqly upload a small media file: a PNG named "dot.png" whose base64 content is "iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAYAAAAfFcSJAAAADUlEQVR42mP8z8BQDwAEhQGAhKmMIQAAAABJRU5ErkJggg==". Then create a collection "${N('Media')}" with an image item "Dot" that uses that media. Tell me the media id.`,
  async verify(r, f) {
    const it = (await items((await col(N('Media')))?.id ?? 'x'))[0]

    check(f, it?.type === 'image' && it?.media?.id, 'image item with media: ' + JSON.stringify(it)?.slice(0, 200))
  },
})

S.push({
  name: 'workspace-usage-export-events',
  prompt: `In staqly: show this workspace's usage (items by status), list the last 5 events of type item.approved, export the event log (just tell me how many lines), and start a full workspace export and wait until it is ready. Report each.`,
  async verify(r, f) {
    for (const t of ['get_usage', 'list_events', 'export_events', 'start_workspace_export']) {
      check(f, r.tools.includes(t), 'not called: ' + t)
    }

    const ex = (await get('/v1/workspace/exports')).data ?? []

    // a concurrent purge expires ready exports on purpose, so finished is enough
    check(f, ex[0]?.finished_at && ex[0]?.size_bytes > 0, 'no finished export: ' + JSON.stringify(ex[0]))
  },
})

S.push({
  name: 'delete-purge',
  async before() {
    const c = (await api('POST', '/v1/collections', { name: N('Trash') })).body

    await api('POST', `/v1/collections/${c.id}/items`, { title: 'Delete me', content: 'x' })
    await api('POST', `/v1/collections/${c.id}/items`, { title: 'Purge me', content: 'secret' })
  },
  prompt: `In staqly collection "${N('Trash')}": delete the item "Delete me" (keep its history), and purge "Purge me" completely including its history. Then delete the collection itself.`,
  async verify(r, f) {
    check(f, !(await col(N('Trash'))), 'collection still exists')
    check(f, r.tools.includes('purge_item') && r.tools.includes('delete_item'), 'tools ' + r.tools)
  },
})

S.push({
  name: 'cloud-only-local',
  prompt: `In staqly, create an API key named "ci" with read-only scopes and tell me the key. If that's not possible here, explain why in one sentence.`,
  async verify(r, f) {
    check(f, !/stq_|sk_[a-z0-9]{10}/i.test(r.final), 'printed a key in local mode')
  },
})

S.push({
  name: 'flow-and-actions-list',
  async before() {
    const c = (await api('POST', '/v1/collections', { name: N('Flow'), actions: [{ name: 'regenerate' }] })).body
    const i = (await api('POST', `/v1/collections/${c.id}/items`, { title: 'Flow item', content: 'x' })).body

    await api('POST', `/v1/items/${i.id}/actions/reject`, { feedback: 'nope' })
  },
  prompt: `In staqly explain the flow of collection "${N('Flow')}" (use get_flow), and list which actions are available right now on its item "Flow item". Do not run any action.`,
  async verify(r, f) {
    check(f, r.tools.includes('get_flow') && r.tools.includes('list_available_actions'), 'tools ' + r.tools)
    check(f, /regenerate/i.test(r.final), 'regenerate not mentioned')
    const it = (await items((await col(N('Flow'))).id))[0]

    check(f, it?.status === 'rejected', 'item changed: ' + it?.status)
  },
})

// --- run with limited concurrency
const todo = S.filter((s) => !ONLY.length || ONLY.includes(s.name))
const results = []
let next = 0

async function worker() {
  while (next < todo.length) {
    const s = todo[next++]
    const t0 = Date.now()

    try {
      await s.before?.()
      const [r] = await Promise.all([claude(s.name, s.prompt), s.during?.()])
      const fails = []

      await s.verify(r, fails)
      results.push({
        name: s.name,
        ok: !fails.length,
        fails,
        tools: r.tools,
        toolErrors: r.errors.length,
        cost: r.cost,
        secs: Math.round((Date.now() - t0) / 1000),
        final: r.final.slice(0, 400),
      })
    } catch (e) {
      results.push({ name: s.name, ok: false, fails: ['harness: ' + e.stack], tools: [] })
    }

    const x = results.at(-1)

    console.log(`${x.ok ? 'PASS' : 'FAIL'} ${x.name} (${x.secs}s, $${x.cost?.toFixed(2)}) ${x.fails.join(' | ')}`)
  }
}

await Promise.all(Array.from({ length: 5 }, worker))
writeFileSync(OUT + 'results.json', JSON.stringify(results, null, 2))

// --- cleanup: collections and webhooks created by this run
for (const c of ((await get('/v1/collections?limit=200')).data ?? []).filter((x) => x.name.startsWith(RUN))) {
  await api('DELETE', `/v1/collections/${c.id}?force=true`)
}

for (const w of ((await get('/v1/webhooks')).data ?? []).filter((x) => x.url.includes(RUN))) {
  await api('DELETE', `/v1/webhooks/${w.id}`)
}

for (const w of ((await get('/v1/watchers')).data ?? []).filter(
  (x) => x.name.includes('Feed') && x.url.includes('8799'),
)) {
  await api('DELETE', `/v1/watchers/${w.id}`)
}

for (const ch of ((await get('/v1/notification-channels')).data ?? []).filter((c) => /^T[a-z0-9]{5} /.test(c.name))) {
  await api('DELETE', `/v1/notification-channels/${ch.id}`)
}

console.log(
  `\n${results.filter((x) => x.ok).length}/${results.length} passed, run ${RUN}, $${results.reduce((a, x) => a + (x.cost ?? 0), 0).toFixed(2)}, logs in ${OUT}`,
)
recv.close()
process.exit(results.every((x) => x.ok) ? 0 : 1)
