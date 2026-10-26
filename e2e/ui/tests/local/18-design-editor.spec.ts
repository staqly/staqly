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

test.describe('the design editor', () => {
  test('two pictures open as one page, a heading and a second page are added, both pages save, it opens again', async ({
    page,
  }) => {
    test.setTimeout(120_000)
    const stamp = Date.now().toString(36)
    const names = [`design-a-${stamp}.png`, `design-b-${stamp}.png`]

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

    // Edit from the selection: the first picture fills the page, the second sits on it.
    await page.getByTestId('media-selection').getByRole('link', { name: 'Edit' }).click()
    await expect(page).toHaveURL(/\/media\/edit\?ids=/)
    await expect(page.getByTestId('design-canvas')).toBeVisible({ timeout: 30_000 })

    const tools = page.getByRole('navigation', { name: 'Design tools' })

    await tools.getByRole('button', { name: 'Layers' }).click()
    await expect(page.getByRole('complementary', { name: 'Layers' }).getByText('Picture')).toHaveCount(2)

    // A heading: the bar above the page offers its font.
    await tools.getByRole('button', { name: 'Text' }).click()
    await page.getByRole('button', { name: 'Add a heading' }).click()
    await expect(page.getByRole('combobox', { name: 'Font' })).toHaveValue('Montserrat')

    // A second page (a carousel).
    await page.getByRole('button', { name: 'Add a page' }).click()
    await expect(page.getByRole('button', { name: 'Page 2' })).toHaveAttribute('aria-current', 'page')

    // Readable in staqly's colours: no contrast problems (the canvas itself is the picture, not text).
    const contrast = (await axe(page)).filter((v) => v.id === 'color-contrast')

    expect(contrast, JSON.stringify(contrast, null, 2)).toEqual([])

    await page.getByTestId('design-save').click()
    await expect(page.getByText('Saved 2 pages to Media')).toBeVisible({ timeout: 30_000 })
    await expect(page).toHaveURL(/\?project=/)

    // One file per page in the library: the first has photos (JPG), the second only a white page (PNG).
    const list = await (await page.request.get('/api/v1/media?limit=100')).json()
    const saved = (list.data as { original_name: string }[]).map((m) => m.original_name)

    // Named after the picture the design opened with (either one: the selection's order).
    expect(saved.filter((n) => new RegExp(`^design-[ab]-${stamp} \\(1 of 2\\)\\.jpg$`).test(n))).toHaveLength(1)
    expect(saved.filter((n) => new RegExp(`^design-[ab]-${stamp} \\(2 of 2\\)\\.png$`).test(n))).toHaveLength(1)

    // Opened again from its project: both pages, the heading still a text.
    await page.reload()
    await expect(page.getByRole('button', { name: 'Page 2' })).toBeVisible({ timeout: 30_000 })
    await page.getByRole('button', { name: 'Page 1' }).click()
    await tools.getByRole('button', { name: 'Layers' }).click()
    await expect(page.getByRole('complementary', { name: 'Layers' }).getByText('Add a heading')).toBeVisible()

    // In Media the two pages are one tile: the carousel.
    await page.goto('/media')
    await ready(page, 'All files')
    await expect(
      page
        .getByRole('listitem')
        .filter({ hasText: `${stamp} (1 of 2)` })
        .getByText('2 pages'),
    ).toBeVisible()
    await expect(page.getByRole('button', { name: new RegExp(`\\(2 of 2\\)\\.png$`), exact: false })).toHaveCount(0)
  })
})
