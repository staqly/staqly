import { apiKeySignIn, expect, ready, test } from '../../lib/fixtures.js'
import { state } from '../../lib/state.js'

// People sign up and sign in with Google, an email code or a passkey; the dashboard never offers an API key or a
// recovery key (agents keep API keys). The test server runs with AUTH_DEV_SIGNIN: the last code per address is
// readable at GET /v1/auth/email/dev-code, and the suite's own sessions come from API keys over the API.
test.use({ storageState: { cookies: [], origins: [] } })

const run = Date.now().toString(36)

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

test.describe('cloud sign-up and sign-in', () => {
  test('/setup moves to the sign-up page: email code or Google, no API key, no workspace form', async ({ page }) => {
    await page.goto('/setup?plan=pro&interval=year')
    await expect(page).toHaveURL(/\/signup\?plan=pro&interval=year/)
    await ready(page, 'Create your account')
    await expect(page.getByText(/API key|recovery key/i)).toHaveCount(0)
    await expect(page.getByRole('textbox', { name: /Workspace name/ })).toHaveCount(0)
    await expect(page.getByRole('link', { name: 'Sign in' })).toHaveAttribute('href', '/login?plan=pro&interval=year')
  })

  test('a new address signs up with a code and lands in the welcome', async ({ page }) => {
    const email = `signup-${run}@example.com`

    await page.goto('/signup')
    await ready(page, 'Create your account')
    // The test server takes new accounts with its setup token only.
    await page.getByLabel('Setup token').fill(state().cloud.setupToken)
    await page.getByRole('textbox', { name: 'Email address' }).fill(email)
    await page.getByTestId('send-code').click()
    await ready(page, 'Check your email')
    await page.getByRole('textbox', { name: 'Code' }).fill(await devCode(email))
    await expect(page).toHaveURL(/\/welcome/)
  })

  test('a deep link goes to sign-in, which offers no API key and links to sign-up', async ({ page }) => {
    await page.goto('/webhooks')
    await expect(page).toHaveURL(/\/login\?next=%2Fwebhooks/)
    await ready(page, 'Sign in to Staqly')
    await expect(page.getByText(/API key|Lost your key/i)).toHaveCount(0)
    await expect(page.getByRole('link', { name: 'Create an account' })).toHaveAttribute(
      'href',
      '/signup?next=%2Fwebhooks',
    )
  })

  test('log out from the workspace menu ends the session', async ({ page }) => {
    await apiKeySignIn(page, state().cloud.adminKey)
    await ready(page, 'Schedule')
    await page.getByRole('button', { name: 'Workspace menu' }).click()
    await expect(page.getByRole('menuitem', { name: /Create another workspace|Sign in to another/ })).toHaveCount(0)
    await page.getByRole('menuitem', { name: 'Log out' }).click()
    await expect(page).toHaveURL(/\/login/)
    await page.goto('/settings')
    await expect(page).toHaveURL(/\/login\?next=%2Fsettings/)
  })
})
