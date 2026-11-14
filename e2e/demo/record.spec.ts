// The README's demo GIFs (assets/screenshots/schedule-a-post-{light,dark}.gif): John Doe connects X, writes a post
// on the calendar, schedules it, publishes it and sees it on X. Recorded against `staqly serve` built from this
// checkout with X's endpoints pointed at a stand-in on this machine (-ldflags), so nothing reaches x.com: the
// consent page, the account and the post page on "x.com" are made up here.
//
//   make dashboard                                # the export the binary embeds
//   pnpm --filter @staqly/e2e demo:gif            # both GIFs, about 3 minutes (needs ffmpeg)
import { execFile } from 'node:child_process'
import { existsSync, mkdtempSync, rmSync } from 'node:fs'
import { createServer, type IncomingMessage, type Server, type ServerResponse } from 'node:http'
import type { AddressInfo } from 'node:net'
import { tmpdir } from 'node:os'
import { join } from 'node:path'
import { promisify } from 'node:util'

import { chromium, expect, test, type Page } from '@playwright/test'

import { dayKey, daysAhead } from '../ui/lib/data.js'
import { ROOT, run, startServer, tokenLink, type RunningServer } from '../ui/lib/stack.js'

const exec = promisify(execFile)
const PROVIDERS = 'github.com/staqly/staqly/apps/api/internal/providers'
const POST_TEXT = 'Scheduling just got easier: plan a week of posts in one go, and they go out on time ✨'

// ---- a stand-in for X ----

interface FakeX {
  url: string
  posts: Map<string, string>
  close: () => void
}

const avatar = `<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 96 96"><rect width="96" height="96" fill="#3b82f6"/>
<text x="48" y="60" font-family="Helvetica,Arial,sans-serif" font-size="38" font-weight="700" fill="#fff" text-anchor="middle">JD</text></svg>`

function consentPage(redirect: string, state: string): string {
  const back = `${redirect}${redirect.includes('?') ? '&' : '?'}state=${encodeURIComponent(state)}&code=demo-code`

  return `<!doctype html><html><head><meta charset="utf-8"><title>Authorize app</title><style>
body{margin:0;min-height:100vh;display:grid;place-items:center;background:#000;color:#e7e9ea;font-family:-apple-system,Helvetica,Arial,sans-serif}
.card{width:420px;padding:36px;border:1px solid #2f3336;border-radius:16px}.logo{font-size:34px;font-weight:800;text-align:center}
h1{font-size:22px;margin:22px 0 8px}p{color:#71767b;font-size:15px;line-height:1.4}
.who{display:flex;gap:12px;align-items:center;margin:20px 0;padding:12px;border:1px solid #2f3336;border-radius:12px}
.who img{width:40px;height:40px;border-radius:50%}.who b{display:block}.who span{color:#71767b}
a.btn{display:block;text-align:center;margin-top:24px;padding:14px;border-radius:999px;background:#eff3f4;color:#0f1419;font-weight:700;text-decoration:none}
</style></head><body><div class="card"><div class="logo">𝕏</div><h1>staqly wants to access your X account</h1>
<p>It will be able to read your profile and post on your behalf.</p>
<div class="who"><img src="/avatar_normal.svg" alt=""><div><b>John Doe</b><span>@johndoe</span></div></div>
<a class="btn" href="${back}" id="authorize">Authorize app</a></div></body></html>`
}

function postPage(text: string, dark: boolean): string {
  const bg = dark ? '#000' : '#fff'
  const fg = dark ? '#e7e9ea' : '#0f1419'
  const muted = '#71767b'
  const line = dark ? '#2f3336' : '#eff3f4'
  const now = new Date()
  const when = `${now.toLocaleTimeString('en-US', { hour: 'numeric', minute: '2-digit' })} · ${now.toLocaleDateString('en-US', { month: 'short', day: 'numeric', year: 'numeric' })}`

  return `<!doctype html><html><head><meta charset="utf-8"><title>John Doe on X</title><style>
body{margin:0;background:${bg};color:${fg};font-family:-apple-system,Helvetica,Arial,sans-serif}
main{width:600px;margin:0 auto;min-height:100vh;border-left:1px solid ${line};border-right:1px solid ${line}}
header{padding:14px 16px;font-size:20px;font-weight:700;border-bottom:1px solid ${line}}
article{padding:16px}.who{display:flex;gap:12px;align-items:center}.who img{width:44px;height:44px;border-radius:50%}
.who b{display:block;font-size:15px}.who span{color:${muted};font-size:15px}
.text{font-size:22px;line-height:1.35;margin:14px 0}.time{color:${muted};font-size:15px;padding-bottom:14px;border-bottom:1px solid ${line}}
.stats{display:flex;gap:22px;padding:14px 0;color:${muted};font-size:15px;border-bottom:1px solid ${line}}.stats b{color:${fg}}
.via{margin-top:14px;color:${muted};font-size:13px}
</style></head><body><main><header>← Post</header><article>
<div class="who"><img src="/avatar_400x400.svg" alt=""><div><b>John Doe</b><span>@johndoe</span></div></div>
<div class="text">${text.replace(/&/g, '&amp;').replace(/</g, '&lt;')}</div><div class="time">${when} · <b style="color:${fg}">1</b> View</div>
<div class="stats"><span><b>0</b> Reposts</span><span><b>0</b> Likes</span><span><b>0</b> Bookmarks</span></div>
<div class="via">Posted with staqly</div></article></main></body></html>`
}

const body = (req: IncomingMessage) =>
  new Promise<string>((r) => {
    let s = ''

    req.on('data', (c) => (s += c))
    req.on('end', () => r(s))
  })

function json(res: ServerResponse, status: number, v: unknown) {
  res.writeHead(status, { 'Content-Type': 'application/json' })
  res.end(JSON.stringify(v))
}

async function startFakeX(): Promise<FakeX> {
  const posts = new Map<string, string>()
  let fake = ''
  const server: Server = createServer((req, res) => {
    const u = new URL(req.url ?? '/', fake)

    void (async () => {
      if (u.pathname === '/i/oauth2/authorize') {
        res.writeHead(200, { 'Content-Type': 'text/html; charset=utf-8' })
        res.end(consentPage(u.searchParams.get('redirect_uri') ?? '', u.searchParams.get('state') ?? ''))
      } else if (u.pathname.startsWith('/avatar')) {
        res.writeHead(200, { 'Content-Type': 'image/svg+xml' })
        res.end(avatar)
      } else if (u.pathname === '/2/oauth2/token') {
        await body(req)
        json(res, 200, {
          token_type: 'bearer',
          access_token: 'demo-access',
          refresh_token: 'demo-refresh',
          expires_in: 7200,
          scope: 'tweet.read tweet.write users.read offline.access media.write',
        })
      } else if (u.pathname === '/2/users/me') {
        json(res, 200, {
          data: {
            id: '1849000000000000001',
            name: 'John Doe',
            username: 'johndoe',
            profile_image_url: `${fake}/avatar_normal.svg`,
          },
        })
      } else if (u.pathname === '/2/tweets' && req.method === 'POST') {
        const id = String(1850000000000000000n + BigInt(posts.size + 1))
        const text = (JSON.parse((await body(req)) || '{}') as { text?: string }).text ?? ''

        posts.set(id, text)
        json(res, 201, { data: { id, text } })
      } else if (u.pathname.startsWith('/2/tweets/')) {
        json(res, 200, {
          data: {
            id: u.pathname.split('/').pop(),
            public_metrics: { like_count: 0, retweet_count: 0, reply_count: 0, impression_count: 1 },
          },
        })
      } else {
        json(res, 404, { title: 'Not Found' })
      }
    })()
  })

  await new Promise<void>((r) => server.listen(0, '127.0.0.1', () => r()))
  fake = `http://127.0.0.1:${(server.address() as AddressInfo).port}`

  return { url: fake, posts, close: () => server.close() }
}

// ---- the recording ----

const pause = (page: Page, ms = 900) => page.waitForTimeout(ms)

async function record(scheme: 'light' | 'dark') {
  if (!existsSync(join(ROOT, 'apps/api/web/index.html'))) {
    throw new Error('apps/api/web has no dashboard export: run `make dashboard` first')
  }

  const fake = await startFakeX()
  const bin = join(mkdtempSync(join(tmpdir(), 'staqly-demo-bin-')), 'staqly')

  await run(
    'go',
    [
      'build',
      '-ldflags',
      `-X ${PROVIDERS}.xAPI=${fake.url} -X ${PROVIDERS}.xAuthorize=${fake.url}/i/oauth2/authorize`,
      '-o',
      bin,
      './cmd/staqly',
    ],
    { cwd: join(ROOT, 'apps/cli') },
  )
  const server: RunningServer = await startServer(bin, {
    mode: 'local',
    port: 62333,
    dashboardPort: 62080,
    env: {
      LOCAL_USER_NAME: 'John Doe',
      PROVIDER_X_CLIENT_ID: 'demo',
      PROVIDER_X_CLIENT_SECRET: 'demo',
      LOG_LEVEL: 'warn',
    },
  })
  const videos = mkdtempSync(join(tmpdir(), 'staqly-demo-video-'))
  const browser = await chromium.launch()

  try {
    await fetch(`${server.api}/v1/collections`, {
      method: 'POST',
      headers: { Authorization: `Bearer ${server.token}`, 'Content-Type': 'application/json' },
      body: JSON.stringify({ name: 'Product updates' }),
    })
    const size = { width: 1280, height: 800 }
    const context = await browser.newContext({
      viewport: size,
      colorScheme: scheme,
      recordVideo: { dir: videos, size },
    })

    // The dashboard starts dark (next-themes defaultTheme), whatever the system says.
    await context.addInitScript((t) => localStorage.setItem('theme', t), scheme)
    const page = await context.newPage()

    // The post's page on "x.com" is the stand-in's.
    await page.route('https://x.com/**', (route) => {
      if (new URL(route.request().url()).pathname.startsWith('/avatar')) {
        return route.fulfill({ contentType: 'image/svg+xml', body: avatar })
      }

      const id = new URL(route.request().url()).pathname.split('/').pop() ?? ''

      return route.fulfill({
        contentType: 'text/html; charset=utf-8',
        body: postPage(fake.posts.get(id) ?? '', scheme === 'dark'),
      })
    })

    console.log(`[demo ${scheme}] connect X`)
    await page.goto(tokenLink(server, '/channels'))
    await expect(page.getByRole('heading', { level: 1, name: 'Channels' })).toBeVisible()
    await pause(page)
    await page.getByTestId('add-channel').click()
    const sheet = page.getByRole('dialog', { name: 'Add channel' })

    await pause(page, 700)
    await sheet.getByTestId('provider-x').click()
    await pause(page, 700)
    await page.getByTestId('oauth-start').click() // the sheet is titled after the platform now
    await expect(page.locator('#authorize')).toBeVisible()
    await pause(page, 1200)
    await page.locator('#authorize').click()
    await expect(page.getByText('John Doe').first()).toBeVisible({ timeout: 15_000 })
    await pause(page, 1500)

    console.log(`[demo ${scheme}] schedule`)
    await page.getByRole('link', { name: 'Schedule' }).first().click()
    await expect(page.getByTestId('schedule-grid')).toBeVisible()
    const tomorrow = daysAhead(1, 10)

    for (let i = 0; i < 2 && (await page.getByTestId(`schedule-slot-${dayKey(tomorrow)}-10`).count()) === 0; i++) {
      await page.getByRole('button', { name: 'Next week' }).click()
    }

    const slot = page.getByTestId(`schedule-slot-${dayKey(tomorrow)}-10`)

    await slot.scrollIntoViewIfNeeded()
    await slot.hover()
    await pause(page, 600)
    await slot.getByTestId('slot-add').focus()
    await page.keyboard.press('Enter')
    await page.getByRole('dialog').getByRole('button', { name: 'New post for this time' }).click()
    const composer = page.getByRole('dialog', { name: 'New post' })

    await pause(page, 600)
    await composer.getByRole('combobox', { name: 'Collection' }).click()
    await page.getByRole('option', { name: 'Product updates' }).click()
    await expect(page.getByRole('listbox')).toHaveCount(0)
    await pause(page, 500)
    // Not pickChannels: the option's name starts with the avatar's initials (JD), so its anchored match misses it.
    await composer.getByRole('button', { name: 'Channels' }).click()
    await page.getByRole('option', { name: /John Doe/ }).click()
    await page.keyboard.press('Escape')
    await composer.getByTestId('post-content').click()
    await page.keyboard.type(POST_TEXT, { delay: 28 })
    await pause(page, 900)
    // Schedule opens the calendar with the slot's time picked; its own Schedule sends the post.
    await composer.getByTestId('date-time-picker').click()
    await page.getByRole('dialog', { name: 'Pick a date and time' }).getByRole('button', { name: 'Schedule' }).click()
    await expect(composer).toBeHidden()
    const entry = slot.getByTestId('schedule-entry').first()

    await expect(entry).toBeVisible()
    await pause(page, 1500)

    console.log(`[demo ${scheme}] publish`)
    await entry.click()
    const post = page.getByRole('dialog')

    await pause(page, 900)
    await post.getByTestId('publish-now').click()
    // The composer closes; the post goes out after the 7 s Undo window.
    await expect.poll(() => fake.posts.size, { timeout: 40_000 }).toBe(1)
    await pause(page, 2500)
    const [id] = [...fake.posts.keys()]

    await page.goto(`https://x.com/johndoe/status/${id}`)
    await expect(page.getByText(POST_TEXT)).toBeVisible()
    await pause(page, 3500)

    console.log(`[demo ${scheme}] video`)
    const video = await page.video()!.path()

    await context.close()
    const out = join(ROOT, `assets/screenshots/schedule-a-post-${scheme}.gif`)

    // 10 frames a second, 900 px wide, one palette for the whole clip.
    await exec(
      'ffmpeg',
      [
        '-y',
        '-i',
        video,
        '-vf',
        'fps=10,scale=900:-1:flags=lanczos,split[a][b];[a]palettegen=stats_mode=diff[p];[b][p]paletteuse=dither=bayer:bayer_scale=5:diff_mode=rectangle',
        out,
      ],
      { maxBuffer: 1 << 26 },
    )
  } finally {
    await browser.close()
    await server.stop()
    fake.close()
    rmSync(videos, { recursive: true, force: true })
  }
}

test('demo GIF, light', async () => {
  await record('light')
})

test('demo GIF, dark', async () => {
  await record('dark')
})
