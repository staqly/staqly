import { axe, expect, ready, test } from '../../lib/fixtures.js'

// Two small PNGs (4x3 and 2x2), uploaded in the library.
const PNG_A = Buffer.from(
  'iVBORw0KGgoAAAANSUhEUgAAAAQAAAADCAIAAAA7ljmRAAAAEElEQVR4nGM4YVMBRww4OQBfThHR5zkcVQAAAABJRU5ErkJggg==',
  'base64',
)
const PNG_B = Buffer.from(
  'iVBORw0KGgoAAAANSUhEUgAAAAIAAAACCAIAAAD91JpzAAAAEElEQVR4nGM4YVMBRAwQCgAq3gXxFDo9qwAAAABJRU5ErkJggg==',
  'base64',
)

test.describe('the media editor', () => {
  test('two pictures open as two layers, the result saves as a new file, back to Media', async ({ page }) => {
    test.setTimeout(120_000)
    const stamp = Date.now().toString(36)
    const names = [`editor-a-${stamp}.png`, `editor-b-${stamp}.png`]

    await page.goto('/media')
    await ready(page, 'All files')
    await page.getByTestId('media-upload-input').setInputFiles([
      { name: names[0]!, mimeType: 'image/png', buffer: PNG_A },
      { name: names[1]!, mimeType: 'image/png', buffer: PNG_B },
    ])

    for (const name of names) {
      await expect(page.getByRole('button', { name, exact: true })).toBeVisible()
    }

    for (const name of names) {
      await page.getByRole('button', { name: `Select ${name}` }).click()
    }

    // Edit from the selection: the editor page (a full page load: it is cross-origin isolated).
    await page.getByTestId('media-selection').getByRole('link', { name: 'Edit' }).click()
    await expect(page).toHaveURL(/\/media\/edit\?ids=/)
    const bar = page.getByTestId('editor-bar')

    await expect(bar.getByRole('heading', { name: '2 pictures' })).toBeVisible({ timeout: 60_000 })
    // OpenGPEX's layer panel lists the second picture as a layer over the first.
    await expect(page.getByText('#2', { exact: true }).first()).toBeVisible({ timeout: 30_000 })
    await expect(page.getByText('Background', { exact: true })).toBeVisible()

    // Readable in staqly's colours: no contrast problems (the canvas itself is the picture, not text). Measured
    // once saving is possible (the buttons fade in from disabled).
    await expect(bar.getByTestId('save-new')).toBeEnabled()
    await page.waitForTimeout(500)
    const contrast = (await axe(page)).filter((v) => v.id === 'color-contrast')

    expect(contrast, JSON.stringify(contrast, null, 2)).toEqual([])

    await bar.getByTestId('save-new').click()
    await expect(page.getByText(/^Saved as editor-[ab]-.* \(edited\)\.png$/)).toBeVisible({ timeout: 30_000 })

    // The saved file is in the library, next to the originals.
    const list = await (await page.request.get('/api/v1/media?limit=100')).json()

    expect(
      list.data.some(
        (m: { original_name: string }) =>
          /^editor-[ab]-.* \(edited\)\.png$/.test(m.original_name) && m.original_name.includes(stamp),
      ),
    ).toBe(true)

    await bar.getByRole('link', { name: 'Back to Media' }).click()
    await expect(page).toHaveURL(/\/media$/)
    await ready(page, 'All files')
  })
})
