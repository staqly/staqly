#!/usr/bin/env node
// Dashboard performance run against a live dashboard (`staqly serve` or the server binary with
// DASHBOARD_ADDR), driven over the Chrome DevTools Protocol: no npm dependency, only a local Chrome.
//
//   node e2e/perf/run.mjs --url http://127.0.0.1:63081 --token stk_local_... [--profile desktop|mobile|both]
//                         [--only load,interact,memory] [--routes /schedule,/activity] [--json out.json] [--chrome /path/to/chrome]
// --token (or STAQLY_API_KEY): the server's access token, traded once for the session cookie Chrome uses.
//
// load:     per route, cold cache: TTFB, FCP, LCP, CLS, TBT (long tasks after FCP), requests (all / API /
//           duplicate API), transferred KB, JS heap. desktop = 40 ms RTT, 10 Mbit/s; mobile = 390x844, 4x
//           CPU slowdown, 150 ms RTT, 1.6 Mbit/s (Lighthouse's "slow 4G" numbers).
// interact: open a post from the list, load 500 rows, command palette typing, post search typing, activity
//           "Load older" (Event Timing durations = input to next paint).
// memory:   2x100 client-side navigations between routes, then 30 post sheet open/close: heap after GC, DOM
//           nodes, JS event listeners, live intervals and EventSources, before and after.
// Seed a big workspace first with e2e/perf/seed.mjs; see e2e/README.md (Performance).
import { spawn } from 'node:child_process'
import { existsSync, mkdtempSync, rmSync, writeFileSync } from 'node:fs'
import { tmpdir } from 'node:os'
import { join } from 'node:path'
import { parseArgs } from 'node:util'

const { values: opt } = parseArgs({
  options: {
    url: { type: 'string', default: 'http://127.0.0.1:63081' },
    profile: { type: 'string', default: 'both' },
    only: { type: 'string', default: 'load,interact,memory' },
    routes: { type: 'string' },
    json: { type: 'string' },
    chrome: { type: 'string', default: process.env.CHROME },
    headful: { type: 'boolean', default: false },
    token: { type: 'string', default: process.env.STAQLY_API_KEY },
  },
})

if (!opt.token) {
  console.error('--token (or STAQLY_API_KEY) is needed: the access token of the server (staqly serve token)')
  process.exit(2)
}

const base = opt.url.replace(/\/$/, '')
const only = new Set(opt.only.split(','))
const DEFAULT_ROUTES = [
  '/schedule',
  '/posts',
  '/posts?collection={col}',
  '/posts/{post}',
  '/channels',
  '/activity',
  '/webhooks',
  '/settings',
]
const PROFILES = {
  desktop: { width: 1440, height: 900, mobile: false, scale: 1, cpu: 1, latency: 40, down: 10_000, up: 10_000 },
  mobile: { width: 390, height: 844, mobile: true, scale: 3, cpu: 4, latency: 150, down: 1_600, up: 750 },
}

const sleep = (ms) => new Promise((r) => setTimeout(r, ms))

function findChrome() {
  const candidates = [
    opt.chrome,
    '/Applications/Google Chrome.app/Contents/MacOS/Google Chrome',
    '/usr/bin/google-chrome',
    '/usr/bin/chromium',
    '/usr/bin/chromium-browser',
  ].filter(Boolean)

  const found = candidates.find((p) => existsSync(p))

  if (!found) {
    throw new Error('No Chrome found: pass --chrome /path/to/chrome or set CHROME')
  }

  return found
}

// ── Minimal CDP client (flattened sessions over the browser websocket) ──────────────────────────────

class CDP {
  #ws
  #id = 0
  #pending = new Map()
  #listeners = new Set()

  static async connect(url) {
    const c = new CDP()

    c.#ws = new WebSocket(url)
    await new Promise((resolve, reject) => {
      c.#ws.addEventListener('open', resolve, { once: true })
      c.#ws.addEventListener('error', reject, { once: true })
    })
    c.#ws.addEventListener('message', (m) => c.#onMessage(JSON.parse(m.data)))

    return c
  }

  #onMessage(msg) {
    if (msg.id !== undefined) {
      const p = this.#pending.get(msg.id)

      this.#pending.delete(msg.id)

      if (msg.error) {
        p?.reject(new Error(`${p.method}: ${msg.error.message}`))
      } else {
        p?.resolve(msg.result)
      }

      return
    }

    for (const l of this.#listeners) {
      l(msg)
    }
  }

  send(method, params = {}, sessionId) {
    const id = ++this.#id

    this.#ws.send(JSON.stringify({ id, method, params, sessionId }))

    return new Promise((resolve, reject) => this.#pending.set(id, { resolve, reject, method }))
  }

  on(fn) {
    this.#listeners.add(fn)

    return () => this.#listeners.delete(fn)
  }

  close() {
    this.#ws.close()
  }
}

// Collects Web Vitals-ish numbers in the page, and counts live intervals / EventSources (leak check).
const PAGE_PROBE = `(() => {
  const P = (window.__perf = { lcp: 0, cls: 0, shifts: [], longtasks: [], events: [], intervals: 0, eventSources: 0 })
  const obs = (type, fn, extra) => { try { new PerformanceObserver((l) => l.getEntries().forEach(fn)).observe({ type, buffered: true, ...extra }) } catch {} }
  obs('largest-contentful-paint', (e) => { P.lcp = e.startTime })
  const what = (n) => n && n.nodeType === 1 ? n.tagName.toLowerCase() + (n.dataset.testid ? '[' + n.dataset.testid + ']' : '') + '.' + String(n.className).split(' ').slice(0, 3).join('.') : String(n && n.nodeName)
  obs('layout-shift', (e) => { if (!e.hadRecentInput) { P.cls += e.value; P.shifts.push({ at: Math.round(e.startTime), value: +e.value.toFixed(4), nodes: (e.sources || []).map((x) => what(x.node)) }) } })
  obs('longtask', (e) => P.longtasks.push([e.startTime, e.duration]))
  obs('event', (e) => P.events.push({ name: e.name, start: e.startTime, duration: e.duration }), { durationThreshold: 16 })
  const live = new Set()
  const si = window.setInterval, ci = window.clearInterval
  window.setInterval = function (...a) { const id = si.apply(this, a); live.add(id); P.intervals = live.size; return id }
  window.clearInterval = function (id) { live.delete(id); P.intervals = live.size; return ci.call(this, id) }
  if (window.EventSource) {
    const ES = window.EventSource
    window.EventSource = class extends ES {
      constructor(...a) { super(...a); P.eventSources++; const c = this.close.bind(this); this.close = () => { if (this.readyState !== 2) P.eventSources--; c() } }
    }
  }
})()`

class Page {
  constructor(cdp, sessionId, targetId) {
    this.cdp = cdp
    this.sessionId = sessionId
    this.targetId = targetId
    this.requests = new Map()
    this.inflight = new Set()
    this.lastNetwork = Date.now()
    this.off = cdp.on((msg) => msg.sessionId === sessionId && this.#onEvent(msg))
  }

  send(method, params) {
    return this.cdp.send(method, params, this.sessionId)
  }

  #onEvent({ method, params }) {
    if (method === 'Network.requestWillBeSent') {
      if (params.request.url.startsWith('data:')) {
        return
      }

      this.requests.set(params.requestId, {
        url: params.request.url,
        method: params.request.method,
        bytes: 0,
        start: params.timestamp,
      })
      this.inflight.add(params.requestId)
      this.lastNetwork = Date.now()
    } else if (method === 'Network.responseReceived') {
      const r = this.requests.get(params.requestId)

      if (r) {
        r.status = params.response.status
        r.encoding = params.response.headers['content-encoding'] || params.response.headers['Content-Encoding']
      }
    } else if (method === 'Network.loadingFinished' || method === 'Network.loadingFailed') {
      const r = this.requests.get(params.requestId)

      if (r && method === 'Network.loadingFinished') {
        r.bytes = params.encodedDataLength
        r.end = params.timestamp
      }

      this.inflight.delete(params.requestId)
      this.lastNetwork = Date.now()
    }
  }

  async eval(expression) {
    const res = await this.send('Runtime.evaluate', { expression, awaitPromise: true, returnByValue: true })

    if (res.exceptionDetails) {
      throw new Error(`eval failed: ${res.exceptionDetails.exception?.description ?? res.exceptionDetails.text}`)
    }

    return res.result.value
  }

  /** Waits until no request has been in flight for `quiet` ms (long-polls excluded by the caller's timeout). */
  async networkIdle(quiet = 800, timeout = 30_000) {
    const end = Date.now() + timeout

    while (Date.now() < end) {
      const busy = [...this.inflight].filter((id) => !/\/wait\b/.test(this.requests.get(id)?.url ?? ''))

      if (busy.length === 0 && Date.now() - this.lastNetwork >= quiet) {
        return
      }

      await sleep(50)
    }
  }

  async waitFor(expression, timeout = 30_000) {
    const end = Date.now() + timeout

    while (Date.now() < end) {
      if (await this.eval(`!!(${expression})`)) {
        return true
      }

      await sleep(50)
    }

    throw new Error(`timed out waiting for ${expression}`)
  }

  async key(key, opts = {}) {
    const code = opts.code ?? (key.length === 1 ? `Key${key.toUpperCase()}` : key)
    const text = key.length === 1 ? key : key === 'Enter' ? '\r' : undefined
    const keyCode = opts.keyCode ?? (key === 'Enter' ? 13 : key === 'Escape' ? 27 : key.toUpperCase().charCodeAt(0))
    const common = { key, code, modifiers: opts.modifiers ?? 0, windowsVirtualKeyCode: keyCode }

    await this.send('Input.dispatchKeyEvent', {
      type: text && !opts.modifiers ? 'keyDown' : 'rawKeyDown',
      text,
      ...common,
    })
    await this.send('Input.dispatchKeyEvent', { type: 'keyUp', ...common })
  }

  async click(selector) {
    const box = await this.eval(`(() => {
      const el = document.querySelector(${JSON.stringify(selector)}); if (!el) return null
      el.scrollIntoView({ block: 'center' }); const r = el.getBoundingClientRect(); return { x: r.x + r.width / 2, y: r.y + r.height / 2 }
    })()`)

    if (!box) {
      throw new Error(`no element ${selector}`)
    }

    for (const type of ['mouseMoved', 'mousePressed', 'mouseReleased']) {
      await this.send('Input.dispatchMouseEvent', { type, x: box.x, y: box.y, button: 'left', clickCount: 1 })
    }
  }

  async heap() {
    await this.send('HeapProfiler.collectGarbage')
    await this.send('HeapProfiler.collectGarbage')
    const { usedSize } = await this.send('Runtime.getHeapUsage')
    const dom = await this.send('Memory.getDOMCounters')
    const probe = await this.eval('({ intervals: __perf.intervals, eventSources: __perf.eventSources })')

    return { heapMB: +(usedSize / 1048576).toFixed(1), nodes: dom.nodes, listeners: dom.jsEventListeners, ...probe }
  }

  /** Event Timing entries since `since` (ms on the page clock): input-to-next-paint durations. */
  async eventsSince(since, names) {
    const evs = await this.eval(`__perf.events.filter((e) => e.start >= ${since})`)

    return evs.filter((e) => !names || names.includes(e.name))
  }

  now() {
    return this.eval('performance.now()')
  }

  async close() {
    this.off()
    await this.cdp.send('Target.closeTarget', { targetId: this.targetId })
  }
}

async function launch() {
  const dir = mkdtempSync(join(tmpdir(), 'staqly-perf-'))
  const args = [
    '--remote-debugging-port=0',
    `--user-data-dir=${dir}`,
    '--no-first-run',
    '--no-default-browser-check',
    '--disable-extensions',
    '--disable-background-networking',
    '--disable-renderer-backgrounding',
    '--disable-background-timer-throttling',
    '--enable-precise-memory-info',
    '--js-flags=--expose-gc',
    ...(opt.headful ? [] : ['--headless=new']),
    'about:blank',
  ]
  const proc = spawn(findChrome(), args, { stdio: ['ignore', 'ignore', 'pipe'] })
  const wsUrl = await new Promise((resolve, reject) => {
    let buf = ''

    proc.stderr.on('data', (d) => {
      buf += d
      const m = buf.match(/DevTools listening on (ws:\/\/\S+)/)

      if (m) {
        resolve(m[1])
      }
    })
    proc.on('exit', () => reject(new Error(`Chrome exited: ${buf.slice(-500)}`)))
  })
  const cdp = await CDP.connect(wsUrl)

  return {
    cdp,
    async close() {
      cdp.close()
      proc.kill()
      await sleep(300)
      rmSync(dir, { recursive: true, force: true })
    },
  }
}

async function newPage(cdp, profile) {
  const { targetId } = await cdp.send('Target.createTarget', { url: 'about:blank' })
  const { sessionId } = await cdp.send('Target.attachToTarget', { targetId, flatten: true })
  const page = new Page(cdp, sessionId, targetId)
  const p = PROFILES[profile]

  await page.send('Page.enable')
  await page.send('Network.enable')
  await page.send('Runtime.enable')
  await page.send('Page.addScriptToEvaluateOnNewDocument', { source: PAGE_PROBE })
  await page.send('Emulation.setDeviceMetricsOverride', {
    width: p.width,
    height: p.height,
    deviceScaleFactor: p.scale,
    mobile: p.mobile,
  })

  if (p.mobile) {
    await page.send('Emulation.setTouchEmulationEnabled', { enabled: true, maxTouchPoints: 5 })
  }

  await page.send('Emulation.setCPUThrottlingRate', { rate: p.cpu })
  await page.send('Network.emulateNetworkConditions', {
    offline: false,
    latency: p.latency,
    downloadThroughput: (p.down * 1000) / 8,
    uploadThroughput: (p.up * 1000) / 8,
  })

  return page
}

async function goto(page, url) {
  const loaded = new Promise((resolve) => {
    const off = page.cdp.on((m) => {
      if (m.sessionId === page.sessionId && m.method === 'Page.loadEventFired') {
        off()
        resolve()
      }
    })
  })

  await page.send('Page.navigate', { url })
  await loaded
  await page.networkIdle()
}

const pct = (xs, p) => {
  if (!xs.length) {
    return 0
  }

  const s = xs.toSorted((a, b) => a - b)

  return Math.round(s[Math.min(s.length - 1, Math.floor((p / 100) * s.length))])
}

// ── Scenarios ────────────────────────────────────────────────────────────────────────────────────────

// The dashboard's sign-in: the access token traded for a session cookie (set in Chrome too, see Main).
let session = ''

async function signIn() {
  const res = await fetch(`${base}/api/v1/sessions/api-key`, {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ api_key: opt.token }),
  })
  const cookie = res.headers.getSetCookie().find((c) => c.startsWith('staqly_session='))

  if (res.status !== 201 || !cookie) {
    throw new Error(`sign-in with the access token: ${res.status} ${await res.text()}`)
  }

  session = cookie.split(';')[0].slice('staqly_session='.length)
}

const getJSON = async (path) =>
  (await fetch(`${base}/api${path}`, { headers: { Cookie: `staqly_session=${session}` } })).json()

async function resolveIds() {
  const get = getJSON
  const cols = (await get('/v1/collections')).data ?? []
  // The biggest collection (most posts) and its newest post.
  const col = cols.toSorted((a, b) => (b.post_count ?? 0) - (a.post_count ?? 0))[0]
  const posts = col ? ((await get(`/v1/posts?collection_id=${col.id}&limit=1`)).data ?? []) : []

  return { col: col?.id ?? '_', post: posts[0]?.id ?? '_', collections: cols.length }
}

async function measureLoad(browser, profile, route) {
  const page = await newPage(browser.cdp, profile)

  await page.send('Network.clearBrowserCache')
  const t0 = Date.now()

  await goto(page, base + route)
  await sleep(500)
  const m = await page.eval(`(() => {
    const nav = performance.getEntriesByType('navigation')[0]
    const fcp = performance.getEntriesByName('first-contentful-paint')[0]?.startTime ?? 0
    const tbt = __perf.longtasks.filter(([s]) => s >= fcp).reduce((n, [, d]) => n + Math.max(0, d - 50), 0)
    return { ttfb: nav.responseStart, fcp, lcp: __perf.lcp, cls: __perf.cls, shifts: __perf.shifts, tbt, longtasks: __perf.longtasks.length,
             domContentLoaded: nav.domContentLoadedEventEnd, nodes: document.getElementsByTagName('*').length }
  })()`)
  const reqs = [...page.requests.values()]
  const apiReqs = reqs.filter((r) => r.url.includes('/api/'))
  const seen = new Map()

  for (const r of apiReqs) {
    const k = `${r.method} ${r.url}`

    seen.set(k, (seen.get(k) ?? 0) + 1)
  }

  const dupes = [...seen].filter(([, n]) => n > 1)
  const heap = await page.heap()

  await page.close()

  return {
    route,
    profile,
    ttfbMs: Math.round(m.ttfb),
    fcpMs: Math.round(m.fcp),
    lcpMs: Math.round(m.lcp),
    cls: +m.cls.toFixed(3),
    tbtMs: Math.round(m.tbt),
    requests: reqs.length,
    apiRequests: apiReqs.length,
    dupApi: dupes.reduce((n, [, c]) => n + c - 1, 0),
    transferKB: +(reqs.reduce((n, r) => n + r.bytes, 0) / 1024).toFixed(1),
    apiKB: +(apiReqs.reduce((n, r) => n + r.bytes, 0) / 1024).toFixed(1),
    compressed: reqs.filter((r) => r.encoding).length,
    heapMB: heap.heapMB,
    domNodes: m.nodes,
    wallMs: Date.now() - t0,
    dupes: dupes.map(([k, n]) => `${n}x ${k.replace(base, '')}`),
    // Layout shifts: when, how much and which elements moved.
    shifts: m.shifts,
    // ms from the navigation's first request: start-end status size method path
    waterfall: reqs.map(
      (r) =>
        `${Math.round((r.start - reqs[0].start) * 1000)}-${Math.round(((r.end ?? r.start) - reqs[0].start) * 1000)}ms ` +
        `${r.status ?? '-'} ${(r.bytes / 1024).toFixed(1)}KB ${r.method} ${r.url.replace(base, '')}`,
    ),
  }
}

async function interact(browser) {
  const page = await newPage(browser.cdp, 'desktop')
  const out = {}
  const rows = '[data-testid="post-row"]'
  const ids = await resolveIds()

  await goto(page, `${base}/posts?collection=${ids.col}`)
  await page.waitFor(`document.querySelector('${rows}')`)
  out.listRows = await page.eval(`document.querySelectorAll('${rows}').length`)

  // Open the post sheet (click the first row), wait for the dialog, close it (Escape).
  let since = await page.now()
  const t = Date.now()

  await page.click(rows)
  await page.waitFor(`document.querySelector('[role=dialog]')`)
  out.openPanelMs = Date.now() - t
  await page.networkIdle(300)
  let evs = await page.eventsSince(since, ['pointerdown', 'click'])

  out.openPanelInputMs = pct(
    evs.map((e) => e.duration),
    100,
  )
  await page.key('Escape')
  await sleep(400)
  // Back on the list (closing may change the URL): reload it so "Load more" starts from one page.
  await goto(page, `${base}/posts?collection=${ids.col}`)
  await page.waitFor(`document.querySelector('${rows}')`)

  // Load 500 rows with "Load more".
  const tl = Date.now()

  for (let i = 0; i < 20; i++) {
    const n = await page.eval(`document.querySelectorAll('${rows}').length`)

    if (n >= 500) {
      break
    }

    const more = await page.eval(
      `(() => { const b = [...document.querySelectorAll('button')].find((b) => b.textContent.trim() === 'Load more'); if (!b) return false; b.click(); return true })()`,
    )

    if (!more) {
      break
    }

    await page.networkIdle(200)
  }

  out.rowsLoaded = await page.eval(`document.querySelectorAll('${rows}').length`)
  out.loadTo500Ms = Date.now() - tl

  // Command palette: Cmd/Ctrl+K, type "perf 01".
  since = await page.now()
  await page.key('k', { modifiers: process.platform === 'darwin' ? 4 : 2, keyCode: 75 })
  await page.waitFor(`document.querySelector('[cmdk-input]')`)

  for (const ch of 'perf 01') {
    await page.key(ch, { code: ch === ' ' ? 'Space' : undefined, keyCode: ch.toUpperCase().charCodeAt(0) })
    await sleep(80)
  }

  await sleep(300)
  evs = await page.eventsSince(since, ['keydown', 'keypress', 'input', 'keyup'])
  out.paletteTypeMaxMs = pct(
    evs.map((e) => e.duration),
    100,
  )
  out.paletteResults = await page.eval(`document.querySelectorAll('[cmdk-item]').length`)
  await page.key('Escape')

  // Post search: type into "Search posts" of the biggest collection.
  await goto(page, `${base}/posts?collection=${ids.col}`)
  // The search sits behind the Filter button.
  await page.waitFor(`document.querySelector('[data-testid="filter-button"]')`)
  await page.click('[data-testid="filter-button"]')
  await page.waitFor(`document.querySelector('[aria-label="Search posts"]')`)
  await page.click('[aria-label="Search posts"]')
  since = await page.now()

  for (const ch of 'launch') {
    await page.key(ch)
    await sleep(80)
  }

  await page.networkIdle(300)
  evs = await page.eventsSince(since, ['keydown', 'keypress', 'input', 'keyup'])
  out.searchTypeMaxMs = pct(
    evs.map((e) => e.duration),
    100,
  )

  // Activity feed: first render and 5x "Load older events".
  const ta = Date.now()

  await goto(page, `${base}/activity`)
  out.activityLoadMs = Date.now() - ta
  since = await page.now()
  const tp = Date.now()

  for (let i = 0; i < 5; i++) {
    const ok = await page.eval(
      `(() => { const b = [...document.querySelectorAll('button')].find((b) => b.textContent.includes('Load older')); if (!b) return false; b.click(); return true })()`,
    )

    if (!ok) {
      break
    }

    await page.networkIdle(200)
  }

  out.activityPage5Ms = Date.now() - tp
  evs = await page.eventsSince(since)
  out.activityClickMaxMs = pct(
    evs.map((e) => e.duration),
    100,
  )
  out.activityNodes = await page.eval(`document.getElementsByTagName('*').length`)
  out.sidebarLinks = await page.eval(
    `document.querySelectorAll('[data-sidebar=menu-sub-button], [data-sidebar=menu-button]').length`,
  )

  await page.close()

  return out
}

async function memory(browser) {
  const page = await newPage(browser.cdp, 'desktop')
  const routes = ['/schedule', '/posts', '/channels', '/activity', '/webhooks', '/alerts', '/integrations', '/settings']
  const entries = '[data-testid="schedule-entry"], [data-testid="schedule-empty"]'

  await goto(page, `${base}/schedule`)
  await page.waitFor(`document.querySelector('${entries}')`)

  const nav = async (href) => {
    await page.eval(
      `(() => { const a = document.querySelector('a[href="${href}"]'); if (a) { a.click(); return true } window.next?.router?.push('${href}'); return false })()`,
    )
    await page.waitFor(`location.pathname === '${href}'`)
    await page.networkIdle(150, 10_000)
  }

  // Warm-up round so lazily loaded chunks and caches don't count as growth.
  for (const r of routes) {
    await nav(r)
  }

  // Compare like with like: every snapshot is taken on the schedule, after the week has rendered.
  await nav('/schedule')
  await page.waitFor(`document.querySelector('${entries}')`)
  const before = await page.heap()
  const t = Date.now()
  // Two rounds of 100: growth that stops after the first (query and router caches filling up) is not a leak.
  const rounds = []

  for (let round = 0; round < 2; round++) {
    for (let i = 0; i < 100; i++) {
      await nav(routes[i % routes.length])
    }

    await nav('/schedule')
    await page.waitFor(`document.querySelector('${entries}')`)
    rounds.push(await page.heap())
  }

  const navMs = Math.round((Date.now() - t) / 202)
  const [afterNav, afterNav200] = rounds

  for (let i = 0; i < 30; i++) {
    // Open a post from the calendar (each entry opens its post sheet), then close it.
    await page.eval(
      `(() => { const e = document.querySelectorAll('[data-testid="schedule-entry"]'); e[${i} % e.length]?.click() })()`,
    )
    await page.waitFor(`document.querySelector('[role=dialog]')`)
    await page.key('Escape')
    await page.waitFor(`!document.querySelector('[role=dialog]')`)
  }

  await page.networkIdle(300)
  const afterPanels = await page.heap()

  await page.close()

  return { before, afterNav, afterNav200, afterPanels, avgNavMs: navMs }
}

// ── Main ─────────────────────────────────────────────────────────────────────────────────────────────

await signIn()
const browser = await launch()
const result = { url: base, date: new Date().toISOString(), load: [], interact: null, memory: null }

try {
  await browser.cdp.send('Storage.setCookies', {
    cookies: [{ name: 'staqly_session', value: session, domain: new URL(base).hostname, path: '/', httpOnly: true }],
  })
  const ids = await resolveIds()
  const routes = (opt.routes ? opt.routes.split(',') : DEFAULT_ROUTES).map((r) =>
    r.replace('{col}', ids.col).replace('{post}', ids.post),
  )
  const profiles = opt.profile === 'both' ? ['desktop', 'mobile'] : [opt.profile]

  console.log(`dashboard ${base}: ${ids.collections} collections`)

  if (only.has('load')) {
    for (const profile of profiles) {
      // One throwaway load warms the server (first SQLite reads, Go page cache).
      await measureLoad(browser, profile, routes[0])

      for (const route of routes) {
        const r = await measureLoad(browser, profile, route)

        result.load.push(r)
      }
    }

    console.table(
      result.load.map((r) => {
        const row = {
          ...r,
          route: r.route.replace(/posts\/\w+/, 'posts/:id').replace(/collection=\w+/, 'collection=:id'),
        }

        delete row.dupes
        delete row.waterfall
        delete row.shifts

        return row
      }),
    )

    for (const r of result.load.filter((x) => x.dupes.length)) {
      console.log(`duplicate API requests on ${r.route} (${r.profile}): ${r.dupes.join(', ')}`)
    }
  }

  if (only.has('interact')) {
    result.interact = await interact(browser)
    console.log('interact', result.interact)
  }

  if (only.has('memory')) {
    result.memory = await memory(browser)
    console.log('memory', JSON.stringify(result.memory, null, 2))
  }
} finally {
  await browser.close()
}

if (opt.json) {
  writeFileSync(opt.json, JSON.stringify(result, null, 2))
  console.log(`wrote ${opt.json}`)
}
