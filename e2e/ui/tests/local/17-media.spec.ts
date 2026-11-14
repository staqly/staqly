import { expect, ready, test } from '../../lib/fixtures.js'

// A 2x3 PNG, so the library measures it (a 2:3 picture: Pinterest fits it as it is).
const PNG = Buffer.from(
  'iVBORw0KGgoAAAANSUhEUgAAAAIAAAADCAIAAAA2iEnWAAAAEElEQVR4nGP4z8AARAwoFABE0AX7pM/egAAAAABJRU5ErkJggg==',
  'base64',
)

test.describe('the media library', () => {
  test('upload a picture, describe it, start a post with it, delete it', async ({ page }) => {
    const name = `library-${Date.now().toString(36)}.png`

    await page.goto('/media')
    await ready(page, 'All files')
    await page.getByTestId('media-upload-input').setInputFiles({ name, mimeType: 'image/png', buffer: PNG })
    // Done: the upload card goes away and a toast names the file.
    await expect(page.getByText(`Uploaded ${name}`)).toBeVisible()
    const tile = page.getByRole('button', { name, exact: true })

    await expect(tile).toBeVisible()

    // The drawer: size and fit, alt text saved.
    await tile.click()
    const drawer = page.getByTestId('media-detail')

    await expect(drawer.getByTestId('media-facts')).toContainText('PNG · 73 B · 2 × 3 · 2:3')
    await drawer.getByRole('tab', { name: /Where it fits/ }).click()
    await expect(drawer.getByTestId('media-fit').filter({ hasText: 'Pinterest' })).toContainText('Fits')
    await drawer.getByRole('tab', { name: 'File' }).click()
    await drawer.getByLabel('Alt text').fill('A tiny test picture')
    await drawer.getByRole('button', { name: 'Save changes' }).click()
    await expect(drawer.getByRole('button', { name: 'Save changes' })).toBeDisabled()

    // New post with this (header icon): the composer opens with the picture attached.
    await drawer.getByRole('button', { name: 'New post with this' }).click()
    const composer = page.getByRole('dialog', { name: 'New post' })

    await expect(composer.getByRole('img', { name: 'A tiny test picture' })).toBeVisible()
    // Nothing was typed in it, so it closes without asking.
    await page.keyboard.press('Escape')
    await expect(composer).toBeHidden()

    // Search finds it; delete it from its drawer: it moves to Deleted (Undo brings it back), then for good.
    // Search is behind the Filter button.
    await page.getByTestId('filter-button').click()
    await page.getByLabel('Search files').fill(name.slice(0, 12))
    await page.keyboard.press('Escape')
    await expect(page.getByRole('button', { name, exact: true })).toBeVisible()
    await page.getByRole('button', { name, exact: true }).click()
    await drawer.getByRole('button', { name: 'Delete file' }).click()
    await expect(page.getByRole('button', { name, exact: true })).toHaveCount(0)
    await page.getByRole('button', { name: 'Undo' }).click()
    await expect(page.getByRole('button', { name, exact: true })).toBeVisible()
    await page.getByRole('button', { name, exact: true }).click()
    await drawer.getByRole('button', { name: 'Delete file' }).click()
    await page
      .getByTestId('media-pane')
      .getByRole('button', { name: /Deleted/ })
      .click()
    await page.getByRole('button', { name, exact: true }).click()
    await drawer.getByRole('button', { name: 'Delete forever' }).click()
    await page.getByRole('alertdialog').getByRole('button', { name: 'Delete forever' }).click()
    await expect(page.getByRole('button', { name, exact: true })).toHaveCount(0)
  })

  test('organise files in folders and tags', async ({ page }) => {
    const stamp = Date.now().toString(36)
    const name = `folders-${stamp}.png`
    const folder = `Clients ${stamp}`

    await page.goto('/media')
    await ready(page, 'All files')
    await page.getByTestId('media-upload-input').setInputFiles({ name, mimeType: 'image/png', buffer: PNG })
    await expect(page.getByRole('button', { name, exact: true })).toBeVisible()

    // The same bytes again: uploaded anyway, with a note that the library has it already.
    await page
      .getByTestId('media-upload-input')
      .setInputFiles({ name: `again-${name}`, mimeType: 'image/png', buffer: PNG })
    await expect(page.getByText(/Already in your library as/)).toBeVisible()

    // A folder from the pane; the view opens it, empty.
    await page.getByTestId('media-new-folder').click()
    await page.getByTestId('media-folder-dialog').getByLabel('Name').fill(folder)
    await page.getByRole('button', { name: 'Create folder' }).click()
    await expect(page.getByTestId('media-place')).toHaveText(folder)
    await expect(page.getByText('This folder is empty')).toBeVisible()

    // Back to every file: move the picture into the folder from its tile menu, then tag it in bulk.
    await page.getByTestId('media-place-all').click()
    await page.getByRole('button', { name: `Select ${name}` }).click()
    await page.getByRole('toolbar', { name: 'Selected files' }).getByRole('button', { name: 'Move to folder' }).click()
    await page.getByTestId('media-folder-picker').getByRole('button', { name: folder }).click()
    await expect(page.getByText(`Moved 1 file to ${folder}`)).toBeVisible()
    await page.getByRole('button', { name: `Select ${name}` }).click()
    await page
      .getByRole('toolbar', { name: 'Selected files' })
      .getByRole('button', { name: 'Tags', exact: true })
      .click()
    const tags = page.getByTestId('media-tags-dialog')

    await tags.getByRole('button', { name: 'Add tags' }).click()
    await page.keyboard.type(`Hero ${stamp}`)
    await page.getByRole('option', { name: `Add “hero-${stamp}”` }).click()
    // Escape closes the tag list first, not the dialog.
    await page.keyboard.press('Escape')
    await tags.getByRole('button', { name: 'Apply' }).click()
    await expect(tags).toBeHidden()
    await page.getByRole('button', { name: 'Clear selection' }).click()

    // The folder and the tag both find it.
    await page.getByRole('treeitem', { name: new RegExp(folder) }).click()
    await expect(page.getByRole('button', { name, exact: true })).toBeVisible()
    await page
      .getByTestId('media-tags')
      .getByRole('button', { name: new RegExp(`hero-${stamp}`) })
      .click()
    await expect(page.getByRole('button', { name, exact: true })).toBeVisible()

    // Deleting the folder keeps the file.
    await page.getByRole('button', { name: 'Folder actions' }).click()
    await page.getByRole('menuitem', { name: 'Delete folder' }).click()
    await expect(page.getByRole('alertdialog')).toContainText('No file is deleted')
    await page.getByRole('alertdialog').getByRole('button', { name: 'Delete folder' }).click()
    await expect(page.getByRole('treeitem', { name: new RegExp(folder) })).toHaveCount(0)
    await expect(page.getByRole('button', { name, exact: true })).toBeVisible()
  })
})
