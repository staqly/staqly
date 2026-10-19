import type { Page } from '@playwright/test'

import { freshCollection } from '../../lib/data.js'
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
    // A name is required: nothing on the other side names your own automation.
    await connect.getByTestId('connect-channel').click()
    await expect(connect.getByText('Name is required')).toBeVisible()
    await connect.getByRole('textbox', { name: /^Name/ }).fill(name)
    // The default delivery calls an endpoint, so its URL is required; the agent delivery needs none.
    await expect(connect.getByRole('textbox', { name: /^Endpoint URL/ })).toBeVisible()
    await connect.getByRole('combobox', { name: /How posts reach your automation/ }).click()
    await page.getByRole('option', { name: 'Let my agents and webhooks pick it up' }).click()
    await expect(connect.getByRole('textbox', { name: /^Endpoint URL/ })).toHaveCount(0)
    await connect.getByTestId('connect-channel').click()
    await expect(connect).toBeHidden()
    await expect(page.getByText(/is connected/)).toBeVisible()
    await expect(row(page, name)).toContainText('Connected')
    const created = (await api.get<{ data: { name: string; provider: string }[] }>('/v1/channels')).data.find(
      (c) => c.name === name,
    )

    expect(created?.provider).toBe('custom')
  })

  test('settings (name, links), test the connection, disable and enable, remove', async ({ page, api }) => {
    const channel = await api.channel(uniq('UI channel'))
    const renamed = `${channel.name} renamed`

    await page.goto('/channels')
    const r = row(page, channel.name)

    await r.getByRole('button', { name: `Actions for ${channel.name}` }).click()
    await page.getByRole('menuitem', { name: 'Settings' }).click()
    const dialog = page.getByRole('dialog', { name: 'Channel settings' })

    await expect(dialog.getByRole('button', { name: 'Save changes' })).toBeDisabled()
    await dialog.getByRole('textbox', { name: 'Name' }).fill(renamed)
    await dialog.getByRole('combobox', { name: 'Links in posts' }).click()
    await page.getByRole('option', { name: 'Show links as plain text' }).click()
    await dialog.getByRole('button', { name: 'Save changes' }).click()
    await expect(dialog).toBeHidden()
    await expect.poll(async () => (await api.get<{ links: string }>(`/v1/channels/${channel.id}`)).links).toBe('defang')
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

  test('a row shows the last post; groups: create, move a channel there, filter, delete (back to Default)', async ({
    page,
    api,
  }) => {
    const col = await freshCollection(api, { posts: 1 })
    const channel = col.channel
    const groupName = uniq('Client')

    await api.publishThrough(col.posts[0]!.id, 'https://example.com/posts/channel-activity')
    await page.goto('/channels')
    await ready(page, 'Channels')
    const r = row(page, channel.name)

    await expect(r.getByTestId('channel-activity')).toContainText('Last post')
    await expect(r.getByTestId('channel-activity')).toContainText('1 post in 30 days')
    await expect(r.getByRole('link', { name: /ago|now/ })).toHaveAttribute('href', `/posts/${col.posts[0]!.id}`)

    // A new group (a modal), then the channel moves there from its row menu.
    await page.getByRole('button', { name: 'New group' }).click()
    const dialog = page.getByRole('dialog', { name: 'New group' })

    await dialog.getByRole('textbox', { name: /Name/ }).fill(groupName)
    await dialog.getByRole('button', { name: 'Create group' }).click()
    await expect(dialog).toBeHidden()
    const section = page.getByTestId('channel-group').filter({ hasText: groupName })

    await expect(section).toContainText('No channels in this group')
    await r.getByRole('button', { name: `Actions for ${channel.name}` }).click()
    await page.getByRole('menuitem', { name: 'Move to group' }).click()
    // By keyboard: the group's "created" toast can sit over the submenu at the bottom right.
    await page.getByRole('menuitem', { name: groupName }).press('Enter')
    await expect(section.getByTestId('channel-row').filter({ hasText: channel.name })).toBeVisible()
    const groups = (await api.get<{ data: { id: string; name: string }[] }>('/v1/channel-groups')).data
    const created = groups.find((g) => g.name === groupName)!

    await expect
      .poll(async () => (await api.get<{ group_id: string }>(`/v1/channels/${channel.id}`)).group_id)
      .toBe(created.id)

    // The filter keeps one group.
    await page.getByRole('combobox', { name: 'Group filter' }).click()
    await page.getByRole('option', { name: groupName }).click()
    await expect(page.getByTestId('channel-group')).toHaveCount(1)
    await page.getByRole('combobox', { name: 'Group filter' }).click()
    await page.getByRole('option', { name: 'All groups' }).click()

    // Deleting the group (after a confirmation) moves the channel back to Default; Default has no Delete.
    await page.getByRole('button', { name: 'Actions for the group Default' }).click()
    await expect(page.getByRole('menuitem', { name: 'Delete group' })).toHaveCount(0)
    await page.keyboard.press('Escape')
    await page.getByRole('button', { name: `Actions for the group ${groupName}` }).click()
    await page.getByRole('menuitem', { name: 'Delete group' }).click()
    await page.getByRole('alertdialog').getByRole('button', { name: 'Delete group' }).click()
    await expect(section).toHaveCount(0)
    const def = groups.find((g) => g.name === 'Default')!

    await expect
      .poll(async () => (await api.get<{ group_id: string }>(`/v1/channels/${channel.id}`)).group_id)
      .toBe(def.id)
  })
})
