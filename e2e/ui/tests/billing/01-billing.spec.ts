import { execFile } from 'node:child_process'
import { mkdtempSync, readFileSync } from 'node:fs'
import { tmpdir } from 'node:os'
import { join } from 'node:path'

import type { Browser, Page } from '@playwright/test'

import { expect, ready, test } from '../../lib/fixtures.js'
import { state } from '../../lib/state.js'

// Billing end to end: the real binary in cloud mode on PostgreSQL, a real browser, and the Stripe stand-in of
// lib/stripe.ts in Stripe's place (its Checkout page pays at once; the spec plays Stripe's later news: a failed
// renewal, retries that ran out). One workspace goes from no plan through the trial, a paid plan, an add-on, a
// second seat, a failed renewal and the locked screen back to paid, then buys X posts and is deleted.
test.use({ storageState: { cookies: [], origins: [] } })

const s = () => state().billing

/** The code mailed to an address: the test server shows it (AUTH_DEV_SIGNIN). */
async function devCode(email: string): Promise<string> {
  let code = ''

  await expect
    .poll(async () => {
      const res = await fetch(`${s().api}/v1/auth/email/dev-code?email=${encodeURIComponent(email)}`)

      code = res.ok ? ((await res.json()) as { code: string }).code : ''

      return code
    })
    .toMatch(/^\d{6}$/)

  return code
}

/** A new account with an email code, over the API (the sign-up page itself: cloud/04-accounts). */
async function signUp(page: Page, email: string) {
  expect((await page.request.post('/api/v1/auth/email/start', { data: { email } })).status()).toBe(202)
  const res = await page.request.post('/api/v1/auth/email/verify', {
    data: { email, code: await devCode(email), setup_token: s().setupToken },
  })

  expect(res.status()).toBe(201)
}

interface Billing {
  plan: string
  subscription_status: string | null
  suspended: boolean
  seats: number
  trial: string
  ended_for: string | null
}

/** The REST API as the signed-in browser (session cookie and its CSRF token). */
async function asBrowser(page: Page) {
  const session = (await (await page.request.get('/api/v1/session')).json()) as {
    csrf_token: string
    workspace: { id: string }
  }
  const headers = { 'X-CSRF-Token': session.csrf_token }

  return {
    workspace: session.workspace.id,
    get: async <T>(path: string) => (await (await page.request.get(`/api/v1${path}`)).json()) as T,
    billing: async () => (await (await page.request.get('/api/v1/billing')).json()) as Billing,
    post: (path: string, data?: unknown) => page.request.post(`/api/v1${path}`, { data, headers }),
    del: (path: string, data?: unknown) => page.request.delete(`/api/v1${path}`, { data, headers }),
  }
}

interface StripeSub {
  id: string
  status: string
  metadata: { workspace_id: string }
  items: { data: { id: string; quantity: number; price: { lookup_key: string } }[] }
}

/** What the stand-in holds: the calls staqly made, the ones it did not know, the subscriptions. */
const stripe = async () =>
  (await (await fetch(`${s().stripe}/_test/state`)).json()) as {
    calls: string[]
    unknown: string[]
    subscriptions: StripeSub[]
  }

/** The workspace's subscriptions at Stripe, newest last. */
const subsOf = async (ws: string) => (await stripe()).subscriptions.filter((x) => x.metadata.workspace_id === ws)

/** Stripe changes a subscription and tells staqly; answers the webhook's status. */
async function stripeSays(sub: string, patch: Record<string, unknown>, event?: string): Promise<number> {
  const res = await fetch(`${s().stripe}/_test/subscriptions/${sub}`, {
    method: 'POST',
    body: JSON.stringify({ patch, event }),
  })

  return ((await res.json()) as { status: number }).status
}

async function newBrowser(browser: Browser) {
  const ctx = await browser.newContext({ storageState: { cookies: [], origins: [] }, baseURL: s().dash })

  return { ctx, page: await ctx.newPage() }
}

const status = (page: Page) => page.getByTestId('plan-status')

test('a workspace: no plan, trial, paid, add-on, second seat, failed renewal, locked, paid again, X posts, deleted', async ({
  page,
  browser,
}) => {
  test.setTimeout(180_000)
  const logged = readFileSync(s().log, 'utf8').length
  // New people on every run and every repeat.
  const run = Date.now().toString(36)
  const owner = `owner-${run}@example.com`
  const editor = `editor-${run}@example.com`

  await signUp(page, owner)
  const me = await asBrowser(page)

  await test.step('no plan yet: the workspace is closed and its owner is sent to the welcome, whose last step is the plan', async () => {
    expect(await me.billing()).toMatchObject({ subscription_status: null, suspended: true, trial: 'available' })
    expect((await me.post('/posts', { collection: 'Default', title: 'Too early', content: 'x' })).status()).toBe(402)

    await page.goto('/schedule')
    await ready(page, 'Welcome to Staqly')
    await page.getByLabel('What should we call you?').fill('Olga Owner')
    await page.getByRole('button', { name: /Continue/ }).click()
    await ready(page, 'Name your workspace')
    await page.getByLabel(/Workspace name/).fill('Paying Studio')
    await page.getByRole('button', { name: /Continue/ }).click()
    await ready(page, 'Where do you post?')
    await page.getByRole('button', { name: /Continue/ }).click()
    await ready(page, 'Connect an agent')
    await page.getByRole('button', { name: 'Not now' }).click()
    await ready(page, 'Invite your team')
    await page.getByRole('button', { name: 'Skip', exact: true }).click()
    await ready(page, 'Choose a plan')
  })

  await test.step('trial: Starter through Stripe Checkout, free for 7 days', async () => {
    await page.getByTestId('welcome-plan-starter').click()
    await page.getByTestId('start-trial').click()
    // Checkout (the stand-in) pays and sends the browser back to the welcome's last step.
    await ready(page, 'You are all set')
    await expect(page).toHaveURL(/checkout=success/)
    expect(await me.billing()).toMatchObject({ plan: 'starter', subscription_status: 'trialing', suspended: false })
    // The workspace is open now.
    await page.getByRole('button', { name: /Go to the calendar/ }).click()
    await ready(page, 'Schedule')

    await page.goto('/settings/plan')
    await expect(page.getByRole('heading', { name: 'Starter plan' })).toBeVisible()
    await expect(status(page)).toHaveText('Trial · 7 days left')
    await expect(page.getByTestId('plan-message')).toContainText('Then the card on file is charged')
    // Add-ons come with a paid plan: not in the trial, in the browser or over the API.
    await expect(page.getByTestId('addons').getByRole('button', { name: 'Add' })).toHaveAttribute(
      'aria-disabled',
      'true',
    )
    const early = await me.post('/billing/addons/ai_pack')

    expect(early.status()).toBe(409)
    expect(((await early.json()) as { error: { details: { reason: string } } }).error.details.reason).toBe('trialing')
    expect((await me.post('/posts', { collection: 'Default', title: 'In the trial', content: 'x' })).status()).toBe(201)
  })

  await test.step('paid: leaving the trial for Plus ends it and charges the card', async () => {
    await page.goto('/plans')
    await ready(page, 'Choose your plan')
    await expect(page.getByTestId('plan-charge-plus')).toContainText('Your trial ends now and Plus is charged today')
    await page.getByRole('button', { name: 'Upgrade to Plus' }).click()
    await expect(page).toHaveURL(/\/settings\/plan/)
    await expect(page.getByRole('heading', { name: 'Plus plan' })).toBeVisible()
    await expect(status(page)).toHaveText('Active')
    expect(await me.billing()).toMatchObject({ plan: 'plus', subscription_status: 'active', trial: 'none' })
  })

  await test.step('add-on: bought on the card on file, shown as active', async () => {
    const addons = page.getByTestId('addons')

    await addons.getByRole('button', { name: 'Add' }).click()
    await page.getByRole('alertdialog').getByRole('button', { name: 'Add' }).click()
    await expect(page.getByText('AI pack added')).toBeVisible()
    await expect(addons.getByText('Active')).toBeVisible()
    const [sub] = await subsOf(me.workspace)

    expect(sub!.items.data.map((it) => it.price.lookup_key)).toEqual([
      'staqly_plus_month',
      'staqly_addon_ai_pack_month',
    ])
    // Twice is refused.
    expect((await me.post('/billing/addons/ai_pack')).status()).toBe(409)
  })

  const ed = await newBrowser(browser)

  await test.step('a second person joins: Stripe bills two seats', async () => {
    const inv = await me.post('/invitations', { email: editor, role: 'editor' })

    expect(inv.status()).toBe(201)
    const link = new URL(((await inv.json()) as { link: string }).link)

    await ed.page.goto(link.pathname + link.search)
    await ready(ed.page, 'Join Paying Studio')
    await ed.page.getByTestId('accept-with-code').click()
    await ready(ed.page, 'Check your email')
    await ed.page.getByRole('textbox', { name: 'Code' }).fill(await devCode(editor))
    // Someone who joins meets a short welcome of their own.
    await ready(ed.page, 'Welcome to Staqly')
    await ed.page.getByRole('button', { name: 'Skip for now' }).click()
    await ready(ed.page, 'Schedule')
    await expect.poll(async () => (await me.billing()).seats).toBe(2)
    // The seat sync is a job: the plan's item follows, the add-on stays at one.
    await expect.poll(async () => (await subsOf(me.workspace))[0]!.items.data.map((it) => it.quantity)).toEqual([2, 1])
  })

  await test.step("paying is a person's: an API key reads billing and is refused every billing write", async () => {
    const made = await me.post('/api-keys', { name: 'Admin key', scopes: ['admin'] })

    expect(made.status()).toBe(201)
    const key = ((await made.json()) as { key: string }).key
    const call = (method: string, path: string, body?: unknown) =>
      fetch(`${s().api}/v1${path}`, {
        method,
        headers: { Authorization: `Bearer ${key}`, 'Content-Type': 'application/json' },
        body: body ? JSON.stringify(body) : undefined,
      })

    expect((await call('GET', '/billing')).status).toBe(200)
    const before = (await stripe()).calls.length

    for (const [method, path, body] of [
      ['POST', '/billing/checkout', { plan: 'business' }],
      ['POST', '/billing/portal'],
      ['POST', '/billing/start-paid-plan'],
      ['POST', '/billing/credits/checkout', { provider: 'x', posts: 200 }],
      ['POST', '/billing/addons/ai_pack'],
      ['DELETE', '/billing/addons/ai_pack'],
    ] as const) {
      const res = await call(method, path, body)

      expect(res.status, `${method} ${path}`).toBe(403)
    }

    // The CLI holds a key too: `staqly billing status` reads, `checkout` and `portal` say who may pay.
    const home = mkdtempSync(join(tmpdir(), 'staqly-ui-cli-'))
    const cli = (...args: string[]) =>
      new Promise<{ code: number; out: string; err: string }>((resolve) => {
        execFile(
          state().bin,
          args,
          {
            env: {
              PATH: process.env.PATH,
              HOME: home,
              STAQLY_CONFIG: join(home, 'config.json'),
              STAQLY_API_URL: s().api,
              STAQLY_API_KEY: key,
              NO_COLOR: '1',
            },
          },
          (err, out, stderr) => resolve({ code: err ? Number(err.code ?? 1) : 0, out, err: stderr }),
        )
      })

    expect(await cli('billing', 'status')).toMatchObject({
      code: 0,
      out: expect.stringMatching(/Subscription:\s+active/),
    })

    for (const command of ['checkout', 'portal']) {
      const res = await cli('billing', command)

      expect(res.code, command).not.toBe(0)
      expect(res.err, command).toContain('billing is managed by a signed-in person')
      expect(res.out, command).not.toContain('http')
    }

    // Refused before Stripe is asked anything.
    expect((await stripe()).calls.slice(before).filter((c) => !c.startsWith('GET '))).toEqual([])
    expect(await me.billing()).toMatchObject({ plan: 'plus', subscription_status: 'active' })
  })

  await test.step('a forged Stripe event changes nothing', async () => {
    const [sub] = await subsOf(me.workspace)
    const res = await fetch(`${s().stripe}/_test/event`, {
      method: 'POST',
      body: JSON.stringify({
        type: 'customer.subscription.deleted',
        object: { ...sub, status: 'canceled' },
        secret: 'whsec_forged',
      }),
    })

    expect(((await res.json()) as { status: number }).status).toBe(400)
    expect(await me.billing()).toMatchObject({ plan: 'plus', subscription_status: 'active', suspended: false })
  })

  await test.step('the renewal fails: a notice on every page, the workspace still works', async () => {
    const [sub] = await subsOf(me.workspace)

    expect(
      await stripeSays(sub!.id, {
        status: 'past_due',
        latest_invoice: { id: 'in_renewal', status: 'open', billing_reason: 'subscription_cycle' },
      }),
    ).toBe(200)
    await page.goto('/schedule')
    await ready(page, 'Schedule')
    await expect(page.getByTestId('past-due-notice')).toContainText(
      'The last payment for this workspace failed. Update the payment method to keep it open.',
    )
    await page.getByTestId('past-due-notice').getByRole('link', { name: 'Plan & usage' }).click()
    await expect(status(page)).toHaveText('Payment failed')
    await expect(page.getByTestId('plan-message')).toContainText('The last payment failed. Update the payment method')
    expect(await me.billing()).toMatchObject({ subscription_status: 'past_due', suspended: false })
    expect((await me.post('/posts', { collection: 'Default', title: 'Still open', content: 'x' })).status()).toBe(201)
    // The editor is not told about money.
    await ed.page.goto('/schedule')
    await ready(ed.page, 'Schedule')
    await expect(ed.page.getByTestId('past-due-notice')).toHaveCount(0)
  })

  await test.step('Stripe gives up: the workspace is locked for everyone, nothing is written, the data stays', async () => {
    const [sub] = await subsOf(me.workspace)

    expect(
      await stripeSays(
        sub!.id,
        { status: 'canceled', cancellation_details: { reason: 'payment_failed' } },
        'customer.subscription.deleted',
      ),
    ).toBe(200)
    expect(await me.billing()).toMatchObject({
      subscription_status: 'canceled',
      suspended: true,
      ended_for: 'payment_failed',
    })

    await page.goto('/schedule')
    const locked = page.getByTestId('plan-locked')

    await expect(locked.getByRole('heading', { name: 'This workspace is locked' })).toBeVisible()
    await expect(locked).toContainText('The payment for its plan failed, so the plan has ended. Choose a plan')
    await expect(locked.getByRole('link', { name: 'Export your data' })).toBeVisible()
    await expect(page.getByTestId('past-due-notice')).toHaveCount(0)
    // Someone who cannot pay is told who can.
    await ed.page.goto('/schedule')
    await expect(ed.page.getByTestId('plan-locked')).toContainText(
      'An owner or admin can open it again in Plan & usage.',
    )
    await expect(ed.page.getByTestId('plan-locked').getByRole('link')).toHaveCount(0)

    expect((await me.post('/posts', { collection: 'Default', title: 'Locked out', content: 'x' })).status()).toBe(402)
    expect((await me.get<{ data: unknown[] }>('/posts')).data).toHaveLength(2)
    // Plan & usage stays open and says the same.
    await page.goto('/settings/plan')
    await expect(status(page)).toHaveText('Locked')
    await expect(page.getByTestId('plan-message')).toContainText(
      'The payment for your plan failed, so the plan ended and this workspace is locked.',
    )
    await expect(page.getByTestId('addons').getByRole('button').first()).toHaveAttribute('aria-disabled', 'true')
  })

  await test.step('paid again: a new plan opens the workspace, for both seats', async () => {
    await page.goto('/schedule')
    await page.getByTestId('plan-locked').getByRole('link', { name: 'Choose a plan' }).click()
    await ready(page, 'Choose your plan')
    // The trial was used: Starter is charged from the first day.
    await expect(page.getByTestId('plan-charge-starter')).toContainText('this workspace already had its free trial')
    await page.getByRole('button', { name: 'Choose Plus' }).click()
    await expect(page).toHaveURL(/\/settings\/plan/)
    await expect(status(page)).toHaveText('Active')
    expect(await me.billing()).toMatchObject({
      plan: 'plus',
      subscription_status: 'active',
      suspended: false,
      ended_for: null,
    })
    const subs = await subsOf(me.workspace)

    expect(subs.map((x) => x.status)).toEqual(['canceled', 'active'])
    expect(subs[1]!.items.data.map((it) => it.quantity)).toEqual([2])
    await page.goto('/schedule')
    await ready(page, 'Schedule')
    await expect(page.getByTestId('plan-locked')).toHaveCount(0)
    expect((await me.post('/posts', { collection: 'Default', title: 'Open again', content: 'x' })).status()).toBe(201)
    await ed.page.goto('/schedule')
    await ready(ed.page, 'Schedule')
  })

  await test.step('a lower plan: switched on the spot', async () => {
    await page.goto('/plans')
    await ready(page, 'Choose your plan')
    await page.getByRole('button', { name: 'Switch to Starter' }).click()
    await expect(page).toHaveURL(/\/settings\/plan/)
    await expect(page.getByRole('heading', { name: 'Starter plan' })).toBeVisible()
    await expect(status(page)).toHaveText('Active')
  })

  await test.step('extra X posts: a one-time payment adds them', async () => {
    await page.goto('/settings/plan')
    await page.getByTestId('buy-x-posts').click()
    await page.getByTestId('bundle-200').click()
    await page.getByTestId('buy-x-posts-continue').click()
    await expect(page).toHaveURL(/credits=success/)
    await expect(page.getByText('Payment received')).toBeVisible()
    await expect(page.getByTestId('x-posts-line')).toContainText('200')
    // Not the editor's to buy.
    const edApi = await asBrowser(ed.page)

    expect((await edApi.post('/billing/credits/checkout', { provider: 'x', posts: 200 })).status()).toBe(403)
    expect((await edApi.post('/billing/checkout', { plan: 'pro' })).status()).toBe(403)
  })

  await test.step('deleting the workspace: only with its exact name; it signs everyone out and cancels the subscription', async () => {
    await page.goto('/settings/data')
    await page.getByTestId('danger-zone').getByRole('button', { name: 'Delete workspace...' }).click()
    const dialog = page.getByRole('dialog')

    await expect(dialog.getByRole('heading')).toHaveText('Delete “Paying Studio” forever?')
    await dialog.getByLabel('Type the workspace name to confirm').fill('Paying Studi')
    await dialog.getByRole('button', { name: 'Delete workspace' }).click()
    await expect(dialog).toBeVisible()
    expect((await me.billing()).plan).toBe('starter') // still there
    // The server refuses a wrong name too.
    expect((await me.del('/workspace', { confirm: 'paying studio' })).status()).toBe(422)

    await dialog.getByLabel('Type the workspace name to confirm').fill('Paying Studio')
    await dialog.getByRole('button', { name: 'Delete workspace' }).click()
    await expect(page).toHaveURL(/\/login/)
    expect((await page.request.get('/api/v1/billing')).status()).toBe(401)
    expect((await ed.page.request.get('/api/v1/posts')).status()).toBe(401)
    await expect.poll(async () => (await subsOf(me.workspace)).map((x) => x.status)).toEqual(['canceled', 'canceled'])
  })

  // Every Stripe call the product made is one the stand-in knows, and the server met nothing it could not handle.
  expect((await stripe()).unknown).toEqual([])
  expect(
    readFileSync(s().log, 'utf8')
      .slice(logged)
      .split('\n')
      .filter((l) => l.includes('level=ERROR')),
  ).toEqual([])
  await ed.ctx.close()
})
