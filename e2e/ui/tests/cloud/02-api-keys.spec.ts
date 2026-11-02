import type { Page } from '@playwright/test'

import { Api } from '../../lib/api.js'
import { axe, expect, ready, test, uniq } from '../../lib/fixtures.js'
import { state } from '../../lib/state.js'

/** Reads the one-time key out of the reveal dialog. */
async function revealedKey(page: Page): Promise<string> {
  const dialog = page.getByRole('dialog', { name: /^(Your new|Rotated) API key$/ })

  await expect(dialog).toContainText('shown only once')
  const key = (await dialog.getByText(/^stk_[a-z0-9]+$/).textContent())?.trim()

  expect(key, 'the new key is shown once').toBeTruthy()

  return key!
}

/** Opens the grouped Scopes multiselect, checks which scopes are ticked, optionally toggles some, closes it. */
async function scopes(page: Page, ticked: Record<string, boolean>, toggle: string[] = []) {
  await page.getByRole('dialog', { name: 'Create API key' }).getByRole('button', { name: 'Scopes' }).click()

  for (const [s, on] of Object.entries(ticked)) {
    await expect(page.getByRole('option', { name: new RegExp(`^${s}`) })).toHaveAttribute('data-checked', String(on))
  }

  for (const s of toggle) {
    await page.getByRole('option', { name: new RegExp(`^${s}`) }).click()
  }

  await page.keyboard.press('Escape')
}

test.describe('API keys (cloud)', () => {
  test('list: active and revoked keys, last used, scopes', async ({ page }) => {
    await page.goto('/integrations/api-keys')
    await ready(page, 'Integrations')
    const agent = page.getByRole('row').filter({ hasText: 'Content agent' })

    await expect(agent).toContainText('Active')
    await expect(agent).not.toContainText('Never')
    await expect(page.getByRole('row').filter({ hasText: 'Old CI key' })).toContainText('Revoked')
    await expect(page.getByRole('row').filter({ hasText: 'Read-only dashboard' })).toContainText('Never')
  })

  test('create: name required, presets set scopes (Full access includes the delete scopes), key shown once and works', async ({
    page,
  }) => {
    const name = uniq('UI key')

    await page.goto('/integrations/api-keys')
    await page.getByRole('button', { name: 'Create API key' }).click()
    const dialog = page.getByRole('dialog', { name: 'Create API key' })

    await dialog.getByRole('button', { name: 'Create key' }).click()
    await expect(dialog.getByRole('textbox', { name: 'Name' })).toHaveAttribute('aria-invalid', 'true')
    await dialog.getByRole('textbox', { name: 'Name' }).fill(name)

    await dialog.getByRole('button', { name: /^Read-only agent/ }).click()
    await scopes(page, { 'posts:read': true, 'posts:write': false, 'posts:publish': false })

    await dialog.getByRole('button', { name: /^Full access/ }).click()

    await scopes(page, {
      'collections:delete': true,
      'posts:delete': true,
      'posts:publish': true,
      'channels:manage': true,
      admin: true,
    })

    // The publisher automation: read and publish, nothing else; then posts:delete on top.
    await dialog.getByRole('button', { name: /^Publisher automation/ }).click()
    await scopes(page, { 'posts:publish': true, 'posts:write': false, admin: false }, ['posts:delete'])
    await dialog.getByRole('button', { name: 'Create key' }).click()

    const key = await revealedKey(page)
    const res = await new Api(state().cloud.api, key).get<{ principal: { scopes: string[] } }>('/v1/session')

    expect(res.principal.scopes).toContain('posts:delete')
    expect(res.principal.scopes).toContain('posts:publish')
    expect(res.principal.scopes).not.toContain('posts:write')
    expect(res.principal.scopes).not.toContain('admin')
    await page.getByRole('button', { name: "I've copied it" }).click()
    await expect(page.getByRole('row').filter({ hasText: name })).toContainText('Active')
  })

  for (const day of [1, 2]) {
    test(`an expiry date cannot use past days (month day ${day})`, async ({ page, api }) => {
      const session = await api.get<{ workspace: { timezone: string } }>('/v1/session')

      expect(session.workspace.timezone).toBe('UTC')
      // Use the seeded workspace's zone, not the Node process's local date. The second of the month also
      // reproduces the ambiguous day-1/day-15 accessible names (the same weekday) on every run.
      const now = new Date()
      const today = new Date(Date.UTC(now.getUTCFullYear(), now.getUTCMonth(), day, 12))

      await page.clock.setFixedTime(today)
      await page.goto('/integrations/api-keys')
      await page.getByRole('button', { name: 'Create API key' }).click()
      const dialog = page.getByRole('dialog', { name: 'Create API key' })

      await dialog.getByRole('textbox', { name: 'Name' }).fill(uniq('Expiring key'))
      // Expires: presets (a month by default) or a day you pick, from tomorrow up to a year ahead.
      await expect(dialog.getByRole('combobox', { name: 'Expires' })).toHaveText('1 month')
      await dialog.getByRole('combobox', { name: 'Expires' }).click()
      await page.getByRole('option', { name: 'Pick a day' }).click()
      await dialog.getByTestId('date-picker').click()
      const picker = page.getByRole('dialog', { name: 'Pick a date' })
      const yesterday = new Date(today)

      yesterday.setUTCDate(yesterday.getUTCDate() - 1)

      const previousMonth = yesterday.getUTCMonth() !== today.getUTCMonth()

      // Yesterday is shown (same month, or after one step back) but cannot be picked.
      if (previousMonth) {
        await picker.getByRole('button', { name: 'Previous month' }).click()
      }

      await expect(
        picker.getByRole('button', {
          name: yesterday.toLocaleDateString('en-US', {
            timeZone: session.workspace.timezone,
            weekday: 'long',
            day: 'numeric',
            month: 'long',
          }),
          exact: true,
        }),
      ).toBeDisabled()

      if (previousMonth) {
        await picker.getByRole('button', { name: 'Next month' }).click()
      }

      // Pick next month's 15th so the real server clock accepts the expiry throughout this month.
      await picker.getByRole('button', { name: 'Next month' }).click()
      await picker.getByRole('button', { name: /\b15$/ }).first().click()
      await expect(picker).toBeHidden()
      await expect(dialog.getByTestId('date-picker')).toContainText(/15/)
      await dialog.getByRole('button', { name: 'Create key' }).click()
      await expect(page.getByText(/Copy it now/)).toBeVisible()
    })
  }

  test('rotate shows a new key and retires the old one; revoke turns the key off', async ({ page, api }) => {
    const name = uniq('Rotate me')
    const created = await api.post<{ key: string; id: string }>('/v1/api-keys', { name, scopes: ['posts:read'] })

    await page.goto('/integrations/api-keys')
    const row = page.getByRole('row').filter({ hasText: name })

    await row.getByRole('button', { name: `Options for ${name}` }).click()
    await page.getByRole('menuitem', { name: /Rotate/ }).click()
    await page.getByRole('alertdialog').getByRole('button', { name: 'Rotate key' }).click()
    const rotated = await revealedKey(page)

    expect(rotated).not.toBe(created.key)
    expect((await new Api(state().cloud.api, created.key).raw('GET', '/v1/posts')).status).toBe(401)
    expect((await new Api(state().cloud.api, rotated).raw('GET', '/v1/posts')).status).toBe(200)
    await page.getByRole('button', { name: "I've copied it" }).click()
    // Rotation is a new key; the old one stays listed as revoked.
    const oldRow = page.getByRole('row').filter({ hasText: created.key.slice(0, 10) })
    const newRow = page.getByRole('row').filter({ hasText: rotated.slice(0, 10) })

    await expect(oldRow).toContainText('Revoked')
    await expect(newRow).toContainText('Active')
    await newRow.getByRole('button', { name: `Options for ${name}` }).click()
    await page.getByRole('menuitem', { name: /Revoke/ }).click()
    await page.getByRole('alertdialog').getByRole('button', { name: 'Revoke key' }).click()
    await expect(page.getByText('API key revoked')).toBeVisible()
    await expect(newRow).toContainText('Revoked')
    expect((await new Api(state().cloud.api, rotated).raw('GET', '/v1/posts')).status).toBe(401)
  })

  test('axe on the cloud-only pages: API keys, the key dialog, plan & usage, login and setup', async ({
    page,
    browser,
  }) => {
    const found: Record<string, unknown> = {}

    for (const path of ['/integrations/api-keys', '/settings/plan', '/settings', '/schedule', '/settings/data']) {
      await page.goto(path)
      await ready(page)
      await page.waitForLoadState('networkidle')
      const v = await axe(page)

      if (v.length) {
        found[path] = v
      }
    }

    await page.emulateMedia({ reducedMotion: 'reduce' })
    await page.goto('/integrations/api-keys')
    await page.getByRole('button', { name: 'Create API key' }).click()
    await page.waitForTimeout(700)
    const dialog = await axe(page)

    if (dialog.length) {
      found['create key dialog'] = dialog
    }

    const ctx = await browser.newContext({ storageState: { cookies: [], origins: [] } })
    const anon = await ctx.newPage()

    for (const path of ['/login', '/signup']) {
      await anon.goto(path)
      await ready(anon)
      const v = await axe(anon)

      if (v.length) {
        found[path] = v
      }
    }

    await ctx.close()
    expect(found).toEqual({})
  })
})
