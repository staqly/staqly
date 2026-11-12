import { axe, expect, ready, test } from '../../lib/fixtures.js'

test('feedback opens from the user menu, retains a failed report and submits ten private screenshots', async ({
  page,
}) => {
  await page.goto('/schedule?token=not-stored#private')
  await ready(page, 'Schedule')
  await page.getByTestId('user-menu-trigger').first().click()
  await page.getByRole('menuitem', { name: 'Help & Feedback', exact: true }).click()
  const dialog = page.getByRole('dialog', { name: 'Help & Feedback', exact: true })

  await expect(dialog).toBeVisible()
  await expect.poll(async () => (await dialog.boundingBox())!.width).toBeGreaterThanOrEqual(650)
  expect(
    (await dialog.getByRole('textbox', { name: 'Description', exact: true }).boundingBox())!.height,
  ).toBeGreaterThanOrEqual(220)
  expect(await axe(page)).toEqual([])
  await dialog.getByRole('textbox', { name: 'Description', exact: true }).fill('The schedule did not show my post')
  await dialog.getByLabel('Screenshots (optional)').setInputFiles(
    Array.from({ length: 10 }, (_, index) => ({
      name: `screenshot-${index}.png`,
      mimeType: 'image/png',
      buffer: Buffer.from(
        'iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAQAAAC1HAwCAAAAC0lEQVR42mP8/x8AAwMCAO+a4X8AAAAASUVORK5CYII=',
        'base64',
      ),
    })),
  )
  await expect(dialog.getByRole('button', { name: /Remove screenshot/ })).toHaveCount(10)
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
  await page.getByTestId('user-menu-trigger').first().click()
  await page.getByRole('menuitem', { name: 'Help & Feedback', exact: true }).click()
  const dialog = page.getByRole('dialog', { name: 'Help & Feedback', exact: true })

  await expect(dialog).toBeVisible()
  const bounds = await dialog.boundingBox()

  expect(bounds).not.toBeNull()
  expect(bounds!.x).toBeGreaterThanOrEqual(0)
  expect(bounds!.x + bounds!.width).toBeLessThanOrEqual(320)
  await dialog.getByLabel('Screenshots (optional)').setInputFiles(
    Array.from({ length: 10 }, (_, index) => ({
      name: `screenshot-${index}-${'long-name'.repeat(12)}.png`,
      mimeType: 'image/png',
      buffer: Buffer.from('image'),
    })),
  )
  await expect(dialog.getByRole('button', { name: /Remove screenshot/ })).toHaveCount(10)
  expect(await dialog.evaluate((el) => el.scrollWidth <= el.clientWidth)).toBe(true)
  await expect(dialog.getByRole('button', { name: 'Send feedback' })).toBeInViewport()
  expect(await axe(page)).toEqual([])
  await page.keyboard.press('Tab')
  expect(await dialog.evaluate((el) => el.contains(document.activeElement))).toBe(true)
  await page.keyboard.press('Escape')
  await expect(dialog).toBeHidden()
})
