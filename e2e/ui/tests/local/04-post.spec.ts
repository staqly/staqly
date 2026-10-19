import type { Page } from '@playwright/test'

import { daysAhead, freshCollection, getPost, titleOf } from '../../lib/data.js'
import { dismissToasts, expect, test } from '../../lib/fixtures.js'
import { pickDateTime } from '../../lib/pick.js'

const sheet = (page: Page) => page.getByRole('dialog').first()
const actions = (page: Page) => page.getByTestId('post-actions')

test.describe('the post', () => {
  test('a published post: content, picture, tags, the channels with their links; it cannot be edited or re-published', async ({
    page,
    srv,
  }) => {
    await page.goto(`/posts/${srv.seed.posts.published}`)
    const s = sheet(page)

    await expect(s.getByRole('heading', { name: 'Posted yesterday' })).toBeVisible()
    await expect(s.getByText('Published', { exact: true }).first()).toBeVisible()
    await expect(s.getByRole('region', { name: 'Content' })).toContainText('Already out.')
    await expect(s.getByRole('region', { name: 'Content' }).getByRole('img')).toBeVisible()
    await expect(s.getByRole('heading', { name: /Channels 2 of 2 published/ })).toBeVisible()
    const targets = s.getByTestId('post-target')

    await expect(targets).toHaveCount(2)
    await expect(targets.first().getByRole('link')).toHaveAttribute('href', 'https://instagram.example/p/abc')
    await expect(targets.first().getByRole('link')).toHaveAttribute('rel', /noopener/)
    // Nothing to publish or move any more; More has Duplicate and Delete but no Edit.
    await expect(actions(page).getByRole('button', { name: 'Publish now' })).toHaveCount(0)
    await actions(page).getByRole('button', { name: 'More actions' }).click()
    await expect(page.getByRole('menuitem', { name: 'Edit' })).toHaveCount(0)
    await expect(page.getByRole('menuitem', { name: 'Duplicate' })).toBeVisible()
    await page.keyboard.press('Escape')
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
    const target = s.getByTestId('post-target').first()

    await expect(target).toContainText('Instagram API: 503 Service Unavailable')
    await target.getByRole('button', { name: 'Retry' }).click()
    await expect(page.getByText(/is being retried/)).toBeVisible()
    await expect(target).toContainText('Publishing')
    await api.waitRequested(post.id)
    await api.result(post.id, col.channel.id, { status: 'published', url: 'https://example.com/retried' })
    await expect(target).toContainText('Published', { timeout: 20_000 })
    await expect(target.getByRole('link')).toHaveAttribute('href', 'https://example.com/retried')
    await expect(s.getByRole('heading', { name: /1 of 1 published/ })).toBeVisible()
    expect(srv.seed.posts.failed).toBeTruthy()
  })

  test('a draft: Publish now hands it to the automation; the sheet follows the result live', async ({ page, api }) => {
    const col = await freshCollection(api, { posts: 1 })
    const post = col.posts[0]!

    await page.goto(`/posts/${post.id}`)
    const s = sheet(page)

    await expect(s.getByText('Draft', { exact: true }).first()).toBeVisible()
    await actions(page).getByRole('button', { name: 'Publish now' }).click()
    await expect(s.getByText('Publishing', { exact: true }).first()).toBeVisible()
    // Publishing: locked; the actions are gone until it settles.
    await expect(actions(page).getByRole('button', { name: 'Publish now' })).toHaveCount(0)
    await api.waitRequested(post.id)
    await api.result(post.id, col.channel.id, { status: 'published', url: 'https://example.com/live' })
    await expect(s.getByText('Published', { exact: true }).first()).toBeVisible({ timeout: 20_000 })
    await expect(s.getByTestId('post-target').getByRole('link')).toHaveAttribute('href', 'https://example.com/live')
  })

  test('schedule from the sheet, move it, clear it; a post without channels cannot be published', async ({
    page,
    api,
  }) => {
    const col = await freshCollection(api, { posts: 1 })
    const post = col.posts[0]!

    await page.goto(`/posts/${post.id}`)
    await actions(page).getByRole('button', { name: 'Schedule' }).click()
    const popover = page.getByRole('dialog').last()

    await expect(popover.getByText('Pick a time')).toBeVisible()
    const when = daysAhead(2, 10)

    await pickDateTime(page, popover, when)
    await popover.getByRole('button', { name: 'Schedule', exact: true }).click()
    await expect(page.getByText(/is scheduled for/)).toBeVisible()
    await expect(sheet(page).getByText('Scheduled', { exact: true }).first()).toBeVisible()
    await expect(sheet(page)).toContainText('Goes out')
    expect(new Date((await getPost(api, post.id)).publish_at!).getTime()).toBe(when.getTime())
    await dismissToasts(page)

    // Move it.
    await actions(page).getByRole('button', { name: 'Reschedule' }).click()
    await expect(page.getByRole('dialog').last().getByText('Move the post')).toBeVisible()
    const later = daysAhead(3, 16)

    await pickDateTime(page, page.getByRole('dialog').last(), later)
    await page.getByRole('dialog').last().getByRole('button', { name: 'Schedule', exact: true }).click()
    await expect.poll(async () => new Date((await getPost(api, post.id)).publish_at!).getTime()).toBe(later.getTime())
    await dismissToasts(page)

    // Clear it: a draft again.
    await actions(page).getByRole('button', { name: 'Reschedule' }).click()
    // The picker's own Clear empties the time; the popover's button then reads Clear schedule.
    const popover2 = page.getByRole('dialog').last()

    await popover2.getByRole('button', { name: 'Clear schedule', exact: true }).first().click()
    await popover2.getByRole('button', { name: 'Clear schedule', exact: true }).last().click()
    await expect(page.getByText(/is a draft again/)).toBeVisible()
    await expect.poll(async () => (await getPost(api, post.id)).status).toBe('draft')

    // No channels: nothing to publish to.
    const lonely = await api.createPost(col.id, { content: 'No channels here' })

    await page.goto(`/posts/${lonely.id}`)
    await expect(sheet(page)).toContainText('This post has no channels')
    await expect(actions(page).getByRole('button', { name: 'Publish now' })).toBeDisabled()
  })

  test('Edit: the composer with the post, a save, a 409 from another writer; Duplicate; Delete', async ({
    page,
    api,
  }) => {
    const col = await freshCollection(api, { posts: 1 })
    const post = col.posts[0]!

    await page.goto(`/posts/${post.id}`)
    await actions(page).getByRole('button', { name: 'More actions' }).click()
    await page.getByRole('menuitem', { name: 'Edit' }).click()
    const composer = page.getByRole('dialog', { name: 'Edit post' })

    await expect(composer.getByTestId('post-content')).toContainText(titleOf(post.content))
    // Someone else edits meanwhile: the save is a clear conflict, nothing is lost.
    await api.patch(`/v1/posts/${post.id}`, { content: 'Edited elsewhere' })
    await composer.getByTestId('post-content').fill('Edited in the UI')
    await composer.getByRole('button', { name: 'Save changes' }).click()
    await expect(page.getByText(/modified|changed|conflict/i).first()).toBeVisible()
    await expect(composer).toBeVisible()
    await expect(composer.getByTestId('post-content')).toContainText('Edited in the UI')
    expect((await getPost(api, post.id)).content).toBe('Edited elsewhere')
    await composer.getByRole('button', { name: 'Close' }).first().click()
    await expect(composer).toBeHidden()

    // The sheet shows the other writer's version; a new edit on top of it saves.
    await expect(sheet(page).getByRole('region', { name: 'Content' })).toContainText('Edited elsewhere')
    await actions(page).getByRole('button', { name: 'More actions' }).click()
    await page.getByRole('menuitem', { name: 'Edit' }).click()
    await composer.getByTestId('post-content').fill('Edited in the UI, second try')
    await composer.getByRole('button', { name: 'Save changes' }).click()
    await expect(composer).toBeHidden()
    await expect.poll(async () => (await getPost(api, post.id)).content).toBe('Edited in the UI, second try')
    await dismissToasts(page)

    // Duplicate: a new draft in the same collection, announced by a toast.
    await actions(page).getByRole('button', { name: 'More actions' }).click()
    await page.getByRole('menuitem', { name: 'Duplicate' }).click()
    await expect(page.getByText(/was duplicated as a draft/)).toBeVisible()
    const others = async () =>
      (await api.get<{ data: { id: string }[] }>(`/v1/collections/${col.id}/posts`)).data.filter(
        (p) => p.id !== post.id,
      )

    await expect.poll(async () => (await others()).length).toBe(1)
    const copyId = (await others())[0]!.id

    await page.goto(`/posts/${copyId}`)
    await expect(sheet(page).getByRole('heading', { name: 'Edited in the UI, second try' })).toBeVisible()
    await expect(sheet(page).getByText('Draft', { exact: true }).first()).toBeVisible()

    // Delete the copy.
    await actions(page).getByRole('button', { name: 'More actions' }).click()
    await page.getByRole('menuitem', { name: 'Delete' }).click()
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
