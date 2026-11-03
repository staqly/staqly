import { axe, expect, ready, test } from '../../lib/fixtures.js'

test('feedback opens from the sidebar, retains a failed report and submits a private screenshot', async ({ page }) => {
  await page.goto('/schedule?token=not-stored#private')
  await ready(page, 'Schedule')
  const feedback = page.getByRole('button', { name: 'Feedback', exact: true })

  expect(
    await feedback.evaluate((el) => {
      const notifications = document.querySelector('[data-testid="notifications-bell"]')

      return !!notifications && !!(el.compareDocumentPosition(notifications) & Node.DOCUMENT_POSITION_FOLLOWING)
    }),
  ).toBe(true)
  await feedback.click()
  const dialog = page.getByRole('dialog', { name: 'Feedback', exact: true })

  await expect(dialog).toBeVisible()
  expect(await axe(page)).toEqual([])
  await dialog.getByRole('textbox', { name: 'Description', exact: true }).fill('The schedule did not show my post')
  await dialog.getByLabel('Screenshot (optional)').setInputFiles({
    name: 'screenshot.png',
    mimeType: 'image/png',
    buffer: Buffer.from(
      'iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAQAAAC1HAwCAAAAC0lEQVR42mP8/x8AAwMCAO+a4X8AAAAASUVORK5CYII=',
      'base64',
    ),
  })
  await page.route('**/api/v1/feedback', (route) =>
    route.fulfill({
      status: 500,
      contentType: 'application/json',
      body: JSON.stringify({ error: { code: 'internal', message: 'Please try again' } }),
    }),
  )
  await dialog.getByRole('button', { name: 'Send feedback' }).click()
  await expect(dialog.getByRole('alert')).toContainText('Please try again')
  await expect(dialog.getByRole('textbox', { name: 'Description', exact: true })).toHaveValue(
    'The schedule did not show my post',
  )
  await page.unroute('**/api/v1/feedback')
  const response = page.waitForResponse(
    (res) => res.url().endsWith('/api/v1/feedback') && res.request().method() === 'POST',
  )

  await dialog.getByRole('button', { name: 'Send feedback' }).click()
  expect((await response).status()).toBe(201)
  await expect(dialog).toBeHidden()
  await expect(page.getByText('Feedback sent. Thank you!')).toBeVisible()
})

test('feedback fits a phone and keeps keyboard focus inside the modal', async ({ page }) => {
  await page.setViewportSize({ width: 320, height: 740 })
  await page.goto('/schedule')
  await ready(page, 'Schedule')
  // Open the mobile sidebar through the shell's toggle.
  await page.getByRole('button', { name: 'More', exact: true }).click()
  await page.getByRole('button', { name: 'Feedback', exact: true }).click()
  const dialog = page.getByRole('dialog', { name: 'Feedback', exact: true })

  await expect(dialog).toBeVisible()
  const bounds = await dialog.boundingBox()

  expect(bounds).not.toBeNull()
  expect(bounds!.x).toBeGreaterThanOrEqual(0)
  expect(bounds!.x + bounds!.width).toBeLessThanOrEqual(320)
  expect(await axe(page)).toEqual([])
  await page.keyboard.press('Tab')
  expect(await dialog.evaluate((el) => el.contains(document.activeElement))).toBe(true)
  await page.keyboard.press('Escape')
  await expect(dialog).toBeHidden()
})
