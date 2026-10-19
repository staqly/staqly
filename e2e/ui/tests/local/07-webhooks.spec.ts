import type { Page } from '@playwright/test'

import { dismissToasts, expect, ready, test } from '../../lib/fixtures.js'
import { state } from '../../lib/state.js'

const card = (page: Page, url: string) => page.getByTestId('webhook-card').filter({ hasText: url })

test.describe('webhooks', () => {
  test('lists hooks with their state; deliveries of every status with details', async ({ page }) => {
    const r = state().receiver

    await page.goto('/webhooks')
    await ready(page, 'Webhooks')
    await expect(card(page, `${r}/ok`).first()).toContainText('Active')
    await expect(card(page, `${r}/ok?paused`)).toContainText('Paused')
    await expect(card(page, `${r}/fail`)).toContainText('up to 1 attempt')
    await expect(card(page, `${r}/fail`)).not.toContainText('1 attempts')

    // Succeeded
    await card(page, 'Publisher').getByRole('button', { name: /\/ok/ }).first().click()
    const table = page.getByTestId('deliveries-table')

    await expect(table.getByText('Succeeded').first()).toBeVisible()
    // Failed, with the receiver's answer in the details
    await card(page, 'Broken receiver')
      .getByRole('button', { name: /\/fail/ })
      .click()
    await expect(table.getByText('Failed').first()).toBeVisible()
    await table.getByRole('button', { name: 'Details' }).first().click()
    const details = page.getByRole('dialog')

    await expect(details).toContainText('500')
    await expect(details).toContainText('receiver says no')
    await page.keyboard.press('Escape')
    // Pending (the receiver never answers in time)
    await card(page, 'Slow receiver')
      .getByRole('button', { name: /\/slow/ })
      .click()
    await expect(table.getByText(/Pending/i).first()).toBeVisible()
  })

  test('create with validation, send a test, pause, edit, rotate the secret, delete', async ({ page, api }) => {
    const r = state().receiver
    const url = `${r}/ok?ui=${Date.now()}`

    await page.goto('/webhooks')
    await page.getByRole('button', { name: 'New webhook' }).first().click()
    const sheet = page.getByRole('dialog', { name: 'New webhook' })

    await sheet.getByRole('textbox', { name: /^Endpoint URL/ }).fill('not a url')
    await sheet.getByRole('button', { name: 'Create webhook' }).click()
    await expect(sheet.getByRole('textbox', { name: /^Endpoint URL/ })).toHaveAttribute('aria-invalid', 'true')
    await sheet.getByRole('textbox', { name: /^Endpoint URL/ }).fill(url)
    await sheet.getByRole('textbox', { name: 'Description' }).fill('UI test hook')
    await sheet.getByRole('spinbutton', { name: /^Max attempts/ }).fill('0')
    await sheet.getByRole('button', { name: 'Create webhook' }).click()
    await expect(sheet.getByRole('spinbutton', { name: /^Max attempts/ })).toHaveAttribute('aria-invalid', 'true')
    await sheet.getByRole('spinbutton', { name: /^Max attempts/ }).fill('3')
    await sheet.getByRole('button', { name: 'Create webhook' }).click()
    await expect(sheet).toBeHidden()

    // The full secret is shown once.
    const c = card(page, url)

    await expect(c.getByRole('status')).toContainText('Copy the signing secret now')
    await c.getByRole('button', { name: 'Done' }).click()
    await expect(c).toContainText(/whsec_\*{4}/)

    // Send test → a delivery appears
    await c.getByRole('button', { name: 'Send test' }).click()
    await expect(page.getByText('Test event queued')).toBeVisible()
    await c.getByRole('button', { name: url }).click()
    await expect(page.getByTestId('deliveries-table').getByText('Succeeded').first()).toBeVisible({ timeout: 15_000 })

    // Pause
    await c.getByRole('switch', { name: 'Pause webhook' }).click()
    await expect(c).toContainText('Paused')
    await expect
      .poll(
        async () =>
          (await api.get<{ data: { url: string; active: boolean }[] }>('/v1/webhooks')).data.find((w) => w.url === url)
            ?.active,
      )
      .toBe(false)

    // Edit: description
    await c.getByRole('button', { name: 'Webhook options' }).click()
    await page.getByRole('menuitem', { name: 'Edit' }).click()
    const edit = page.getByRole('dialog', { name: 'Edit webhook' })

    await expect(edit.getByRole('textbox', { name: /^Endpoint URL/ })).toHaveValue(url)
    await edit.getByRole('textbox', { name: 'Description' }).fill('UI test hook (edited)')
    await edit.getByRole('button', { name: /Save/ }).click()
    await expect(edit).toBeHidden()
    await expect(c).toContainText('UI test hook (edited)')

    // Rotate
    await dismissToasts(page)
    await c.getByRole('button', { name: 'Webhook options' }).click()
    await page.getByRole('menuitem', { name: 'Rotate secret' }).click()
    await expect(c.getByRole('status')).toContainText('Copy the signing secret now')
    await c.getByRole('button', { name: 'Done' }).click()

    // Delete
    await c.getByRole('button', { name: 'Webhook options' }).click()
    await page.getByRole('menuitem', { name: 'Delete' }).click()
    await page.getByRole('alertdialog').getByRole('button', { name: 'Delete webhook' }).click()
    await expect(c).toHaveCount(0)
  })

  test('retry a failed delivery', async ({ page }) => {
    await page.goto('/webhooks')
    await card(page, 'Broken receiver')
      .getByRole('button', { name: /\/fail/ })
      .click()
    const table = page.getByTestId('deliveries-table')
    const retry = table.getByRole('button', { name: /^Retry delivery/ }).first()

    await retry.click()
    await expect(page.getByText('Delivery re-queued')).toBeVisible()
  })
})
