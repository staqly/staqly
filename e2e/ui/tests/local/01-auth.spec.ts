import { expect, ready, test } from '../../lib/fixtures.js'
import { connectLink } from '../../lib/stack.js'
import { state } from '../../lib/state.js'

// A browser that never signed in (the dashboard is exposed, as in the Docker image).
test.use({ storageState: { cookies: [], origins: [] } })

test.describe('local sign-in', () => {
  test('a new browser is asked to connect, a one-time link signs it in, the link cannot be reused', async ({
    page,
    browser,
  }) => {
    const s = state()

    await page.goto('/schedule')
    // Not the calendar: an explanation of how to connect this device.
    await expect(page.getByRole('heading', { level: 1, name: 'Schedule' })).toHaveCount(0)
    await expect(page.getByRole('heading', { level: 1, name: 'Sign in this browser' })).toBeVisible()
    await expect(page.getByRole('main')).toContainText('staqly serve connect-link')
    await expect(page.getByRole('main')).toContainText('docker exec staqly server connect-link')

    const link = await connectLink(s.bin, { ...s.local, mode: 'local' })

    await page.goto(link)
    await ready(page, 'Schedule')
    // The token was in the fragment and is gone from the address bar.
    expect(page.url()).not.toContain('#')

    // The same link in another browser: refused with a way forward.
    const other = await browser.newContext({ storageState: { cookies: [], origins: [] } })
    const p2 = await other.newPage()

    await p2.goto(link)
    await expect(p2.getByRole('heading', { level: 1, name: 'This link was already used' })).toBeVisible()
    await expect(p2.getByRole('heading', { level: 1, name: 'Schedule' })).toHaveCount(0)
    await other.close()
  })

  test('a malformed link explains itself', async ({ page }) => {
    await page.goto('/connect#not-a-real-token')
    await expect(page.getByRole('heading', { level: 1, name: "This link isn't valid" })).toBeVisible()
    // The token never stays in the address bar.
    expect(page.url()).not.toContain('not-a-real-token')
  })
})
