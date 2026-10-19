import { expect, ready, test } from '../../lib/fixtures.js'
import { state } from '../../lib/state.js'

// The welcome wizard of a brand-new account: it opens on its own after the first sign-in, walks through the
// person's name, their goal, the workspace's name, a first channel and an agent key, and never comes back on its
// own once finished (the user menu reopens it).
test.use({ storageState: { cookies: [], origins: [] } })

const email = `welcome-${Date.now().toString(36)}@example.com`

test('welcome: a new account goes through the wizard once', async ({ page }) => {
  const s = state().cloud
  const start = await page.request.post('/api/v1/auth/email/start', { data: { email } })

  expect(start.status()).toBe(202)
  let code = ''

  await expect
    .poll(async () => {
      const res = await fetch(`${s.api}/v1/auth/email/dev-code?email=${encodeURIComponent(email)}`)

      code = res.ok ? ((await res.json()) as { code: string }).code : ''

      return code
    })
    .toMatch(/^\d{6}$/)
  const verify = await page.request.post('/api/v1/auth/email/verify', {
    data: { email, code, setup_token: s.setupToken },
  })

  expect(verify.status()).toBe(201)

  // The dashboard's home sends a new person to the welcome.
  await page.goto('/')
  await expect(page).toHaveURL(/\/welcome/)
  await ready(page, 'Welcome to Staqly')
  await page.getByLabel('What should we call you?').fill('Wanda Welcome')
  await page.getByText('Automate with agents').click()
  await page.getByRole('button', { name: /Continue/ }).click()

  await ready(page, 'Name your workspace')
  await page.getByLabel(/Workspace name/).fill('Wanda Studio')
  await page.getByRole('button', { name: /Continue/ }).click()

  // A first channel inline: your own automation, posts sent to an endpoint of yours.
  await ready(page, 'Connect your first channel')
  await page.getByRole('button', { name: 'Your own automation' }).click()
  await page.getByRole('textbox', { name: /^Name/ }).fill('My n8n')
  await page.getByPlaceholder('https://n8n.example.com/webhook/staqly').fill('https://n8n.example.com/webhook/staqly')
  await page.getByTestId('connect-channel').click()
  await expect(page.getByRole('list', { name: 'Connected channels' })).toContainText('My n8n')
  await page.getByRole('button', { name: /Continue/ }).click()

  // The agents goal puts the agent first; its key lands in the command.
  await ready(page, 'Connect an agent')
  await page.getByRole('button', { name: 'Create a key for it' }).click()
  await expect(page.getByText(/Bearer stk_/)).toBeVisible()
  await page.getByRole('button', { name: /Continue/ }).click()

  // Inviting the team is its own step after the agent, and can be skipped.
  await ready(page, 'Invite your team')
  await page.getByRole('button', { name: 'Skip', exact: true }).click()

  await ready(page, 'You are all set')
  await expect(page.getByText('1 channel connected')).toBeVisible()
  await page.getByRole('button', { name: /Go to the calendar/ }).click()
  await ready(page, 'Schedule')
  await expect(page.getByTestId('user-name').first()).toHaveText('Wanda Welcome')
  await expect(page.getByTestId('workspace-name').first()).toHaveText('Wanda Studio')

  // Done means done: home stays home, and the user menu no longer offers it.
  await page.goto('/')
  await ready(page, 'Schedule')
  await page.getByTestId('user-menu-trigger').first().click()
  await expect(page.getByRole('menu')).toBeVisible()
  await expect(page.getByRole('menuitem', { name: /Getting started/ })).toHaveCount(0)
})
