import type { Browser, Page } from '@playwright/test'

import { expect, ready, test } from '../../lib/fixtures.js'
import { state } from '../../lib/state.js'

// Accounts with email codes, end to end in real browsers: sign up, sign in again, a second workspace, an
// invited editor in a second browser, what an editor and a viewer may not do, and workspace isolation.
// The test server runs with AUTH_DEV_SIGNIN, which shows the last code per address to tests
// (GET /v1/auth/email/dev-code); real servers mail it.
test.use({ storageState: { cookies: [], origins: [] } })
test.describe.configure({ mode: 'serial' })

const run = Date.now().toString(36)
const addr = (who: string) => `${who}-${run}@example.com`

async function devCode(email: string): Promise<string> {
  let code = ''

  await expect
    .poll(async () => {
      const res = await fetch(`${state().cloud.api}/v1/auth/email/dev-code?email=${encodeURIComponent(email)}`)

      code = res.ok ? ((await res.json()) as { code: string }).code : ''

      return code
    })
    .toMatch(/^\d{6}$/)

  return code
}

/** Signs in on /login with an email code; setupToken only for a new account (the test server needs one). */
async function signInWithCode(page: Page, email: string, setupToken = '') {
  await page.goto('/login')
  await ready(page, 'Sign in to Staqly')

  if (setupToken) {
    await page.getByLabel('Setup token (only for a new account)').fill(setupToken)
  }

  await page.getByRole('textbox', { name: 'Email address' }).fill(email)
  await page.getByTestId('send-code').click()
  await ready(page, 'Check your email')
  await page.getByRole('textbox', { name: 'Code' }).fill(await devCode(email))
}

/** A new person meets the welcome wizard first (05-welcome covers it): skip it to reach the calendar. */
async function pastWelcome(page: Page) {
  const heading = page.getByRole('heading', { level: 1, name: /^(Schedule|Welcome to Staqly)$/ })

  await expect(heading).toBeVisible()

  if ((await heading.textContent()) === 'Welcome to Staqly') {
    await page.getByRole('button', { name: 'Skip for now' }).click()
  }

  await ready(page, 'Schedule')
}

/** The REST API as the signed-in browser (session cookie and its CSRF token). */
async function asBrowser(page: Page) {
  const s = await (await page.request.get('/api/v1/session')).json()
  const h = { 'X-CSRF-Token': s.csrf_token as string }

  return {
    session: s as { workspace: { id: string; name: string }; role: string },
    get: (path: string) => page.request.get(`/api/v1${path}`),
    post: (path: string, data: unknown) => page.request.post(`/api/v1${path}`, { data, headers: h }),
  }
}

/** The path of an invitation link (the link names the server's public address). */
function invitePath(link: string | undefined): string {
  const u = new URL(link ?? '')

  return u.pathname + u.search
}

async function newBrowser(browser: Browser) {
  const ctx = await browser.newContext({ storageState: { cookies: [], origins: [] } })

  return { ctx, page: await ctx.newPage() }
}

test('email codes: sign-up, workspaces, invitations, roles and isolation', async ({ page, browser }) => {
  test.setTimeout(120_000)
  const owner = addr('owner')
  const editor = addr('editor')
  const viewer = addr('viewer')
  const operator = addr('operator')

  // 1. Sign-up: a new address gets an account and a first workspace.
  await signInWithCode(page, owner, state().cloud.setupToken)
  const signedUpAt = Date.now()

  await pastWelcome(page)
  await expect(page.getByTestId('user-name').first()).toHaveText(owner)
  let me = await asBrowser(page)

  expect(me.session.role).toBe('owner')
  const wsA = me.session.workspace

  // A post in the first workspace.
  const postA = await me.post('/posts', { collection: 'Default', title: 'Only in A', content: `Only in A ${run}` })

  expect(postA.status()).toBe(201)
  const postAId = ((await postA.json()) as { id: string }).id

  // 2. The account page lists the sign-in method.
  await page.goto('/settings/account')
  await expect(page.getByTestId('sign-in-methods')).toContainText(owner)
  // The only way to sign in cannot be removed: no Remove until there is another.
  await expect(page.getByTestId('sign-in-methods').getByRole('button', { name: 'Remove' })).toHaveCount(0)

  // 3. Invite an editor, viewer and operator to the first workspace (Members page).
  const links: Record<string, string> = {}

  for (const [email, role] of [
    [editor, 'Editor'],
    [viewer, 'Viewer'],
    [operator, 'Operator'],
  ] as const) {
    await page.goto('/settings/members')
    await ready(page, 'Settings')
    await page.getByRole('button', { name: 'Invite people' }).click()
    const sheet = page.getByRole('dialog', { name: 'Invite people' })

    await sheet.getByRole('textbox', { name: 'Email' }).fill(email)
    await sheet.getByRole('combobox', { name: 'Role' }).selectOption({ label: role })
    await sheet.getByRole('button', { name: 'Send invitation' }).click()
    links[email] = await page.getByRole('textbox', { name: 'Invitation link' }).inputValue()
    await page.keyboard.press('Escape')
  }

  // 4. A second workspace: it opens right away and is empty of the first one's posts.
  await page.getByTestId('workspace-menu-trigger').first().click()
  await page.getByRole('menuitem', { name: 'New workspace' }).click()
  await page.getByRole('textbox', { name: 'Name' }).fill(`B ${run}`)
  await page.getByRole('button', { name: 'Create workspace' }).click()
  await ready(page, 'Schedule')
  await expect(page.getByTestId('workspace-menu-trigger').first()).toContainText(`B ${run}`)
  me = await asBrowser(page)
  expect(me.session.workspace.id).not.toBe(wsA.id)
  const listB = await (await me.get('/posts?limit=100')).json()

  expect(JSON.stringify(listB)).not.toContain(`Only in A ${run}`)
  expect((await me.get(`/posts/${postAId}`)).status()).toBe(404)
  await page.goto('/posts')
  await expect(page.getByRole('main')).not.toContainText(`Only in A ${run}`)

  // 5. The editor accepts in a second browser with a code sent to the invited address.
  const ed = await newBrowser(browser)

  await ed.page.goto(invitePath(links[editor]))
  await ready(ed.page, `Join ${wsA.name}`)
  await ed.page.getByTestId('accept-with-code').click()
  await ready(ed.page, 'Check your email')
  await ed.page.getByRole('textbox', { name: 'Code' }).fill(await devCode(editor))
  await pastWelcome(ed.page)
  const edApi = await asBrowser(ed.page)

  expect(edApi.session.workspace.id).toBe(wsA.id)
  expect(edApi.session.role).toBe('editor')
  // Sees the workspace's posts, writes and publishes; cannot manage people.
  expect(JSON.stringify(await (await edApi.get('/posts?limit=100')).json())).toContain(`Only in A ${run}`)
  expect(
    (
      await edApi.post('/posts', { collection: 'Default', title: 'By the editor', content: `By the editor ${run}` })
    ).status(),
  ).toBe(201)
  expect((await edApi.get('/members')).status()).toBe(403)
  expect((await edApi.post('/invitations', { email: addr('x'), role: 'viewer' })).status()).toBe(403)
  await ed.page.goto('/settings')
  await expect(ed.page.getByRole('link', { name: 'Members' })).toHaveCount(0)
  await expect(ed.page.getByRole('link', { name: 'Account' })).toBeVisible()
  // The editor never sees the owner's second workspace.
  expect(JSON.stringify(await (await edApi.get('/workspaces')).json())).not.toContain(`B ${run}`)
  await ed.ctx.close()

  // 6. The viewer reads but cannot write or publish.
  const vw = await newBrowser(browser)

  await vw.page.goto(invitePath(links[viewer]))
  await vw.page.getByTestId('accept-with-code').click()
  await vw.page.getByRole('textbox', { name: 'Code' }).fill(await devCode(viewer))
  await pastWelcome(vw.page)
  const vwApi = await asBrowser(vw.page)

  expect(vwApi.session.role).toBe('viewer')
  expect((await vwApi.get(`/posts/${postAId}`)).status()).toBe(200)
  expect((await vwApi.post('/posts', { collection: 'Default', title: 'Nope', content: 'nope' })).status()).toBe(403)
  expect((await vwApi.post(`/posts/${postAId}/publish`, {})).status()).toBe(403)
  await vw.ctx.close()

  // Operators accept invitations, connect channels and create integration keys without administration.
  const op = await newBrowser(browser)

  await op.page.goto(invitePath(links[operator]))
  await op.page.getByTestId('accept-with-code').click()
  await op.page.getByRole('textbox', { name: 'Code' }).fill(await devCode(operator))
  await pastWelcome(op.page)
  const opApi = await asBrowser(op.page)

  expect(opApi.session.role).toBe('operator')
  await op.page.goto('/channels')
  await op.page.getByTestId('add-channel').click()
  await op.page.getByTestId('provider-custom').click()
  const connect = op.page.getByRole('dialog', { name: 'Connect Your own automation' })

  await connect.getByRole('textbox', { name: /^Name/ }).fill(`Operator channel ${run}`)
  await connect.getByRole('combobox', { name: /How posts reach your automation/ }).click()
  await op.page.getByRole('option', { name: 'Let my agents and webhooks pick it up' }).click()
  await connect.getByTestId('connect-channel').click()
  await expect(connect).toBeHidden()
  await expect(op.page.getByText(/is connected/)).toBeVisible()
  await op.page.goto('/integrations/api-keys?new=1')
  const keyForm = op.page.getByRole('dialog', { name: 'Create API key' })

  await keyForm.getByRole('textbox', { name: 'Name' }).fill('Operator integration')
  await keyForm.getByRole('button', { name: /^Full access/ }).click()
  await keyForm.getByRole('button', { name: 'Create key' }).click()
  await expect(op.page.getByRole('dialog', { name: 'Your new API key' })).toBeVisible()
  await op.page.getByRole('button', { name: "I've copied it" }).click()
  const keys = await (await opApi.get('/api-keys')).json()

  expect(keys.data).toHaveLength(1)
  expect(keys.data[0].scopes).toContain('channels:manage')
  expect(keys.data[0].scopes).not.toContain('admin')
  expect((await opApi.get('/agents')).status()).toBe(200)
  expect((await opApi.get('/alerts')).status()).toBe(200)
  expect((await opApi.get('/webhooks')).status()).toBe(200)
  expect((await opApi.get('/members')).status()).toBe(403)
  expect((await opApi.get('/billing/details')).status()).toBe(403)
  await op.page.goto('/integrations?view=agents')
  await expect(op.page.getByTestId('agents')).toBeVisible()
  await expect(op.page.getByRole('link', { name: 'Alerts', exact: true })).toBeVisible()
  await expect(op.page.getByRole('link', { name: 'Webhooks', exact: true })).toBeVisible()
  await op.ctx.close()

  // 7. Sign out and back in with a new code: the same person, a new session, no second account.
  await page.getByTestId('user-menu-trigger').first().click()
  await page.getByRole('menuitem', { name: 'Sign out' }).click()
  await expect(page).toHaveURL(/\/login/)
  await page.waitForTimeout(Math.max(0, 31_000 - (Date.now() - signedUpAt))) // one code per address every 30 s
  await signInWithCode(page, owner)
  await ready(page, 'Schedule')
  const again = await asBrowser(page)
  const mine = (await (await again.get('/workspaces')).json()) as { data: { name: string }[] }

  expect(mine.data.map((w) => w.name).toSorted()).toEqual([wsA.name, `B ${run}`].toSorted())
})

test('a wrong code is explained and Use another address goes back', async ({ page }) => {
  const email = addr('wrong')

  await page.goto('/login')
  await page.getByRole('textbox', { name: 'Email address' }).fill(email)
  await page.getByTestId('send-code').click()
  await ready(page, 'Check your email')
  const right = await devCode(email)

  await page.getByRole('textbox', { name: 'Code' }).fill(right === '000000' ? '111111' : '000000')
  await expect(page.getByText(/That code is not right\. 4 tries left\./)).toBeVisible()
  await expect(page.getByTestId('resend-code')).toBeDisabled()
  await page.getByRole('button', { name: 'Use another address' }).click()
  await expect(page.getByRole('textbox', { name: 'Email address' })).toBeVisible()
})

test('the email sign-in address changes with a code to the current address, then one to the new', async ({ page }) => {
  const before = addr('mover')
  const after = addr('moved')

  await signInWithCode(page, before, state().cloud.setupToken)
  await pastWelcome(page)
  const me = (await asBrowser(page)).session
  const signInCode = await devCode(before)

  await page.goto('/settings/account')
  // One address per person: the card offers to change it, not to add another.
  await expect(page.getByRole('button', { name: 'Add email address' })).toHaveCount(0)
  await page.getByTestId('change-email').click()
  const sheet = page.getByRole('dialog', { name: 'Change email address' })

  await sheet.getByRole('button', { name: 'Send code' }).click()
  // The code box keeps the last code per address: wait for the new one.
  let current = ''

  await expect
    .poll(async () => {
      current = await devCode(before)

      return current !== signInCode
    })
    .toBe(true)
  await sheet.getByRole('textbox', { name: 'Code' }).fill(current)
  await sheet.getByRole('textbox', { name: 'New email address' }).fill(after)
  await sheet.getByTestId('send-code').click()
  await sheet.getByRole('textbox', { name: 'Code' }).fill(await devCode(after))
  await expect(sheet).toBeHidden()
  await expect(page.getByTestId('sign-in-methods')).toContainText(after)
  await expect(page.getByTestId('sign-in-methods')).not.toContainText(before)

  // The new address signs the same person in.
  await page.getByTestId('user-menu-trigger').first().click()
  await page.getByRole('menuitem', { name: 'Sign out' }).click()
  await expect(page).toHaveURL(/\/login/)
  await signInWithCode(page, after)
  await ready(page, 'Schedule')
  expect((await asBrowser(page)).session.workspace.id).toBe(me.workspace.id)
})
