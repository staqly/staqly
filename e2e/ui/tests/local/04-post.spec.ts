import type { Page } from '@playwright/test'

import { daysAhead, freshCollection, getPost, titleOf } from '../../lib/data.js'
import { dismissToasts, expect, test } from '../../lib/fixtures.js'
import { pickDateTime } from '../../lib/pick.js'

const sheet = (page: Page) => page.getByRole('dialog').first()
const actions = (page: Page) => page.getByTestId('post-actions')

/** How each channel went: the list in the sheet's Delivery tab. */
async function channelList(page: Page) {
  await sheet(page)
    .getByRole('tab', { name: /Delivery/ })
    .click()

  return sheet(page).getByRole('list', { name: 'Delivery per channel' })
}

/** Publish now from the composer footer (the post as saved; it goes out after the short Undo window). */
async function publishNow(page: Page) {
  await page.getByTestId('publish-now').click()
}

test.describe('the post', () => {
  test('a published post: content, picture, tags, the channels with their links; it cannot be edited or re-published', async ({
    page,
    srv,
  }) => {
    await page.goto(`/posts/${srv.seed.posts.published}`)
    const s = sheet(page)

    await expect(s.getByRole('heading', { name: 'Posted yesterday' })).toBeVisible()
    await expect(s.getByText('Published', { exact: true }).first()).toBeVisible()
    // The text and its picture, read only: a post that went out cannot be edited.
    const text = s.getByTestId('post-content')

    await expect(text).toContainText('Already out.')
    await expect(text).toHaveAttribute('contenteditable', 'false')
    await expect(s.getByRole('img', { name: /\.(png|jpe?g|webp)$/i }).first()).toBeVisible()
    await expect(s).toContainText('This post went out and can no longer be edited')
    await expect(s.getByRole('button', { name: 'Save changes' })).toHaveCount(0)
    // Each channel with its state and link.
    const targets = (await channelList(page)).getByTestId('post-target')

    await expect(targets).toHaveCount(2)
    await expect(targets.filter({ hasText: 'Published' })).toHaveCount(2)
    await expect(targets.first().getByRole('link')).toHaveAttribute('href', 'https://instagram.example/p/abc')
    await expect(targets.first().getByRole('link')).toHaveAttribute('rel', /noopener/)
    // Nothing to publish any more: the icon row opens it, shows its numbers, shares, duplicates; it stays as history.
    await expect(actions(page).getByRole('button', { name: 'Publish now' })).toHaveCount(0)
    await expect(actions(page).getByRole('button', { name: 'Open post' })).toBeVisible()
    await expect(sheet(page).getByRole('tab', { name: 'Statistics' })).toBeVisible()
    await expect(actions(page).getByRole('button', { name: /Preview and feedback/ })).toBeVisible()
    await expect(actions(page).getByRole('button', { name: 'Duplicate' })).toBeVisible()
    await expect(actions(page).getByRole('button', { name: 'Delete' })).toHaveCount(0)
  })

  test('a failed post: the error on its channel, Retry asks the automation again, the result publishes it', async ({
    page,
    srv,
    api,
  }) => {
    const col = await freshCollection(api, { posts: 1 })
    const post = col.posts[0]!

    await api.publishFailing(post.id, 'Instagram API: 503 Service Unavailable')
    await page.goto(`/posts/${post.id}`)
    const s = sheet(page)

    await expect(s.getByText('Failed', { exact: true }).first()).toBeVisible()
    // The channel's card: its state, and Retry in the corner.
    const target = (await channelList(page)).getByRole('listitem').first()

    await expect(target).toContainText('Instagram API: 503 Service Unavailable')
    await target.getByRole('button', { name: 'Retry' }).click()
    await expect(page.getByText(/is being retried/)).toBeVisible()
    await expect(target).toContainText('Publishing')
    await api.waitRequested(post.id)
    await api.result(post.id, col.channel.id, { status: 'published', url: 'https://example.com/retried' })
    await expect(target).toContainText('Published', { timeout: 20_000 })
    await expect(target.getByRole('link')).toHaveAttribute('href', 'https://example.com/retried')
    // The post settles with its only channel: the header says Published.
    await expect(s.getByText('Published', { exact: true }).first()).toBeVisible()
    expect(srv.seed.posts.failed).toBeTruthy()
  })

  test('a draft: Publish now hands it to the automation; the sheet follows the result live', async ({ page, api }) => {
    const col = await freshCollection(api, { posts: 1 })
    const post = col.posts[0]!

    await page.goto(`/posts/${post.id}`)
    const s = sheet(page)

    await expect(s.getByText('Draft', { exact: true }).first()).toBeVisible()
    await publishNow(page)
    // The sheet closes; after the Undo window the queue picks the post up (its scheduler runs every few seconds).
    await expect(s).toBeHidden()
    await expect.poll(async () => (await getPost(api, post.id)).status, { timeout: 20_000 }).toBe('publishing')
    await page.goto(`/posts/${post.id}`)
    await expect(s.getByText('Publishing', { exact: true }).first()).toBeVisible()
    // Publishing: locked; Publish now is gone until it settles.
    await expect(s).toContainText('Going out now')
    await expect(actions(page).getByRole('button', { name: 'Publish now' })).toHaveCount(0)
    await api.waitRequested(post.id)
    await api.result(post.id, col.channel.id, { status: 'published', url: 'https://example.com/live' })
    await expect(s.getByText('Published', { exact: true }).first()).toBeVisible({ timeout: 20_000 })
    await expect((await channelList(page)).getByTestId('post-target').getByRole('link')).toHaveAttribute(
      'href',
      'https://example.com/live',
    )
  })

  test('schedule from the sheet, move it, clear it; a post without channels cannot be published', async ({
    page,
    api,
  }) => {
    const col = await freshCollection(api, { posts: 1 })
    const post = col.posts[0]!

    // The time is picked from the calendar half of the footer's Publish now; Schedule there saves it.
    await page.goto(`/posts/${post.id}`)
    const when = daysAhead(2, 10)

    await pickDateTime(page, sheet(page), when, 'Schedule')
    await expect(page.getByText(/is scheduled/)).toBeVisible()
    await expect.poll(async () => (await getPost(api, post.id)).status).toBe('scheduled')
    expect(new Date((await getPost(api, post.id)).publish_at!).getTime()).toBe(when.getTime())
    await dismissToasts(page)

    // Move it: the sheet shows the time, Update saves the new one.
    await page.goto(`/posts/${post.id}`)
    await expect(sheet(page).getByText('Scheduled', { exact: true }).first()).toBeVisible()
    await expect(sheet(page)).toContainText('Goes out')
    const later = daysAhead(3, 16)

    await pickDateTime(page, sheet(page), later, 'Schedule')
    await expect.poll(async () => new Date((await getPost(api, post.id)).publish_at!).getTime()).toBe(later.getTime())
    await dismissToasts(page)

    // Clear it: without a time, Save changes makes it a draft again.
    await page.goto(`/posts/${post.id}`)
    await sheet(page).getByRole('button', { name: 'Clear schedule' }).click()
    await expect(sheet(page).getByTestId('publish-at-chip')).toHaveCount(0)
    await sheet(page).getByRole('button', { name: 'Save changes' }).click()
    await expect.poll(async () => (await getPost(api, post.id)).status).toBe('draft')
    expect((await getPost(api, post.id)).publish_at).toBeNull()

    // No channels: nothing to publish to, neither from the header nor from the footer.
    const lonely = await api.createPost(col.id, { content: 'No channels here' })

    await page.goto(`/posts/${lonely.id}`)
    await expect(sheet(page).getByRole('button', { name: 'Channels' })).toContainText('Pick where the post goes')
    await expect(sheet(page).getByTestId('publish-now')).toBeDisabled()
    await expect(actions(page).getByRole('button', { name: 'Publish now' })).toHaveCount(0)
  })

  test('Edit: the composer with the post, a save, a 409 from another writer; Duplicate; Delete', async ({
    page,
    api,
  }) => {
    const col = await freshCollection(api, { posts: 1 })
    const post = col.posts[0]!

    // The sheet is the composer: the post opens ready to edit.
    await page.goto(`/posts/${post.id}`)
    const composer = sheet(page)

    await expect(composer.getByTestId('post-content')).toContainText(titleOf(post.content))
    await expect(composer.getByTestId('post-content')).toHaveAttribute('contenteditable', 'true')
    // Someone else edits meanwhile: the save is a clear conflict, nothing is lost.
    await api.patch(`/v1/posts/${post.id}`, { content: 'Edited elsewhere' })
    await composer.getByTestId('post-content').fill('Edited in the UI')
    await composer.getByRole('button', { name: 'Save draft' }).click()
    await expect(page.getByText(/modified|changed|conflict/i).first()).toBeVisible()
    await expect(composer).toBeVisible()
    await expect(composer.getByTestId('post-content')).toContainText('Edited in the UI')
    expect((await getPost(api, post.id)).content).toBe('Edited elsewhere')
    await composer.getByRole('button', { name: 'Close' }).first().click()
    await expect(composer).toBeHidden()

    // Opened again it shows the other writer's version; a new edit on top of it saves and closes the sheet.
    await page.goto(`/posts/${post.id}`)
    await expect(sheet(page).getByTestId('post-content')).toContainText('Edited elsewhere')
    await sheet(page).getByTestId('post-content').fill('Edited in the UI, second try')
    await sheet(page).getByRole('button', { name: 'Save draft' }).click()
    await expect(page.getByText(/was saved/)).toBeVisible()
    await expect(page).toHaveURL(/\/posts(\?|$)/)
    await page.goto(`/posts/${post.id}`)
    await expect.poll(async () => (await getPost(api, post.id)).content).toBe('Edited in the UI, second try')
    await dismissToasts(page)

    // Duplicate: a new draft in the same collection, announced by a toast and opened.
    await actions(page).getByRole('button', { name: 'Duplicate' }).click()
    await expect(page.getByText(/was duplicated as a draft/)).toBeVisible()
    const others = async () =>
      (await api.get<{ data: { id: string }[] }>(`/v1/collections/${col.id}/posts`)).data.filter(
        (p) => p.id !== post.id,
      )

    await expect.poll(async () => (await others()).length).toBe(1)
    const copyId = (await others())[0]!.id

    await page.goto(`/posts/${copyId}`)
    // The copy keeps the title and the edited text.
    await expect(sheet(page).getByRole('heading', { name: titleOf(post.content) })).toBeVisible()
    await expect(sheet(page).getByTestId('post-content')).toContainText('Edited in the UI, second try')
    await expect(sheet(page).getByText('Draft', { exact: true }).first()).toBeVisible()

    // Delete the copy.
    await actions(page).getByRole('button', { name: 'Delete' }).click()
    const confirm = page.getByRole('alertdialog')

    await expect(confirm).toContainText('It will not be published.')
    await confirm.getByRole('button', { name: 'Delete' }).click()
    await expect(page).toHaveURL(/\/posts(\?|$)/)
    expect((await api.raw('GET', `/v1/posts/${copyId}`)).status).toBe(404)
  })

  test('a post that does not exist explains itself', async ({ page }) => {
    await page.goto('/posts/post_doesnotexist000000')
    await expect(page.getByRole('dialog')).toContainText(/not found/i)
    await expect(page.getByRole('dialog').getByRole('button', { name: /Try again/ })).toBeVisible()
  })
})
