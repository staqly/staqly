import type { Page } from '@playwright/test'

import { freshCollection } from '../../lib/data.js'
import { expect, ready, test } from '../../lib/fixtures.js'

const rows = (page: Page) => page.getByTestId('posts-list').getByTestId('post-row')

test.describe('posts list', () => {
  test('select posts and delete them together from the floating bar; a row deletes one post', async ({ page, api }) => {
    const col = await freshCollection(api, { posts: 4 })

    await page.goto(`/posts?collection=${col.id}`)
    await ready(page, col.name)
    await expect(rows(page)).toHaveCount(4)

    // A checkbox, then Shift on another: the range between them is selected.
    await page.getByRole('checkbox', { name: `Select ${col.name} post 4` }).click()
    await page.getByRole('checkbox', { name: `Select ${col.name} post 2` }).click({ modifiers: ['Shift'] })
    const bar = page.getByRole('toolbar', { name: 'Selected posts' })

    await expect(bar).toContainText('3 selected')
    await bar.getByRole('button', { name: 'Delete' }).click()
    const confirm = page.getByRole('alertdialog')

    await expect(confirm).toContainText('Delete 3 posts?')
    await confirm.getByRole('button', { name: 'Delete 3 posts' }).click()
    await expect(rows(page)).toHaveCount(1)
    await expect(bar).toBeHidden()

    // The row's own Delete asks first, then removes just that post.
    const last = rows(page).first()

    await last.hover()
    await last.getByRole('button', { name: 'Delete', exact: true }).click()
    await page.getByRole('alertdialog').getByRole('button', { name: 'Delete' }).click()
    await expect(rows(page)).toHaveCount(0)
  })
})
