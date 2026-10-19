import { expect, ready, test, uniq } from '../../lib/fixtures.js'
import { state } from '../../lib/state.js'

// A browser without a session.
test.use({ storageState: { cookies: [], origins: [] } })

/** A fresh cloud workspace (each from its own documentation address: signups are limited per IP). */
async function signup(name = uniq('UI ws')) {
  const s = state().cloud
  const res = await fetch(`${s.api}/v1/workspaces`, {
    method: 'POST',
    headers: {
      'Content-Type': 'application/json',
      'X-Forwarded-For': `198.51.100.${1 + Math.floor(Math.random() * 250)}`,
    },
    body: JSON.stringify({ name, setup_token: s.setupToken }),
  })

  expect(res.status).toBe(201)

  return (await res.json()) as { api_key: { key: string }; recovery_key: string; workspace: { name: string } }
}

test.describe('cloud sign-in', () => {
  test('a deep link goes to sign-in and comes back; wrong, agent and admin keys', async ({ page }) => {
    const s = state().cloud

    await page.goto('/webhooks')
    await expect(page).toHaveURL(/\/login\?next=%2Fwebhooks/)
    await ready(page, 'Sign in with your API key')
    const key = page.getByRole('textbox', { name: 'API key' })

    await page.getByRole('button', { name: 'Sign in' }).click()
    await expect(key).toHaveAttribute('aria-invalid', 'true')
    await expect(key).toBeFocused()

    await key.fill('stk_wrongwrongwrongwrongwrong')
    await page.getByRole('button', { name: 'Sign in' }).click()
    await expect(page.getByText(/invalid|not valid|unknown|wrong/i).first()).toBeVisible()
    await expect(key).toHaveAttribute('aria-invalid', 'true')

    // An agent key (no admin scope) is refused with the reason.
    await key.fill(s.agentKey)
    await page.getByRole('button', { name: 'Sign in' }).click()
    await expect(page.getByText(/admin/i).first()).toBeVisible()
    await expect(page).toHaveURL(/\/login/)

    await key.fill(s.adminKey)
    await page.getByRole('button', { name: 'Sign in' }).click()
    await ready(page, 'Webhooks')
    await expect(page.getByRole('button', { name: 'Workspace menu' })).toContainText('Acme content')
  })

  test('log out from the workspace menu ends the session', async ({ page }) => {
    const s = state().cloud

    await page.goto('/login')
    await page.getByRole('textbox', { name: 'API key' }).fill(s.adminKey)
    await page.getByRole('button', { name: 'Sign in' }).click()
    await ready(page, 'Schedule')
    await page.getByRole('button', { name: 'Workspace menu' }).click()
    await page.getByRole('menuitem', { name: 'Log out' }).click()
    await expect(page).toHaveURL(/\/login/)
    await page.goto('/settings')
    await expect(page).toHaveURL(/\/login\?next=%2Fsettings/)
  })

  test('recovery: a wrong key is refused, the right one shows new keys once and signs in', async ({ page }) => {
    const ws = await signup()

    await page.goto('/login')
    await page.getByRole('button', { name: 'Lost your key?' }).click()
    await ready(page, 'Recover your workspace')
    const box = page.getByRole('textbox', { name: /Recovery key/ })

    await box.fill('staqly-recovery-nope')
    await page
      .getByRole('button', { name: /Recover|Sign in/ })
      .first()
      .click()
    await expect(box).toHaveAttribute('aria-invalid', 'true')
    await box.fill(ws.recovery_key)
    await page
      .getByRole('button', { name: /Recover|Sign in/ })
      .first()
      .click()
    await ready(page, 'Save your new keys')
    await expect(page.getByRole('alert').filter({ hasText: 'no longer works' })).toBeVisible()

    // Open dashboard needs the acknowledgement first.
    await page.getByRole('button', { name: 'Open dashboard' }).click()
    await expect(page.getByRole('checkbox', { name: /I have saved my new API key/ })).toHaveAttribute(
      'aria-invalid',
      'true',
    )
    await page.getByRole('checkbox', { name: /I have saved my new API key/ }).check()
    await page.getByRole('button', { name: 'Open dashboard' }).click()
    await ready(page, 'Schedule')
    await expect(page.getByRole('button', { name: 'Workspace menu' })).toContainText(ws.workspace.name)

    // The used recovery key is gone.
    const again = await fetch(`${state().cloud.api}/v1/sessions/recover`, {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ recovery_key: ws.recovery_key }),
    })

    expect(again.status).toBe(401)
  })

  test('setup: a wrong setup token is refused under its field; the right one creates a workspace', async ({ page }) => {
    const s = state().cloud
    const name = uniq('Created in browser')

    await page.goto('/setup')
    await ready(page, 'Create your free workspace')
    await page.getByRole('textbox', { name: 'Workspace name' }).fill(name)
    await page.getByRole('textbox', { name: 'Setup token' }).fill('wrong-token')
    await page.getByRole('button', { name: 'Create free workspace' }).click()
    await expect(page.getByRole('textbox', { name: 'Setup token' })).toHaveAttribute('aria-invalid', 'true')
    await page.getByRole('textbox', { name: 'Setup token' }).fill(s.setupToken)
    await page.getByRole('button', { name: 'Create free workspace' }).click()
    await ready(page, 'Save your keys')
    await page.getByRole('checkbox', { name: /saved/ }).check()
    await page.getByRole('button', { name: 'Open dashboard' }).click()
    await ready(page, 'Schedule')
    await expect(page.getByRole('button', { name: 'Workspace menu' })).toContainText(name)
  })
})
