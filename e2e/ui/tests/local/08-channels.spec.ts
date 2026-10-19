import type { Page } from '@playwright/test'

import { expect, ready, test, uniq } from '../../lib/fixtures.js'

const row = (page: Page, name: string) => page.getByTestId('channel-row').filter({ hasText: name })

test.describe('channels', () => {
  test('the list groups channels by platform with their status; disabled and connected are told apart', async ({
    page,
    srv,
  }) => {
    await page.goto('/channels')
    await ready(page, 'Channels')
    const custom = page.getByTestId('channels-custom')

    expect(await custom.getByTestId('channel-row').count()).toBeGreaterThanOrEqual(4)
    await expect(row(page, 'Company blog')).toContainText('Connected')
    await expect(row(page, 'Old account')).toContainText('Disabled')
    await expect(page.getByRole('region', { name: 'Your own automation' })).toBeVisible()
    expect(srv.seed.channels.blog).toBeTruthy()
  })

  test('Add channel: platforms in groups, the ones on the way greyed with Soon; connect your own automation', async ({
    page,
    api,
  }) => {
    const name = uniq('UI automation')

    await page.goto('/channels')
    await page.getByTestId('add-channel').click()
    const sheet = page.getByRole('dialog', { name: 'Add channel' })

    await expect(sheet.getByRole('region', { name: 'Your own' })).toBeVisible()
    await expect(sheet.getByRole('region', { name: 'Social networks' })).toBeVisible()
    const soon = sheet.getByRole('button', { name: /, coming soon$/ })

    await expect(soon.first()).toBeDisabled()
    await expect(soon.first()).toContainText('Soon')
    expect(await soon.count()).toBeGreaterThan(5)
    await sheet.getByTestId('provider-custom').click()
    // The sheet's title (and so its accessible name) becomes the platform's.
    const connect = page.getByRole('dialog', { name: 'Connect Your own automation' })

    await expect(connect).toBeVisible()
    await connect.getByRole('button', { name: 'Back to platforms' }).click()
    await expect(sheet.getByTestId('provider-custom')).toBeVisible()
    await sheet.getByTestId('provider-custom').click()
    await connect.getByRole('textbox', { name: /^Name/ }).fill(name)
    await connect.getByTestId('connect-channel').click()
    await expect(connect).toBeHidden()
    await expect(page.getByText(/is connected/)).toBeVisible()
    await expect(row(page, name)).toContainText('Connected')
    const created = (await api.get<{ data: { name: string; provider: string }[] }>('/v1/channels')).data.find(
      (c) => c.name === name,
    )

    expect(created?.provider).toBe('custom')
  })

  test('rename, test the connection, disable and enable, remove', async ({ page, api }) => {
    const channel = await api.channel(uniq('UI channel'))
    const renamed = `${channel.name} renamed`

    await page.goto('/channels')
    const r = row(page, channel.name)

    await r.getByRole('button', { name: `Actions for ${channel.name}` }).click()
    await page.getByRole('menuitem', { name: 'Rename' }).click()
    const dialog = page.getByRole('dialog', { name: 'Rename channel' })

    await expect(dialog.getByRole('button', { name: 'Save' })).toBeDisabled()
    await dialog.getByRole('textbox', { name: 'Name' }).fill(renamed)
    await dialog.getByRole('button', { name: 'Save' }).click()
    await expect(dialog).toBeHidden()
    const rr = row(page, renamed)

    await expect(rr).toBeVisible()
    await rr.getByRole('button', { name: `Actions for ${renamed}` }).click()
    await page.getByRole('menuitem', { name: 'Test connection' }).click()
    await expect(page.getByText(/works|connected|ok/i).first()).toBeVisible()

    await rr.getByRole('button', { name: `Actions for ${renamed}` }).click()
    await page.getByRole('menuitem', { name: 'Disable' }).click()
    await expect(rr).toContainText('Disabled')
    await expect
      .poll(async () => (await api.get<{ status: string }>(`/v1/channels/${channel.id}`)).status)
      .toBe('disabled')
    await rr.getByRole('button', { name: `Actions for ${renamed}` }).click()
    await page.getByRole('menuitem', { name: 'Enable' }).click()
    await expect(rr).toContainText('Connected')

    await rr.getByRole('button', { name: `Actions for ${renamed}` }).click()
    await page.getByRole('menuitem', { name: 'Remove' }).click()
    const confirm = page.getByRole('alertdialog')

    await expect(confirm).toContainText(`Remove ${renamed}?`)
    await confirm.getByRole('button', { name: 'Remove' }).click()
    await expect(rr).toHaveCount(0)
    expect((await api.raw('GET', `/v1/channels/${channel.id}`)).status).toBe(404)
  })
})
