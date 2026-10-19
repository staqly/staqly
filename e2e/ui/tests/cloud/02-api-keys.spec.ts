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

const scopes = (page: Page) =>
  page.getByRole('dialog', { name: 'Create API key' }).getByRole('group', { name: 'Scopes' })

test.describe('API keys (cloud)', () => {
  test('list: active and revoked keys, last used, scopes', async ({ page }) => {
    await page.goto('/integrations/api-keys')
    await ready(page, 'API keys')
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
    await expect(scopes(page).getByRole('checkbox', { name: 'posts:read' })).toBeChecked()
    await expect(scopes(page).getByRole('checkbox', { name: 'posts:write' })).not.toBeChecked()
    await expect(scopes(page).getByRole('checkbox', { name: 'posts:publish' })).not.toBeChecked()

    await dialog.getByRole('button', { name: /^Full access/ }).click()

    for (const s of ['collections:delete', 'posts:delete', 'posts:publish', 'channels:manage', 'admin']) {
      await expect(scopes(page).getByRole('checkbox', { name: s })).toBeChecked()
    }

    // The publisher automation: read and publish, nothing else; then posts:delete on top.
    await dialog.getByRole('button', { name: /^Publisher automation/ }).click()
    await expect(scopes(page).getByRole('checkbox', { name: 'posts:publish' })).toBeChecked()
    await expect(scopes(page).getByRole('checkbox', { name: 'posts:write' })).not.toBeChecked()
    await expect(scopes(page).getByRole('checkbox', { name: 'admin' })).not.toBeChecked()
    await scopes(page).getByRole('checkbox', { name: 'posts:delete' }).check()
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

  test('an expiry date comes from a calendar where past days cannot be picked', async ({ page }) => {
    await page.goto('/integrations/api-keys')
    await page.getByRole('button', { name: 'Create API key' }).click()
    const dialog = page.getByRole('dialog', { name: 'Create API key' })

    await dialog.getByRole('textbox', { name: 'Name' }).fill(uniq('Expiring key'))
    await dialog.getByTestId('date-picker').click()
    const picker = page.getByRole('dialog', { name: 'Pick a date' })
    const yesterday = new Date()

    yesterday.setDate(yesterday.getDate() - 1)

    // Yesterday is shown (same month, or after one step back) but cannot be picked.
    if (yesterday.getMonth() !== new Date().getMonth()) {
      await picker.getByRole('button', { name: 'Previous month' }).click()
    }

    await expect(
      picker.getByRole('button', {
        name: yesterday.toLocaleDateString('en-US', { weekday: 'long', day: 'numeric', month: 'long' }),
      }),
    ).toBeDisabled()
    await picker.getByRole('button', { name: 'Next month' }).click()
    await picker.getByRole('button', { name: /\b15$/ }).first().click()
    await expect(picker).toBeHidden()
    await expect(dialog.getByTestId('date-picker')).toContainText(/15/)
    await dialog.getByRole('button', { name: 'Create key' }).click()
    await expect(page.getByText(/Copy it now/)).toBeVisible()
  })

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

    for (const path of ['/login', '/setup']) {
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
