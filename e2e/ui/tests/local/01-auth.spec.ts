import { expect, ready, test } from '../../lib/fixtures.js'
import { connectLink, tokenLink } from '../../lib/stack.js'
import { state } from '../../lib/state.js'

// A browser that never signed in: every browser of a local install signs in once with its access token.
test.use({ storageState: { cookies: [], origins: [] } })

test.describe('local sign-in', () => {
  test('a new browser is asked for the access token; a wrong one is refused, the right one signs it in', async ({
    page,
  }) => {
    const s = state()

    await page.goto('/schedule')
    // Not the calendar: the sign-in, with where to find the token.
    await expect(page.getByRole('heading', { level: 1, name: 'Schedule' })).toHaveCount(0)
    await expect(page.getByRole('heading', { level: 1, name: 'Sign in to Staqly' })).toBeVisible()
    await expect(page.getByTestId('local-sign-in')).toContainText('staqly serve token')
    await expect(page.getByTestId('local-sign-in')).toContainText('docker exec staqly server token')

    await page.getByLabel(/Access token/).fill('stk_local_thisisnotthetokenofthiscomputer')
    await page.getByRole('button', { name: 'Sign in' }).click()
    await expect(page.getByText(/not the access token of this computer/)).toBeVisible()

    await page.getByLabel(/Access token/).fill(s.local.token)
    await page.getByRole('button', { name: 'Sign in' }).click()
    await ready(page, 'Schedule')
    // A reload stays signed in (a session cookie, not the token).
    await page.reload()
    await ready(page, 'Schedule')
  })

  test('the link printed at start signs the browser in, keeps the page and drops the token from the address bar', async ({
    page,
  }) => {
    await page.goto(tokenLink(state().local, '/channels'))
    await ready(page, 'Channels')
    await expect(page).not.toHaveURL(/#|stk_local_/)

    // The root link (what the server prints) opens the calendar.
    await page.context().clearCookies()
    await page.goto(tokenLink(state().local))
    await ready(page, 'Schedule')
    await expect(page).not.toHaveURL(/stk_local_/)
  })

  test('a one-time connect link signs another browser in and cannot be reused', async ({ page, browser }) => {
    const s = state()
    const link = await connectLink(s.bin, { ...s.local, mode: 'local' })

    await page.goto(link)
    await ready(page, 'Schedule')
    expect(page.url()).not.toContain('#')

    const other = await browser.newContext({ storageState: { cookies: [], origins: [] } })
    const p2 = await other.newPage()

    await p2.goto(link)
    await expect(p2.getByRole('heading', { level: 1, name: 'This link was already used' })).toBeVisible()
    await expect(p2.getByRole('heading', { level: 1, name: 'Schedule' })).toHaveCount(0)
    await other.close()
  })

  test('a malformed connect link explains itself', async ({ page }) => {
    await page.goto('/connect#not-a-real-token')
    await expect(page.getByRole('heading', { level: 1, name: "This link isn't valid" })).toBeVisible()
    // The token never stays in the address bar.
    expect(page.url()).not.toContain('not-a-real-token')
  })
})
