import { freshCollection } from '../../lib/data.js'
import { expect, ready, test, uniq } from '../../lib/fixtures.js'
import { state } from '../../lib/state.js'

test.describe('alerts', () => {
  test('the list with health, a test message, deliveries, pause and resume', async ({ page }) => {
    await page.goto('/alerts')
    await ready(page, 'Alerts')
    const ntfy = page.getByRole('article', { name: 'Phone (ntfy)' })

    await expect(ntfy).toContainText('Active')
    await expect(page.getByTestId('usage-chip-alerts')).toHaveText(/2 alerts/)
    await ntfy.getByRole('button', { name: 'Send test' }).click()
    await expect(page.getByText('Test message sent')).toBeVisible()
    await ntfy.getByRole('button', { name: 'Deliveries' }).click()
    const deliveries = page.getByRole('dialog', { name: 'Deliveries' })

    await expect(deliveries).toBeVisible()
    await expect(deliveries.getByText(/sent/i).first()).toBeVisible()
    await page.keyboard.press('Escape')

    await ntfy.getByRole('switch', { name: 'Pause Phone (ntfy)' }).click()
    await expect(ntfy.getByRole('switch', { name: 'Enable Phone (ntfy)' })).not.toBeChecked()
    await ntfy.getByRole('switch', { name: 'Enable Phone (ntfy)' }).click()
    await expect(ntfy.getByRole('switch', { name: 'Pause Phone (ntfy)' })).toBeChecked()
  })

  test('a failing email alert: the test explains the failure', async ({ page }) => {
    await page.goto('/alerts')
    const email = page.getByRole('article', { name: 'Team email' })

    await email.getByRole('button', { name: 'Send test' }).click()
    // The SMTP server (port 9) refuses: the error is shown, not a success.
    await expect(page.getByText(/connection refused|could not|failed|dial/i).first()).toBeVisible({ timeout: 20_000 })
    await expect(page.getByText('Test message sent')).toHaveCount(0)
  })

  test('create an ntfy alert with validation and a collection filter, it receives a published post, edit it, delete it', async ({
    page,
    api,
  }) => {
    const name = uniq('UI ntfy')
    const col = await freshCollection(api, { posts: 1 })

    await page.goto('/alerts')
    await page.getByRole('main').getByRole('button', { name: 'New alert' }).click()
    const sheet = page.getByRole('dialog').last()

    await sheet.getByRole('radio', { name: 'ntfy' }).click()
    await expect(sheet.getByRole('radio', { name: 'ntfy' })).toHaveAttribute('aria-checked', 'true')
    await sheet.getByRole('textbox', { name: /^Name/ }).fill(name)
    await sheet
      .getByRole('button', { name: /Create/ })
      .last()
      .click()
    // Topic is required.
    await expect(sheet.locator('[aria-invalid="true"]').first()).toBeVisible()
    await sheet.getByRole('textbox', { name: /^Topic/ }).fill(`staqly-ui-${Date.now()}`)
    await sheet.getByRole('textbox', { name: /^Server/ }).fill(state().receiver)
    await sheet
      .getByRole('button', { name: /Create/ })
      .last()
      .click()
    await expect(sheet).toBeHidden()
    const card = page.getByRole('article', { name })

    await expect(card).toBeVisible()
    await expect(card).toContainText('Nothing sent yet')

    // A post goes out: the alert sends.
    const created = (await api.get<{ data: { id: string; name: string }[] }>('/v1/alerts')).data.find(
      (a) => a.name === name,
    )!

    await api.publishThrough(col.posts[0]!.id)
    await expect
      .poll(
        async () =>
          (await api.get<{ data: { status: string }[] }>(`/v1/alerts/${created.id}/deliveries`)).data[0]?.status,
        { timeout: 30_000 },
      )
      .toBe('sent')
    await page.reload()
    await expect(page.getByRole('article', { name })).toContainText(/Last sent/)

    await card.getByRole('button', { name: `${name} options` }).click()
    await page.getByRole('menuitem', { name: /Edit/ }).click()
    const edit = page.getByRole('dialog').last()

    await edit.getByRole('textbox', { name: /^Name/ }).fill(`${name} edited`)
    await edit.getByRole('button', { name: /Save/ }).last().click()
    await expect(page.getByRole('article', { name: `${name} edited` })).toBeVisible()
    await page
      .getByRole('article', { name: `${name} edited` })
      .getByRole('button', { name: /options/ })
      .click()
    await page.getByRole('menuitem', { name: /Delete/ }).click()
    await page
      .getByRole('alertdialog')
      .getByRole('button', { name: /Delete/ })
      .click()
    await expect(page.getByRole('article', { name: `${name} edited` })).toHaveCount(0)
  })
})
