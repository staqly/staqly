// Real n8n integration suite (CI: .github/workflows/n8n.yml): starts n8n from integrations/n8n/compose.yml
// next to the running dev stack (`make dev`: container `staqly` on the `staqly_default`
// network, local mode), imports real workflows over n8n's public API, drives them and
// checks every side effect over the staqly REST API. Tears n8n down at the end (KEEP=1
// keeps it). See e2e/README.md.
//
// The story: posts go to a channel on "your own automation" (provider custom); staqly asks
// n8n to publish them (post.publish_requested), n8n publishes with a mock and reports the
// result; n8n also writes posts, feeds a schedule from an endpoint and polls the work list.
//
//   pnpm --filter @staqly/e2e test:n8n            # all scenarios (about 3 min)
//   node e2e/n8n/run.mjs demo failure-retry       # selected scenarios
import { execFileSync } from 'node:child_process'
import crypto from 'node:crypto'
import { mkdirSync, readFileSync, writeFileSync } from 'node:fs'
import { tmpdir } from 'node:os'

const API = 'http://localhost:3333'
const API_FROM_N8N = 'http://api:3333'
const N8N_PORT = process.env.N8N_PORT ?? '25678'
const N8N = `http://localhost:${N8N_PORT}`
const HOOK_FROM_STAQLY = 'http://n8n:5678/webhook/'
const PROJECT = 'staqly-n8n-e2e'
const ROOT = new URL('../../', import.meta.url).pathname
const COMPOSE = ['compose', '-p', PROJECT, '-f', `${ROOT}integrations/n8n/compose.yml`]
const RUN = 'n8nE2E' + Date.now().toString(36).slice(-5)
const SLUG = RUN.toLowerCase()
const OUT = `${tmpdir()}/staqly-n8n-e2e/${RUN}/`
const ONLY = process.argv.slice(2)
const OWNER = { email: 'e2e-owner@example.com', password: 'E2e-n8n-Passw0rd' }

mkdirSync(OUT, { recursive: true })

// --- helpers
const sleep = (ms) => new Promise((r) => setTimeout(r, ms))
const log = (...a) => console.log(new Date().toISOString().slice(11, 19), ...a)

async function request(base, method, path, body, headers = {}) {
  const init = { method, headers: { ...headers } }

  if (body !== undefined) {
    init.headers['content-type'] = 'application/json'
    init.body = typeof body === 'string' ? body : JSON.stringify(body)
  }

  const r = await fetch(base + path, init)
  const t = await r.text()
  let j = null

  try {
    j = JSON.parse(t)
  } catch {}

  return { status: r.status, headers: r.headers, body: j ?? t }
}

const api = (method, path, body, headers) => request(API, method, path, body, headers)
const get = async (p) => (await api('GET', p)).body
const post = (id) => get(`/v1/posts/${id}`)

async function until(fn, ms = 30000, every = 500) {
  const end = Date.now() + ms
  let v

  while (Date.now() < end) {
    if ((v = await fn())) {
      return v
    }

    await sleep(every)
  }

  return v
}

const postsIn = async (cid, q = '') => (await get(`/v1/posts?collection_id=${cid}&limit=100${q}`)).data ?? []
const events = async (id) => ((await get(`/v1/posts/${id}/events?limit=100`)).data ?? []).map((e) => e.type)
const deliveries = async (q) => (await get(`/v1/webhook-deliveries?limit=100&${q}`)).data ?? []

function docker(args, opts = {}) {
  return execFileSync('docker', args, { encoding: 'utf8', env: { ...process.env, N8N_PORT }, ...opts })
}

// --- n8n
let cookie = ''
let n8nKey = ''

const n8n = (method, path, body) => request(N8N, method, path, body, { 'X-N8N-API-KEY': n8nKey })
const rest = (method, path, body) => request(N8N, method, path, body, { cookie })

async function startN8n() {
  log('starting n8n on', N8N)
  docker([...COMPOSE, 'down', '-v'], { stdio: 'inherit' }) // a fresh n8n: no workflows from an earlier run
  docker([...COMPOSE, 'up', '-d', '--wait'], { stdio: 'inherit' })
  const ready = await until(async () => {
    try {
      return (await fetch(`${N8N}/healthz/readiness`)).ok
    } catch {
      return false
    }
  }, 120000)

  if (!ready) {
    throw new Error('n8n did not become ready')
  }

  const version = docker([...COMPOSE, 'exec', '-T', 'n8n', 'n8n', '--version']).trim()

  log('n8n', version)
  let r = await request(N8N, 'POST', '/rest/owner/setup', { ...OWNER, firstName: 'E2E', lastName: 'Owner' })

  if (r.status !== 200) {
    r = await request(N8N, 'POST', '/rest/login', { emailOrLdapLoginId: OWNER.email, password: OWNER.password })
  }

  if (r.status !== 200) {
    throw new Error(`n8n owner setup/login: ${r.status} ${JSON.stringify(r.body)}`)
  }

  cookie = (r.headers.get('set-cookie') ?? '').split(';')[0]
  const scopes = (await rest('GET', '/rest/api-keys/scopes')).body.data
  const key = await rest('POST', '/rest/api-keys', { label: RUN, expiresAt: null, scopes })

  n8nKey = key.body?.data?.rawApiKey

  if (!n8nKey) {
    throw new Error(`n8n API key: ${key.status} ${JSON.stringify(key.body)}`)
  }

  return version
}

function stopN8n() {
  if (process.env.KEEP === '1') {
    log(`KEEP=1: n8n left running on ${N8N} (owner ${OWNER.email} / ${OWNER.password})`)

    return
  }

  docker([...COMPOSE, 'down', '-v'], { stdio: 'inherit' })
}

const workflows = {}

async function importWorkflow(wf, activate = true) {
  const body = { name: wf.name, nodes: wf.nodes, connections: wf.connections, settings: wf.settings ?? {} }

  body.settings = { ...body.settings, executionOrder: 'v1', saveDataSuccessExecution: 'all' }
  const r = await n8n('POST', '/api/v1/workflows', body)

  if (r.status !== 200) {
    throw new Error(`import ${wf.name}: ${r.status} ${JSON.stringify(r.body)}`)
  }

  workflows[wf.name] = r.body.id
  hookPaths[r.body.id] = wf.nodes.filter((n) => n.type === 'n8n-nodes-base.webhook').map((n) => n.parameters.path)

  if (activate) {
    await setActive(r.body.id, true)
  }

  return r.body.id
}

const hookPaths = {}

// (De)activates a workflow and waits until n8n has (un)registered its production webhooks:
// the public API answers before the routes change, so a request right after activate can
// still get 404 (a GET on a registered POST webhook answers "not registered for GET").
async function setActive(id, on) {
  const r = await n8n('POST', `/api/v1/workflows/${id}/${on ? 'activate' : 'deactivate'}`)

  if (r.status !== 200) {
    throw new Error(`${on ? 'activate' : 'deactivate'} ${id}: ${r.status} ${JSON.stringify(r.body)}`)
  }

  for (const path of hookPaths[id] ?? []) {
    const ok = await until(
      async () => {
        const m = (await request(N8N, 'GET', `/webhook/${path}`)).body?.message ?? ''

        return on === /not registered for GET/.test(m)
      },
      15000,
      200,
    )

    if (!ok) {
      throw new Error(`webhook ${path} did not become ${on ? 'active' : 'inactive'}`)
    }
  }
}

async function executions(workflowId) {
  const r = await n8n('GET', `/api/v1/executions?workflowId=${workflowId}&limit=250`)

  return r.body?.data ?? []
}

async function execution(id) {
  return (await n8n('GET', `/api/v1/executions/${id}?includeData=true`)).body
}

// Error message of a failed execution (first node error found).
function execError(ex) {
  const rd = ex?.data?.resultData

  return rd?.error?.message ?? ''
}

// --- workflow builders
function workflow(name) {
  let x = 0
  const wf = { name, nodes: [], connections: {} }

  wf.add = (n) => {
    n.id = crypto.randomUUID()
    n.position = [x, 0]
    x += 240
    wf.nodes.push(n)

    return n.name
  }

  wf.link = (from, to, output = 0) => {
    const c = (wf.connections[from] ??= { main: [] })

    while (c.main.length <= output) {
      c.main.push([])
    }

    c.main[output].push({ node: to, type: 'main', index: 0 })
  }

  wf.chain = (...names) => names.slice(1).forEach((n, i) => wf.link(names[i], n))

  return wf
}

const webhookNode = (name, path, extra = {}) => ({
  name,
  type: 'n8n-nodes-base.webhook',
  typeVersion: 2,
  webhookId: crypto.randomUUID(),
  parameters: { httpMethod: 'POST', path, responseMode: 'onReceived', options: {}, ...extra },
})

const codeNode = (name, jsCode) => ({ name, type: 'n8n-nodes-base.code', typeVersion: 2, parameters: { jsCode } })

// HTTP Request 4.2 with a JSON body expression; staqly errors fail the node.
const httpNode = (name, method, url, jsonBody, { headers, retry, fullResponse } = {}) => ({
  name,
  type: 'n8n-nodes-base.httpRequest',
  typeVersion: 4.2,
  ...(retry ? { retryOnFail: true, maxTries: 4, waitBetweenTries: 1000 } : {}),
  parameters: {
    method,
    url,
    ...(headers
      ? {
          sendHeaders: true,
          headerParameters: { parameters: Object.entries(headers).map(([n, v]) => ({ name: n, value: v })) },
        }
      : {}),
    ...(jsonBody ? { sendBody: true, specifyBody: 'json', jsonBody } : {}),
    options: fullResponse ? { response: { response: { fullResponse: true } } } : {},
  },
})

function switchNode(name, field, values) {
  return {
    name,
    type: 'n8n-nodes-base.switch',
    typeVersion: 3.2,
    parameters: {
      rules: {
        values: values.map((v) => ({
          conditions: {
            options: { caseSensitive: true, leftValue: '', typeValidation: 'strict', version: 2 },
            conditions: [
              { id: v, leftValue: `={{ ${field} }}`, rightValue: v, operator: { type: 'string', operation: 'equals' } },
            ],
            combinator: 'and',
          },
          renameOutput: true,
          outputKey: v,
        })),
      },
      options: { fallbackOutput: 'none' },
    },
  }
}

// The signature check shipped in the demo workflow, reused verbatim with a secret.
const demo = JSON.parse(readFileSync(`${ROOT}integrations/n8n/instagram-avatar-demo.json`, 'utf8'))
const demoNode = (name) => demo.nodes.find((n) => n.name === name)

function verifyCode(secret) {
  const code = demoNode('Verify & parse').parameters.jsCode

  if (!code.includes("webhookSecret: '',")) {
    throw new Error('Verify & parse: webhookSecret placeholder not found')
  }

  return code.replace("webhookSecret: '',", `webhookSecret: ${JSON.stringify(secret)},`)
}

// Signs a body like staqly does.
const sign = (secret, ts, body) => 'sha256=' + crypto.createHmac('sha256', secret).update(`${ts}.${body}`).digest('hex')

// --- MCP (Streamable HTTP, stateless JSON-RPC)
async function mcp(name, args) {
  const r = await fetch(`${API}/mcp`, {
    method: 'POST',
    headers: { 'content-type': 'application/json', accept: 'application/json, text/event-stream' },
    body: JSON.stringify({ jsonrpc: '2.0', id: Date.now(), method: 'tools/call', params: { name, arguments: args } }),
  })
  let t = await r.text()

  if ((r.headers.get('content-type') ?? '').includes('text/event-stream')) {
    t = t
      .split('\n')
      .filter((l) => l.startsWith('data:'))
      .map((l) => l.slice(5).trim())
      .pop()
  }

  const res = JSON.parse(t).result

  if (!res || res.isError) {
    throw new Error(`MCP ${name}: ${t}`)
  }

  return res.structuredContent
}

// --- results
const results = []
let current = null

function check(cond, msg, evidence = '') {
  current.checks.push({ ok: !!cond, msg, evidence })

  if (!cond) {
    log('  FAIL', msg, evidence)
  }
}

const note = (s) => current.evidence.push(s)

// --- shared fixtures (created once)
const fx = {}
const created = { collections: [], webhooks: [], channels: [], schedules: [] }

async function collection(name) {
  const r = await api('POST', '/v1/collections', { name, description: 'n8n e2e' })

  if (r.status !== 201) {
    throw new Error(`collection ${name}: ${r.status} ${JSON.stringify(r.body)}`)
  }

  created.collections.push(r.body.id)

  return r.body
}

async function channel(name) {
  const r = await api('POST', '/v1/channels', { provider: 'custom', name })

  if (r.status !== 201) {
    throw new Error(`channel ${name}: ${r.status} ${JSON.stringify(r.body)}`)
  }

  created.channels.push(r.body.id)

  return r.body
}

async function webhook(url, types, collectionIds, extra = {}) {
  const r = await api('POST', '/v1/webhooks', {
    url,
    description: RUN,
    events: types,
    collection_ids: collectionIds,
    ...extra,
  })

  if (r.status !== 201) {
    throw new Error(`webhook: ${r.status} ${JSON.stringify(r.body)}`)
  }

  created.webhooks.push(r.body.id)

  return r.body
}

/** Waits until the post's targets were handed to the automation (post.publish_requested went out). */
const requested = (id) =>
  until(async () => {
    const p = await post(id)

    return p.targets?.every((t) => t.status === 'publishing' && t.attempts > 0) ? p : null
  })

const settled = (id, ms = 30000) =>
  until(async () => {
    const p = await post(id)

    return p.status === 'published' || p.status === 'failed' ? p : null
  }, ms)

// Producer: POST /webhook/<slug>-create {collection, content, channel, scenario, key, publish} creates a post with
// an Idempotency-Key and answers with the created post (Respond: last node).
function producerWorkflow() {
  const wf = workflow(`${RUN} producer`)
  const hook = wf.add(
    webhookNode('Create request', `${SLUG}-create`, { responseMode: 'lastNode', responseData: 'firstEntryJson' }),
  )
  const create = wf.add(
    httpNode(
      'Create post',
      'POST',
      `=${API_FROM_N8N}/v1/posts`,
      '={{ JSON.stringify({ collection: $json.body.collection, content: $json.body.content, targets: [{ channel_id: $json.body.channel }], source: "n8n", metadata: { scenario: $json.body.scenario || "ok" }, publish_now: $json.body.publish !== false }) }}',
      { headers: { 'Idempotency-Key': '={{ $json.body.key }}' }, retry: true, fullResponse: true },
    ),
  )
  const out = wf.add(
    codeNode(
      'Created',
      `const body = $('Create request').first().json.body
const r = $input.first().json
if (body.failAfterCreate) throw new Error('downstream step failed after create')
return [{ json: { id: r.body.id, status: r.body.status, replayed: (r.headers || {})['idempotent-replayed'] === 'true' } }]`,
    ),
  )

  wf.chain(hook, create, out)

  return wf
}

// Consumer: staqly webhook → Verify & parse (demo code) → by event type:
//   post.publish_requested → mock publish (fails for scenario "fail", once) → report the result
//   post.published / post.failed → record the outcome (nothing to do)
function consumerWorkflow(secret) {
  const wf = workflow(`${RUN} consumer`)
  const V = "$('Verify & parse').item.json"
  const hook = wf.add(webhookNode('Staqly events', `${SLUG}-events`, { options: { rawBody: true } }))
  const verify = wf.add(
    codeNode(
      'Verify & parse',
      verifyCode(secret).replace("apiBaseUrl: 'http://api:3333'", `apiBaseUrl: '${API_FROM_N8N}'`),
    ),
  )
  const route = wf.add(
    switchNode('Route', '$json.event.type', ['post.publish_requested', 'post.published', 'post.failed']),
  )
  const pub = wf.add(
    codeNode(
      'Mock publish',
      `return $input.all().map((it) => {
  const post = it.json.post
  const target = it.json.target
  // scenario "fail": the first try fails, a retry (attempts > 1) succeeds
  if (post.metadata && post.metadata.scenario === 'fail' && (it.json.event.data.post.targets || []).find((t) => t.channel_id === target.channel_id).attempts <= 1) {
    return { json: { status: 'failed', error: 'mock publisher: API quota exceeded' } }
  }
  return { json: { status: 'published', url: 'https://social.example/p/' + post.id, external_id: 'n8n-' + post.id } }
})`,
    ),
  )
  const report = wf.add(
    httpNode(
      'Report result',
      'POST',
      `=${API_FROM_N8N}/v1/posts/{{ ${V}.post.id }}/targets/{{ ${V}.channel.id }}/result`,
      '={{ JSON.stringify($json) }}',
    ),
  )
  const outcome = wf.add(
    codeNode(
      'Record outcome',
      'return $input.all().map((it) => ({ json: { post_id: it.json.post.id, status: it.json.post.status } }))',
    ),
  )

  wf.chain(hook, verify, route)
  wf.link(route, pub, 0)
  wf.link(route, outcome, 1)
  wf.link(route, outcome, 2)
  wf.chain(pub, report)

  return wf
}

// Poller: POST /webhook/<slug>-poll {} reads GET /v1/work and reports every waiting target as published.
function pollerWorkflow() {
  const wf = workflow(`${RUN} poller`)
  const hook = wf.add(
    webhookNode('Poll request', `${SLUG}-poll`, { responseMode: 'lastNode', responseData: 'allEntries' }),
  )
  const list = wf.add(httpNode('List work', 'GET', `=${API_FROM_N8N}/v1/work`))
  const split = wf.add(
    codeNode(
      'Split targets',
      `const mine = $('Poll request').first().json.body.collection_id
return ($input.first().json.data || []).filter((w) => w.kind === 'target' && w.collection.id === mine).map((w) => ({ json: { id: w.post.id, channel: w.channel.id } }))`,
    ),
  )
  const done = wf.add(
    httpNode(
      'Report published',
      'POST',
      `=${API_FROM_N8N}/v1/posts/{{ $json.id }}/targets/{{ $json.channel }}/result`,
      "={{ JSON.stringify({ status: 'published', url: 'https://social.example/polled/' + $json.id }) }}",
    ),
  )

  wf.chain(hook, list, split, done)

  return wf
}

// Down receiver: a second, separately (de)activated consumer for the manual-retry scenario.
function downWorkflow(secret) {
  const wf = workflow(`${RUN} down-receiver`)
  const hook = wf.add(webhookNode('Staqly events', `${SLUG}-down`, { options: { rawBody: true } }))
  const verify = wf.add(
    codeNode(
      'Verify & parse',
      verifyCode(secret).replace("apiBaseUrl: 'http://api:3333'", `apiBaseUrl: '${API_FROM_N8N}'`),
    ),
  )
  const report = wf.add(
    httpNode(
      'Report result',
      'POST',
      `=${API_FROM_N8N}/v1/posts/{{ $json.post.id }}/targets/{{ $json.channel.id }}/result`,
      "={{ JSON.stringify({ status: 'published', url: 'https://social.example/after-retry/' + $json.post.id }) }}",
    ),
  )

  wf.chain(hook, verify, report)

  return wf
}

// Source: a schedule's endpoint. POST /webhook/<slug>-source {schedule, collection_id, channels, slot} (signed like a
// webhook) answers {posts: [...]} with two posts for the slot.
function sourceWorkflow(secret) {
  const wf = workflow(`${RUN} source`)
  const hook = wf.add(
    webhookNode('Posts wanted', `${SLUG}-source`, {
      responseMode: 'lastNode',
      responseData: 'firstEntryJson',
      options: { rawBody: true },
    }),
  )
  const verify = wf.add(
    codeNode(
      'Verify & parse',
      verifyCode(secret)
        .replace("apiBaseUrl: 'http://api:3333'", `apiBaseUrl: '${API_FROM_N8N}'`)
        .replace('const post = (event.data && event.data.post) || {};', 'const post = {};'),
    ),
  )
  const answer = wf.add(
    codeNode(
      'Write posts',
      `const req = $input.first().json.event
return [{ json: { posts: [
  { content: 'From the n8n endpoint, one (' + req.slot + ')', tags: ['endpoint'] },
  { content: 'From the n8n endpoint, two', targets: req.channels.map((c) => ({ channel_id: c, content: 'short two' })) },
] } }]`,
    ),
  )

  wf.chain(hook, verify, answer)

  return wf
}

const produce = (collectionName, content, scenario = 'ok', key = crypto.randomUUID(), extra = {}) =>
  request(N8N, 'POST', `/webhook/${SLUG}-create`, {
    collection: collectionName,
    content,
    channel: fx.channel.id,
    scenario,
    key,
    ...extra,
  })

const sendEvent = (body, headers) => request(N8N, 'POST', `/webhook/${SLUG}-events`, body, headers)
const unixNow = () => Math.floor(Date.now() / 1000)

// --- scenarios
const S = []

S.push({
  name: 'demo',
  integration: 'n8n + cURL',
  async run() {
    const name = `${RUN} Instagram Avatar`
    const channelName = `${RUN} n8n publisher`
    const out = execFileSync(`${ROOT}integrations/n8n/setup-demo.sh`, [`${HOOK_FROM_STAQLY}staqly-publish`], {
      encoding: 'utf8',
      env: { ...process.env, STAQLY_DEMO_COLLECTION: name, STAQLY_DEMO_CHANNEL: channelName },
    })
    const lines = out.split('\n').map((l) => l.trim())
    const secret = lines[lines.findIndex((l) => l.startsWith('1. Paste the webhook secret')) + 2]
    const token = lines[lines.findIndex((l) => l.startsWith('2. Paste the demo token')) + 1]
    const c = await get(`/v1/collections/${encodeURIComponent(name)}`)

    created.collections.push(c.id)
    const ch = (await get('/v1/channels')).data.find((x) => x.name === channelName)

    created.channels.push(ch?.id)
    const hooks = (await get('/v1/webhooks')).data.filter((w) => w.collection_ids.includes(c.id))

    created.webhooks.push(...hooks.map((w) => w.id))
    check(
      c.id && ch?.provider === 'custom' && hooks.length === 1,
      'setup-demo.sh created the collection, the channel on your own automation and one webhook',
      `${c.id} ${ch?.id} ${hooks.map((w) => w.id)}`,
    )
    check(
      hooks[0]?.events.includes('post.publish_requested'),
      'the webhook listens to post.publish_requested',
      hooks[0]?.events.join(','),
    )
    check(/^[0-9a-f]{48}$/.test(token) && secret.length > 20, 'setup-demo.sh printed secret and demo token')

    // Patch the shipped workflow exactly where the cookbook tells users to.
    const wf = structuredClone(demo)

    wf.name = `${RUN} demo`
    const cfg = wf.nodes.find((n) => n.name === 'Config').parameters.assignments.assignments

    cfg.find((a) => a.name === 'collection').value = name
    cfg.find((a) => a.name === 'channel').value = channelName
    const ver = wf.nodes.find((n) => n.name === 'Verify & parse')

    ver.parameters.jsCode = ver.parameters.jsCode.replace(
      "webhookSecret: '',",
      `webhookSecret: ${JSON.stringify(secret)},`,
    )
    const tok = wf.nodes.find((n) => n.name === 'Check demo token')

    tok.parameters.jsCode = tok.parameters.jsCode.replace(
      "const DEMO_TOKEN = '';",
      `const DEMO_TOKEN = ${JSON.stringify(token)};`,
    )

    for (const n of wf.nodes) {
      if (n.type === 'n8n-nodes-base.webhook') {
        n.webhookId = crypto.randomUUID()
      }
    }

    const wid = await importWorkflow(wf)

    note(`workflow ${wid}`)

    // generate without / with the token
    const before = (await postsIn(c.id)).length
    const bad = await request(N8N, 'POST', '/webhook/staqly-demo-generate', {}, { 'X-Demo-Token': 'wrong' })
    const badEx = await until(async () => (await executions(wid)).find((e) => e.status === 'error'), 10000)
    const badMsg = badEx ? execError(await execution(badEx.id)) : ''

    check(
      (await postsIn(c.id)).length === before && /Invalid or missing X-Demo-Token/.test(badMsg),
      'generate with a wrong token fails in Check demo token and creates nothing',
      `HTTP ${bad.status}, execution ${badEx?.id}: ${badMsg}`,
    )

    const gen = async () => {
      const n = (await postsIn(c.id)).length
      const r = await request(N8N, 'POST', '/webhook/staqly-demo-generate', {}, { 'X-Demo-Token': token })

      check(r.status === 200, 'generate trigger accepted', `HTTP ${r.status}`)

      return until(async () => {
        const list = await postsIn(c.id, '&sort=created_desc')

        return list.length > n ? list[0] : null
      })
    }

    // generated post: a picture, the n8n channel as its target, published right away through the workflow
    const a = await gen()

    check(
      a?.media_items?.length === 1 && a.media_items[0].mime_type === 'image/png',
      'generated post carries a PNG',
      `${a?.id} ${JSON.stringify(a?.media_items?.map((m) => m.mime_type))}`,
    )
    check(
      a?.targets?.[0]?.channel_id === ch.id,
      'the post targets the n8n channel',
      JSON.stringify(a?.targets?.map((t) => t.channel_id)),
    )
    const done = await settled(a.id)

    check(
      done?.status === 'published' && /instagram\.example\/p\//.test(done.targets[0].external_url),
      'publish_now → post.publish_requested → Mock Publisher → reported published with the link',
      `${a.id} ${done?.status} ${done?.targets?.[0]?.external_url}`,
    )
    check(
      done?.targets?.[0]?.external_id?.startsWith('17'),
      'the platform id is recorded on the target',
      done?.targets?.[0]?.external_id,
    )
    const evs = await events(a.id)

    check(
      ['post.created', 'post.publish_requested', 'post.target_published', 'post.published'].every((t) =>
        evs.includes(t),
      ),
      'history: created → publish_requested → target_published → published',
      evs.join(','),
    )
    // post.published reached the workflow too (Record outcome ran) and no execution failed after the token test
    await sleep(2500)
    const exs = await executions(wid)

    check(
      exs.filter((e) => e.status === 'success').length >= 3,
      'generate, publish and outcome executions succeeded',
      exs.map((e) => `${e.id}:${e.status}`).join(' '),
    )
    check(
      !exs.some((e) => e.status === 'error' && e.startedAt > done.created_at),
      'no failed demo executions after the first generation',
      exs.map((e) => `${e.id}:${e.status}`).join(' '),
    )

    // forged webhook
    const fakeBody = JSON.stringify({
      id: 'evt_forged',
      type: 'post.publish_requested',
      data: { post: { id: a.id }, channel: { id: ch.id }, target: {} },
    })
    const ts = Math.floor(Date.now() / 1000)
    const forged = await request(N8N, 'POST', '/webhook/staqly-publish', fakeBody, {
      'x-staqly-timestamp': String(ts),
      'x-staqly-signature': sign('wrong-secret', ts, fakeBody),
      'x-staqly-event-id': 'evt_forged',
    })

    await sleep(1500)
    const last = (await executions(wid))[0]
    const ex = await execution(last.id)

    check(
      last.status === 'error' && /Invalid Staqly signature/.test(execError(ex)),
      'forged webhook rejected by Verify & parse',
      `HTTP ${forged.status}, execution ${last.id} ${last.status}: ${execError(ex)}`,
    )
    check((await post(a.id)).status === 'published', 'the forged event changed nothing')
    note(`post ${a.id}`)
  },
})

S.push({
  name: 'publish-success',
  integration: 'n8n + cURL',
  async run() {
    const r = await produce(fx.main.name, `${RUN} publish me`)
    const id = r.body?.id

    check(
      r.status === 200 && id && r.body.status === 'publishing',
      'n8n producer created a post and published it on the n8n channel',
      `HTTP ${r.status} ${id} ${r.body?.status}`,
    )
    const it = await settled(id)

    check(
      it?.status === 'published' &&
        it.targets[0].external_url === `https://social.example/p/${id}` &&
        it.targets[0].external_id === `n8n-${id}`,
      'n8n consumer reported published with url and external_id',
      `${id} ${it?.status} ${JSON.stringify(it?.targets?.[0])}`,
    )
    const dl = await deliveries(`post_id=${id}`)

    check(
      dl.length >= 2 && dl.every((x) => x.status === 'succeeded'),
      'deliveries post.publish_requested + post.published succeeded',
      dl.map((x) => `${x.event_type}:${x.status}:${x.last_status_code ?? ''}`).join(' '),
    )
    // A draft stays a draft: the producer can also just write.
    const d = await produce(fx.main.name, `${RUN} just a draft`, 'ok', crypto.randomUUID(), { publish: false })

    check(d.body?.status === 'draft', 'a post without publish stays a draft for a schedule or a person', d.body?.status)
    note(`post ${id}; events ${(await events(id)).join(',')}`)
  },
})

S.push({
  name: 'failure-retry',
  integration: 'n8n + cURL',
  async run() {
    const id = (await produce(fx.main.name, `${RUN} fail once`, 'fail')).body.id
    const failed = await settled(id)

    check(
      failed?.status === 'failed' && failed.targets[0].error === 'mock publisher: API quota exceeded',
      'n8n reported failed with the error',
      `${id} ${failed?.status} ${failed?.targets?.[0]?.error}`,
    )
    const rt = await api('POST', `/v1/posts/${id}/targets/${fx.channel.id}/retry`)

    check(
      rt.status === 202 && rt.body?.status === 'publishing',
      'cURL retries the target: the post is publishing again',
      `${rt.status} ${rt.body?.status}`,
    )
    const ok = await settled(id)

    check(
      ok?.status === 'published' && ok.targets[0].attempts === 2,
      'n8n published it on the second request',
      `${id} ${ok?.status} attempts=${ok?.targets?.[0]?.attempts}`,
    )
    const evs = await events(id)

    check(
      evs.filter((t) => t === 'post.publish_requested').length === 2 &&
        evs.includes('post.target_failed') &&
        evs.includes('post.failed') &&
        evs.includes('post.published'),
      'history: two requests, one failure, then published',
      evs.join(','),
    )
  },
})

S.push({
  name: 'endpoint-down-auto-retry',
  integration: 'n8n + cURL',
  async run() {
    await setActive(workflows[`${RUN} consumer`], false)
    const id = (await produce(fx.main.name, `${RUN} while n8n is down`)).body.id
    const failing = await until(async () => {
      const dl = (await deliveries(`post_id=${id}`)).filter((x) => x.attempts >= 1 && x.status === 'pending')

      return dl.length >= 1 ? dl : null
    }, 20000)

    check(
      failing,
      'the delivery fails and stays queued for retry while the n8n workflow is inactive',
      JSON.stringify(failing?.map((x) => [x.event_type, x.status, x.attempts, x.last_status_code, x.next_attempt_at])),
    )
    check((await post(id)).status === 'publishing', 'the post waits in publishing')
    await setActive(workflows[`${RUN} consumer`], true)
    const it = await until(
      async () => {
        const x = await post(id)

        return x.status === 'published' ? x : null
      },
      90000,
      2000,
    )
    const dl = await deliveries(`post_id=${id}`)

    check(it, 'automatic retry delivered after reactivation and n8n published the post', `${id} ${it?.status}`)
    note(`deliveries ${dl.map((x) => `${x.id}:${x.event_type}:${x.status}:attempts=${x.attempts}`).join(' ')}`)
  },
})

S.push({
  name: 'endpoint-down-manual-retry',
  integration: 'n8n + cURL',
  async run() {
    await setActive(workflows[`${RUN} down-receiver`], false)
    const r = await api('POST', '/v1/posts', {
      collection: fx.down.id,
      content: `${RUN} manual retry`,
      targets: [{ channel_id: fx.downChannel.id }],
      publish_now: true,
    })
    const id = r.body.id
    const failed = await until(async () => {
      const dl = (await deliveries(`post_id=${id}&webhook_id=${fx.downHook.id}`)).filter((x) => x.status === 'failed')

      return dl.length ? dl[0] : null
    }, 20000)

    check(
      failed?.last_status_code === 404,
      'delivery failed for good (max_attempts 1, n8n answers 404)',
      `${failed?.id} ${failed?.status} ${failed?.last_status_code}`,
    )
    await setActive(workflows[`${RUN} down-receiver`], true)
    const rt = await api('POST', `/v1/webhook-deliveries/${failed.id}/retry`)

    check(rt.status === 202, 'manual delivery retry accepted', `HTTP ${rt.status}`)
    const it = await settled(id)

    check(
      it?.status === 'published' && /after-retry/.test(it.targets[0].external_url),
      'retried delivery reached n8n, post published',
      `${id} ${it?.status}`,
    )
  },
})

S.push({
  name: 'signature',
  integration: 'n8n + cURL',
  async run() {
    // A publishing post no webhook covers, so only forged events could touch it.
    const r = await api('POST', '/v1/posts', {
      collection: fx.poll.id,
      content: `${RUN} forge target`,
      targets: [{ channel_id: fx.channel.id }],
      publish_now: true,
    })
    const id = r.body.id

    await requested(id)
    const wid = workflows[`${RUN} consumer`]
    const ev = (eid) =>
      JSON.stringify({
        id: eid,
        type: 'post.publish_requested',
        created_at: new Date().toISOString(),
        data: {
          post: { id, content: 'x', metadata: {}, targets: [{ channel_id: fx.channel.id, attempts: 1 }] },
          channel: { id: fx.channel.id },
          target: { channel_id: fx.channel.id, content: 'x' },
          collection: { id: fx.poll.id },
        },
      })
    const cases = [
      [
        'wrong secret',
        ev('evt_f1'),
        (b, t) => ({ 'x-staqly-timestamp': `${t}`, 'x-staqly-signature': sign('nope', t, b) }),
      ],
      [
        'tampered body',
        ev('evt_f2'),
        (b, t) => ({
          'x-staqly-timestamp': `${t}`,
          'x-staqly-signature': sign(fx.mainHook.secret, t, ev('evt_other')),
        }),
      ],
      [
        'stale timestamp',
        ev('evt_f3'),
        (b, t) => ({ 'x-staqly-timestamp': `${t - 600}`, 'x-staqly-signature': sign(fx.mainHook.secret, t - 600, b) }),
      ],
      ['missing signature', ev('evt_f4'), (b, t) => ({ 'x-staqly-timestamp': `${t}` })],
    ]

    for (const [label, body, h] of cases) {
      const eid = JSON.parse(body).id
      const before = new Set((await executions(wid)).map((e) => e.id))

      await sendEvent(body, h(body, unixNow()))
      // The forged request's own execution, found by its event id: a genuine staqly delivery (a queued retry
      // from an earlier scenario) can land in the same window and must not be mistaken for it.
      const full = await until(async () => {
        for (const run of (await executions(wid)).filter((x) => !before.has(x.id))) {
          const x = await execution(run.id)

          if (JSON.stringify(x?.data ?? {}).includes(eid)) {
            return x
          }
        }

        return null
      }, 10000)

      check(
        full?.status === 'error',
        `${label}: n8n execution fails`,
        `execution ${full?.id} ${full?.status}: ${execError(full)}`,
      )
    }

    await sleep(1000)
    check((await post(id)).status === 'publishing', 'forged events changed nothing (post still publishing)')
    // A correctly signed event goes through; replaying it is dropped by the dedupe.
    const good = ev('evt_' + RUN)
    const t = unixNow()
    const headers = {
      'x-staqly-timestamp': `${t}`,
      'x-staqly-signature': sign(fx.mainHook.secret, t, good),
      'x-staqly-event-id': 'evt_' + RUN,
    }

    await sendEvent(good, headers)
    const done = await settled(id, 15000)

    check(done?.status === 'published', 'a correctly signed event is processed', `${id} ${done?.status}`)
    const before = (await executions(wid)).length

    await sendEvent(good, headers)
    await until(async () => (await executions(wid)).length > before, 10000)
    await sleep(1000)
    const evs = await events(id)

    check(
      evs.filter((x) => x === 'post.published').length === 1,
      'replayed event id deduplicated (one publication)',
      evs.join(','),
    )
  },
})

S.push({
  name: 'idempotency',
  integration: 'n8n',
  async run() {
    const key = `${RUN}-idem-1`
    const a = await produce(fx.poll.name, `${RUN} idempotent`, 'ok', key, { publish: false })
    const b = await produce(fx.poll.name, `${RUN} idempotent`, 'ok', key, { publish: false })

    check(
      a.body?.id && a.body.id === b.body?.id && b.body.replayed === true,
      'same Idempotency-Key from a re-run returns the same post (Idempotent-Replayed)',
      `${a.body?.id} ${b.body?.id} replayed=${b.body?.replayed}`,
    )
    // n8n's own "retry execution" of a run that failed after creating the post.
    const key2 = `${RUN}-idem-2`
    const f = await produce(fx.poll.name, `${RUN} retried execution`, 'ok', key2, {
      failAfterCreate: true,
      publish: false,
    })
    const pid = workflows[`${RUN} producer`]
    const failed = await until(async () => (await executions(pid)).find((e) => e.status === 'error'), 10000)

    check(failed, 'producer execution failed after creating the post', `HTTP ${f.status} execution ${failed?.id}`)
    const rr = await n8n('POST', `/api/v1/executions/${failed.id}/retry`, { loadWorkflow: false })

    check(
      rr.status === 200,
      'n8n retry of the failed execution ran',
      `HTTP ${rr.status} ${JSON.stringify(rr.body).slice(0, 160)}`,
    )
    await sleep(1500)
    const list = (await postsIn(fx.poll.id)).filter((x) => x.content === `${RUN} retried execution`)

    check(list.length === 1, 'retried execution created no duplicate', `${list.length} post(s)`)
    const all = (await postsIn(fx.poll.id)).filter((x) => x.content === `${RUN} idempotent`)

    check(all.length === 1, 'exactly one post for the repeated key', `${all.length} post(s)`)
  },
})

S.push({
  name: 'poll',
  integration: 'n8n',
  async run() {
    // No webhook covers this collection: the work waits until the poller reports it.
    const ids = []

    for (let i = 0; i < 3; i++) {
      ids.push(
        (
          await api('POST', '/v1/posts', {
            collection: fx.poll.id,
            content: `${RUN} poll ${i}`,
            targets: [{ channel_id: fx.channel.id }],
          })
        ).body.id,
      )
    }

    await api('POST', `/v1/posts/${ids[0]}/publish`)
    await api('POST', `/v1/posts/${ids[1]}/publish`)
    await requested(ids[0])
    await requested(ids[1])
    const work = (await get('/v1/work')).data
      .filter((w) => w.kind === 'target' && w.collection.id === fx.poll.id)
      .map((w) => w.post.id)

    check(
      work.includes(ids[0]) && work.includes(ids[1]) && !work.includes(ids[2]),
      'GET /v1/work lists the two waiting targets',
      work.join(' '),
    )
    const r = await request(N8N, 'POST', `/webhook/${SLUG}-poll`, { collection_id: fx.poll.id })
    const handled = Array.isArray(r.body) ? r.body.map((x) => x.id) : []

    check(r.status === 200, 'poller ran', `HTTP ${r.status} ${JSON.stringify(r.body).slice(0, 200)}`)
    const st = await Promise.all(ids.map(async (id) => (await post(id)).status))

    check(
      st[0] === 'published' && st[1] === 'published' && st[2] === 'draft',
      'poller published the waiting targets only',
      st.join(','),
    )
    check(
      (await get('/v1/work')).data.filter((w) => w.kind === 'target' && w.collection.id === fx.poll.id).length === 0,
      'the work list is empty afterwards',
    )
    note(`poll handled ${handled.length} post(s): ${handled.join(' ')}`)
  },
})

S.push({
  name: 'schedule-endpoint',
  integration: 'n8n + cURL',
  async run() {
    // A schedule of an empty collection asks the n8n endpoint for posts and publishes what it gets.
    const col = await collection(`${RUN} fed`)
    const secret = `${RUN}-endpoint-secret`
    const wid = await importWorkflow(sourceWorkflow(secret))
    const s = await api('POST', '/v1/schedules', {
      collection: col.id,
      name: 'Fetch from n8n',
      kind: 'recurring',
      rule: { every_minutes: 30 },
      channels: [fx.channel.id],
      source: 'endpoint',
      endpoint_url: `${HOOK_FROM_STAQLY}${SLUG}-source`,
      endpoint_secret: secret,
    })

    created.schedules.push(s.body?.id)
    check(
      s.status === 201,
      'schedule with source endpoint created',
      `${s.status} ${JSON.stringify(s.body).slice(0, 160)}`,
    )
    const ran = await api('POST', `/v1/schedules/${s.body.id}/run`)

    check(
      ran.body?.last_result === 'published 2',
      'the run fetched two posts from n8n and published them',
      `${ran.status} ${ran.body?.last_result}`,
    )
    const posts = await postsIn(col.id, '&sort=created_asc')

    check(
      posts.length === 2 &&
        posts[0].source === 'schedule:Fetch from n8n' &&
        posts[1].targets[0].content === 'short two',
      'the posts carry the schedule as source and their own target text',
      JSON.stringify(posts.map((p) => [p.content, p.source, p.targets?.[0]?.content])),
    )
    const done = await Promise.all(posts.map((p) => settled(p.id)))

    check(
      done.every((p) => p?.status === 'published'),
      'the consumer published both',
      done.map((p) => p?.status).join(','),
    )
    const exs = await executions(wid)

    check(
      exs.length === 1 && exs[0].status === 'success',
      'one source execution, verified signature',
      exs.map((e) => `${e.id}:${e.status}`).join(' '),
    )
    // A wrong secret: the run records the failure, nothing is created.
    const bad = await api('PATCH', `/v1/schedules/${s.body.id}`, { endpoint_secret: 'wrong' })

    check(bad.status === 200, 'secret changed', `${bad.status}`)
    const failed = await api('POST', `/v1/schedules/${s.body.id}/run`)

    check(
      (failed.body?.last_result ?? '').startsWith('endpoint failed'),
      'a request n8n cannot verify fails the run',
      failed.body?.last_result,
    )
    check((await postsIn(col.id)).length === 2, 'nothing was created by the failed run')
  },
})

S.push({
  name: 'concurrency',
  integration: 'n8n + cURL',
  async run() {
    const N = 8
    const rs = await Promise.all(Array.from({ length: N }, (_, i) => produce(fx.main.name, `${RUN} parallel ${i}`)))
    const ids = rs.map((r) => r.body?.id)

    check(
      rs.every((r) => r.status === 200) && new Set(ids).size === N,
      `${N} parallel n8n executions created and published ${N} distinct posts`,
      rs.map((r) => r.status).join(','),
    )
    const key = `${RUN}-same-key`
    const same = await Promise.all(Array.from({ length: 5 }, () => produce(fx.main.name, `${RUN} same key`, 'ok', key)))
    const sameIds = new Set(same.map((r) => r.body?.id).filter(Boolean))

    check(
      same.every((r) => r.status === 200) && sameIds.size === 1,
      '5 parallel executions with one Idempotency-Key → one post',
      `${same.map((r) => r.status).join(',')} replayed=${same.map((r) => r.body?.replayed).join(',')} ids=${[...sameIds].join(' ')}`,
    )
    const all = [...ids, ...sameIds]
    const done = await until(
      async () => {
        const st = await Promise.all(all.map(async (id) => (await post(id)).status))

        return st.every((s) => s === 'published') ? st : null
      },
      60000,
      1000,
    )

    check(done, `all ${all.length} posts published by parallel n8n executions`)
    const counts = await Promise.all(
      all.map(async (id) => (await events(id)).filter((t) => t === 'post.published').length),
    )

    check(
      counts.every((n) => n === 1),
      'each post published exactly once',
      counts.join(','),
    )
  },
})

S.push({
  name: 'mcp-continues',
  integration: 'n8n + MCP',
  async run() {
    // MCP writes and publishes, n8n publishes and reports.
    const a = await mcp('create_post', {
      collection: fx.main.name,
      content: `${RUN} mcp writes`,
      targets: [{ channel_id: fx.channel.id }],
    })
    const pub = await mcp('publish_post', { post_id: a.id })

    check(pub?.status === 'publishing', 'MCP publish_post hands the post to the n8n channel', pub?.status)
    const done = await settled(a.id)

    check(
      done?.status === 'published' && done.targets[0].external_url,
      'n8n published the MCP post',
      `${a.id} ${done?.status}`,
    )
    // n8n writes (no publish), MCP sees it in the work list after publishing and reports the result itself.
    const b = (await produce(fx.poll.name, `${RUN} mcp finishes`, 'ok', crypto.randomUUID(), { publish: false })).body
      .id

    await mcp('publish_post', { post_id: b })
    await requested(b)
    const work = await mcp('list_requested_work', {})
    const listed = JSON.stringify(work).includes(b)

    check(listed, 'MCP list_requested_work shows the n8n post waiting on the n8n channel')
    const res = await mcp('report_publish_result', {
      post_id: b,
      channel_id: fx.channel.id,
      status: 'published',
      url: 'https://example.com/p/1',
    })

    check(res?.status === 'published', 'MCP report_publish_result published an n8n-created post', res?.status)
  },
})

// Deletes what this run (and earlier runs kept with KEEP=1) created: collections named n8nE2E...
// with their posts, channels, schedules and webhooks of this suite or pointing at those collections.
async function cleanup() {
  const cols = ((await get('/v1/collections?limit=200')).data ?? []).filter(
    (c) => c.name.startsWith('n8nE2E') || created.collections.includes(c.id),
  )
  const ids = new Set(cols.map((c) => c.id))

  for (const w of (await get('/v1/webhooks')).data ?? []) {
    if (
      w.description.startsWith('n8nE2E') ||
      w.description === 'n8n publish demo' ||
      created.webhooks.includes(w.id) ||
      w.collection_ids.some((c) => ids.has(c))
    ) {
      await api('DELETE', `/v1/webhooks/${w.id}`)
    }
  }

  for (const s of (await get('/v1/schedules')).data ?? []) {
    if (ids.has(s.collection_id) || created.schedules.includes(s.id)) {
      await api('DELETE', `/v1/schedules/${s.id}`)
    }
  }

  for (const c of cols) {
    await api('DELETE', `/v1/collections/${c.id}?confirm=${encodeURIComponent(c.name)}`)
  }

  for (const ch of (await get('/v1/channels')).data ?? []) {
    if (ch.name.startsWith('n8nE2E') || created.channels.includes(ch.id)) {
      await api('DELETE', `/v1/channels/${ch.id}`)
    }
  }

  log(`cleanup: ${cols.length} collection(s) deleted`)
}

// --- main
const selected = ONLY.length ? S.filter((s) => ONLY.includes(s.name)) : S
let failedCount = 0
let version = ''

try {
  version = await startN8n()
  fx.main = await collection(`${RUN} main`)
  fx.poll = await collection(`${RUN} poll`)
  fx.down = await collection(`${RUN} down`)
  fx.channel = await channel(`${RUN} n8n channel`)
  fx.downChannel = await channel(`${RUN} down channel`)
  fx.mainHook = await webhook(
    `${HOOK_FROM_STAQLY}${SLUG}-events`,
    ['post.publish_requested', 'post.published', 'post.failed'],
    [fx.main.id],
  )
  fx.downHook = await webhook(`${HOOK_FROM_STAQLY}${SLUG}-down`, ['post.publish_requested'], [fx.down.id], {
    max_attempts: 1,
  })
  await importWorkflow(producerWorkflow())
  await importWorkflow(consumerWorkflow(fx.mainHook.secret))
  await importWorkflow(pollerWorkflow())
  await importWorkflow(downWorkflow(fx.downHook.secret))

  for (const s of selected) {
    current = { name: s.name, integration: s.integration, checks: [], evidence: [] }
    results.push(current)
    log('▶', s.name)
    const t0 = Date.now()

    try {
      await s.run()
    } catch (e) {
      current.checks.push({ ok: false, msg: 'scenario threw', evidence: String(e?.stack ?? e).slice(0, 500) })
      log('  ERROR', e)
    }

    current.ms = Date.now() - t0
    const bad = current.checks.filter((c) => !c.ok).length

    failedCount += bad ? 1 : 0
    log(
      bad ? '✗' : '✓',
      s.name,
      `${current.checks.length - bad}/${current.checks.length}`,
      `${Math.round(current.ms / 1000)}s`,
    )
  }
} finally {
  writeFileSync(`${OUT}results.json`, JSON.stringify({ run: RUN, n8n: version, results, workflows }, null, 2))

  if (process.env.KEEP !== '1') {
    await cleanup()
  }

  stopN8n()
}

console.log(
  `\n${RUN} n8n ${version}: ${selected.length - failedCount}/${selected.length} scenarios passed; details in ${OUT}results.json`,
)

for (const r of results) {
  for (const c of r.checks) {
    console.log(`${c.ok ? 'ok  ' : 'FAIL'} ${r.name} | ${c.msg}${c.evidence ? ' | ' + c.evidence : ''}`)
  }

  for (const e of r.evidence) {
    console.log(`     ${r.name} | ${e}`)
  }
}

process.exit(failedCount ? 1 : 0)
