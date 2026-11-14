import { expect, ready, test } from '../../lib/fixtures.js'

// The e2e server has no token platform to connect and no OAuth app keys, so connecting itself is covered by the
// Go tests (TestChannelLinks); here: making a link, the page someone without access sees, and revoking it.
test.describe('connect links', () => {
  test('the team makes a link; a fresh browser opens its page; revoked, it stops working', async ({
    page,
    browser,
  }) => {
    await page.goto('/channels')
    await ready(page, 'Channels')
    await page.getByRole('button', { name: 'Invite someone to connect a channel' }).click()
    const dialog = page.getByTestId('connect-link-dialog')

    await dialog.getByLabel('Note for them').fill("For ACME's Instagram")
    await dialog.getByRole('button', { name: 'Make link' }).click()
    const url = await dialog.getByTestId('connect-link-url').inputValue()

    expect(url).toMatch(/\/add-channel#[a-z2-7]{20,}$/)
    await expect(dialog.getByTestId('connect-links')).toContainText("For ACME's Instagram")

    // Someone without a session: the workspace's name and the note, nothing of the dashboard.
    const guest = await browser.newContext()
    const them = await guest.newPage()

    await them.goto(url)
    await expect(them.getByRole('heading', { name: /Connect an account to/ })).toBeVisible()
    await expect(them.getByText("For ACME's Instagram")).toBeVisible()
    await expect(them.getByRole('navigation', { name: 'Main' })).toHaveCount(0)

    // Revoked: the page says so.
    await dialog
      .getByRole('button', { name: /Revoke the link/ })
      .first()
      .click()
    await expect(dialog.getByTestId('connect-links')).toHaveCount(0)
    await them.reload()
    await expect(them.getByRole('heading', { name: "This link can't be used" })).toBeVisible()
    await guest.close()
  })
})
