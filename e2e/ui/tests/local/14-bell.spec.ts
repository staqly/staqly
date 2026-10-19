import { freshCollection, titleOf } from '../../lib/data.js'
import { expect, ready, test } from '../../lib/fixtures.js'

test.describe('notification bell', () => {
  test('a published post shows up as unread, opening lists it with the link and clears the count', async ({
    page,
    api,
  }) => {
    const col = await freshCollection(api, { posts: 1 })
    const post = col.posts[0]!
    const title = titleOf(post.content)

    await api.publishThrough(post.id, 'https://example.com/posts/1')

    await page.goto('/schedule')
    await ready(page, 'Schedule')
    const bell = page.getByTestId('notifications-bell').first()

    // Other seeded runs may be unread too: at least this one.
    await expect(bell).toHaveAttribute('aria-label', /Notifications, \d+ unread/)
    await bell.click()
    const list = page.getByTestId('notifications-list')
    const row = list.getByRole('listitem').filter({ hasText: `“${title}” was published` })

    await expect(row).toBeVisible()
    // Still unread while the list is open (the row carries a dot); Mark all as read clears the count, also after a
    // reload.
    await expect(bell).toHaveAttribute('aria-label', /Notifications, \d+ unread/)
    await expect(row.getByRole('img', { name: 'New' })).toBeVisible()
    await page.getByRole('button', { name: 'Mark all as read' }).click()
    await expect(bell).toHaveAttribute('aria-label', 'Notifications')
    await page.keyboard.press('Escape')
    await expect(list).toBeHidden()
    await page.reload()
    await ready(page, 'Schedule')
    await expect(page.getByTestId('notifications-bell').first()).toHaveAttribute('aria-label', 'Notifications')
    // The row leads to the post.
    await page.getByTestId('notifications-bell').first().click()
    await page
      .getByTestId('notifications-list')
      .getByRole('button', { name: `“${title}” was published` })
      .click()
    await expect(page).toHaveURL(new RegExp(`/posts/${post.id}`))
    await expect(page.getByRole('dialog').getByRole('heading', { name: title })).toBeVisible()
  })

  test('a failed post and a request to the automation are listed too', async ({ page, api }) => {
    const col = await freshCollection(api, { posts: 1 })
    const post = col.posts[0]!

    await api.publishFailing(post.id, 'Mock API: 503')
    await page.goto('/schedule')
    await page.getByTestId('notifications-bell').first().click()
    const list = page.getByTestId('notifications-list')

    await expect(
      list.getByRole('listitem').filter({ hasText: `Publishing “${titleOf(post.content)}” failed` }),
    ).toBeVisible()
    await expect(
      list.getByRole('listitem').filter({ hasText: `“${titleOf(post.content)}” is waiting for your automation` }),
    ).toBeVisible()
  })
})
