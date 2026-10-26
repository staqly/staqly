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
  test('two pictures open as two pages, a heading moves between them, a third page is added, every page saves, it opens again', async ({
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

    // Edit from the selection: a page per picture (a carousel).
    await page.getByTestId('media-selection').getByRole('link', { name: 'Edit' }).click()
    await expect(page).toHaveURL(/\/media\/edit\?ids=/)
    await expect(page.getByTestId('design-canvas')).toBeVisible({ timeout: 30_000 })

    const tools = page.getByRole('navigation', { name: 'Design tools' })

    await tools.getByRole('button', { name: 'Layers' }).click()
    await expect(page.getByRole('complementary', { name: 'Layers' }).getByText('Picture')).toHaveCount(1)
    await expect(page.getByRole('button', { name: 'Page 2' })).toBeVisible()

    // A heading: the bar above the page offers its font.
    await tools.getByRole('button', { name: 'Text' }).click()
    await page.getByRole('button', { name: 'Add a heading' }).click()
    await expect(page.getByRole('combobox', { name: 'Font' })).toHaveValue('Montserrat')

    // Layers reorder by dragging: the bottom picture goes to the top.
    await tools.getByRole('button', { name: 'Layers' }).click()
    const layers = page.getByRole('list', { name: 'Layers, top first' }).getByRole('listitem')

    await expect(layers.first()).toHaveText(/Add a heading/)

    const from = (await layers.last().boundingBox())!
    const to = (await layers.first().boundingBox())!

    await page.mouse.move(from.x + 60, from.y + from.height / 2)
    await page.mouse.down()

    for (let i = 1; i <= 10; i++) {
      await page.mouse.move(from.x + 60, from.y + from.height / 2 - ((from.y - to.y) * i) / 10)
    }

    await page.mouse.up()
    await expect(layers.first()).toHaveText(/Picture/)

    // The heading moves to the second page (its menu, or a right-click on the layer).
    await page.getByRole('button', { name: 'More for Add a heading' }).click()
    await page.getByRole('menuitem', { name: 'To page 2' }).click()
    await expect(page.getByRole('button', { name: 'Page 2' })).toHaveAttribute('aria-current', 'page')
    await expect(layers.filter({ hasText: 'Add a heading' })).toHaveCount(1)

    // A third page.
    await page.getByRole('button', { name: 'Add a page' }).click()
    await expect(page.getByRole('button', { name: 'Page 3' })).toHaveAttribute('aria-current', 'page')

    // Readable in staqly's colours: no contrast problems (the canvas itself is the picture, not text).
    const contrast = (await axe(page)).filter((v) => v.id === 'color-contrast')

    expect(contrast, JSON.stringify(contrast, null, 2)).toEqual([])

    // A new design asks for its name and folder first.
    await page.getByTestId('design-save').click()
    await expect(page.getByRole('radio', { name: 'Media library (no folder)' })).toBeChecked()
    await page.getByTestId('design-save-confirm').click()
    await expect(page.getByText('Saved 3 pages to Media')).toBeVisible({ timeout: 30_000 })
    await expect(page).toHaveURL(/\?project=/)

    // One file per page in the library: pages with photos as JPG, the empty third one as PNG.
    const list = await (await page.request.get('/api/v1/media?limit=100')).json()
    const saved = (list.data as { original_name: string }[]).map((m) => m.original_name)

    // Named after the picture the design opened with (either one: the selection's order).
    expect(saved.filter((n) => new RegExp(`^design-[ab]-${stamp} \\(1 of 3\\)\\.jpg$`).test(n))).toHaveLength(1)
    expect(saved.filter((n) => new RegExp(`^design-[ab]-${stamp} \\(2 of 3\\)\\.jpg$`).test(n))).toHaveLength(1)
    expect(saved.filter((n) => new RegExp(`^design-[ab]-${stamp} \\(3 of 3\\)\\.png$`).test(n))).toHaveLength(1)

    // Opened again from its project: every page, the heading still a text on the second.
    await page.reload()
    await expect(page.getByRole('button', { name: 'Page 3' })).toBeVisible({ timeout: 30_000 })
    await page.getByRole('button', { name: 'Page 2' }).click()
    await tools.getByRole('button', { name: 'Layers' }).click()
    await expect(page.getByRole('complementary', { name: 'Layers' }).getByText('Add a heading')).toBeVisible()

    // In Media the three pages are one tile: the carousel.
    await page.goto('/media')
    await ready(page, 'All files')
    await expect(
      page
        .getByRole('listitem')
        .filter({ hasText: `${stamp} (1 of 3)` })
        .getByText('3 pages'),
    ).toBeVisible()
    await expect(page.getByRole('button', { name: new RegExp(`${stamp} \\([23] of 3\\)`), exact: false })).toHaveCount(
      0,
    )
  })

  // The local server's AI runs on a fake fal.ai (e2e/ui/lib/receiver.ts).
  test('AI makes a picture: a preview first, on the page only when kept, and Undo takes it back', async ({ page }) => {
    test.setTimeout(90_000)
    await page.goto('/media/edit')
    await expect(page.getByTestId('design-canvas')).toBeVisible({ timeout: 30_000 })

    const tools = page.getByRole('navigation', { name: 'Design tools' })
    const layers = page.getByRole('complementary', { name: 'Layers' })

    await tools.getByRole('button', { name: 'AI' }).click()
    await expect(page.getByRole('radio', { name: 'New picture' })).toBeChecked()
    await page.getByRole('textbox', { name: 'What should AI do?' }).fill('A lighthouse at dawn')
    await page.getByRole('button', { name: 'Make the picture' }).click()
    await expect(page.getByRole('img', { name: 'What AI made' })).toBeVisible({ timeout: 30_000 })
    await expect(page.getByText('Made by fal.ai.')).toBeVisible()

    const violations = await axe(page)

    expect(violations, JSON.stringify(violations, null, 2)).toEqual([])

    // Nothing on the page until it is kept.
    await tools.getByRole('button', { name: 'Layers' }).click()
    await expect(layers.getByText('Picture')).toHaveCount(0)
    await tools.getByRole('button', { name: 'AI' }).click()
    await page.getByRole('button', { name: 'Use it' }).click()
    await expect(page.getByText('Added to the design')).toBeVisible()

    await tools.getByRole('button', { name: 'Layers' }).click()
    await expect(layers.getByText('Picture')).toHaveCount(1)
    await page.getByRole('button', { name: 'Undo' }).click()
    await expect(layers.getByText('Picture')).toHaveCount(0)
  })
})
