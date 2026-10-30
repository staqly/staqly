import type { Page } from '@playwright/test'

import { daysAhead, freshCollection, getPost, titleOf } from '../../lib/data.js'
import { expect, ready, test } from '../../lib/fixtures.js'
import { channelOption, pickChannels, pickDateTime } from '../../lib/pick.js'

const rows = (page: Page) => page.getByTestId('posts-list').getByTestId('post-row')

test.describe('posts', () => {
  test('the collection pane and the open collection: rows with status and channels, filters in the URL', async ({
    page,
    srv,
  }) => {
    const s = srv.seed

    await page.goto(`/posts?collection=${s.collections.instagram}`)
    await ready(page, 'Instagram posts')
    const pane = page.getByRole('navigation', { name: 'Collections' })

    await expect(pane.getByRole('link', { name: /Instagram posts/ })).toHaveAttribute('aria-current', 'page')
    // Archived collections are not in the list until the switch is on.
    await expect(pane.getByRole('link', { name: /Old campaigns/ })).toHaveCount(0)
    const list = rows(page)

    // Upcoming posts first; the ones that went out in their own section under them.
    await expect(
      page.getByTestId('posts-upcoming').getByTestId('post-row').filter({ hasText: 'Beach day' }),
    ).toContainText('Scheduled')
    await expect(
      page.getByTestId('posts-published').getByTestId('post-row').filter({ hasText: 'Posted yesterday' }),
    ).toContainText('Published')
    await expect(list.filter({ hasText: 'Upload failed' })).toContainText('Failed')
    await expect(list.filter({ hasText: 'Beach day' })).toContainText('Scheduled')
    await expect(list.filter({ hasText: 'Draft carousel' })).toContainText('Draft')
    // A row names its channels for the screen reader.
    await expect(
      list.filter({ hasText: 'Posted yesterday' }).getByRole('img', { name: /Company blog/ }),
    ).toHaveAttribute('aria-label', /Brand social/)
    // Status filter, behind the Filter button; a filter in use shows as a chip under the title.
    await page.getByTestId('filter-button').click()
    await page.getByRole('combobox', { name: 'Status filter' }).click()
    await page.getByRole('option', { name: 'Failed' }).click()
    await expect(page).toHaveURL(/status=failed/)
    await expect(list).toHaveCount(1)
    await expect(list.first()).toContainText('Upload failed')
    await page.keyboard.press('Escape')
    await page.getByRole('button', { name: 'Remove filter Failed' }).click()
    // Search
    await page.getByTestId('filter-button').click()
    await page.getByRole('textbox', { name: 'Search posts' }).fill('coffee')
    await expect(list).toHaveCount(1)
    await expect(list.first()).toContainText('Slow morning, good coffee')
    await page.keyboard.press('Escape')
    // The search is read from the address too.
    await page.goto(`/posts?collection=${s.collections.instagram}&q=coffee`)
    await expect(rows(page)).toHaveCount(1)
    await expect(page.getByTestId('post-filters')).toContainText('coffee')
    // Channel filter: only posts with a target on that channel; Clear all takes every filter off.
    await page.getByTestId('filter-button').click()
    await expect(page.getByRole('textbox', { name: 'Search posts' })).toHaveValue('coffee')
    await page.getByRole('button', { name: 'Clear all' }).click()
    await page.getByRole('button', { name: /^Channel filter:/ }).click()
    await page.getByRole('option', { name: 'Weekly newsletter' }).click()
    await page.keyboard.press('Escape')
    await page.keyboard.press('Escape')
    await expect(page.getByText('No posts match')).toBeVisible()
    await page.getByRole('button', { name: 'Remove filter Weekly newsletter' }).click()
    await expect(rows(page).first()).toBeVisible()

    // Show archived collections
    await pane.getByRole('switch', { name: 'Show archived' }).click()
    await expect(pane.getByRole('link', { name: /Old campaigns/ })).toBeVisible()
    await expect(page).toHaveURL(/archived=1/)
    await pane.getByRole('link', { name: /Old campaigns/ }).click()
    await ready(page, 'Old campaigns')
    await expect(page.getByTestId('new-post')).toBeDisabled()
  })

  test('a row opens the post as a drawer with the id in the URL; Escape goes back to the list', async ({
    page,
    srv,
  }) => {
    await page.goto(`/posts?collection=${srv.seed.collections.instagram}`)
    await ready(page, 'Instagram posts')
    // A published post, in the Published section. (Its title link: rows below the fold skip rendering until
    // scrolled to, so a click in the same frame as the scroll would land on the bare row.)
    await rows(page).filter({ hasText: 'Posted yesterday' }).getByTestId('post-link').click()
    await expect(page).toHaveURL(new RegExp(`/posts/${srv.seed.posts.published}`))
    const sheet = page.getByRole('dialog')

    await expect(sheet.getByRole('heading', { name: 'Posted yesterday' })).toBeVisible()
    await expect(sheet.getByTestId('post-target')).toHaveCount(2)
    await page.keyboard.press('Escape')
    await expect(page).toHaveURL(/\/posts(\?|$)/)
    await expect(sheet).toBeHidden()
  })

  test('new post: validation, pick channels, counters, save a draft; then schedule and publish now', async ({
    page,
    api,
  }) => {
    const col = await freshCollection(api, { posts: 0 })

    await page.goto(`/posts?collection=${col.id}`)
    await ready(page, col.name)
    await expect(page.getByText('No posts yet').first()).toBeVisible()
    await page.getByTestId('new-post').click()
    const sheet = page.getByRole('dialog', { name: 'New post' })

    await expect(sheet).toBeVisible()
    // The collection is preselected; without text nothing can be saved.
    await expect(sheet.getByRole('combobox', { name: 'Collection' })).toContainText(col.name)
    await expect(sheet.getByRole('button', { name: 'Save draft' })).toBeDisabled()
    await expect(sheet.getByTestId('publish-now')).toBeDisabled()
    await sheet.getByTestId('post-title').fill('Composer draft')
    await sheet.getByTestId('post-content').fill('Hello from the composer')
    await expect(sheet.getByRole('button', { name: 'Save draft' })).toBeEnabled()
    // No channel yet: publishing is not offered, a draft is.
    await expect(sheet.getByTestId('publish-now')).toBeDisabled()
    await pickChannels(page, sheet, col.channel.name)
    // The picked channel shows in the field, and publishing is offered now.
    await expect(sheet.getByRole('button', { name: 'Channels' })).toContainText(col.channel.name)
    await expect(sheet.getByTestId('publish-now')).toBeEnabled()
    // The character counter names the channel; with one channel there is no per-channel tab (its text is the post's).
    await sheet.getByRole('button', { name: 'Characters per channel' }).first().click()
    await expect(page.getByTestId('counters')).toContainText(col.channel.name)
    await page.keyboard.press('Escape')
    await expect(sheet.getByRole('tab', { name: `Text for ${col.channel.name}` })).toHaveCount(0)
    await sheet.getByRole('button', { name: 'Save draft' }).click()
    await expect(sheet).toBeHidden()
    const row = rows(page).filter({ hasText: 'Composer draft' })

    await expect(row).toContainText('Draft')
    const created = (
      await api.get<{ data: { id: string; status: string; targets: unknown[] }[] }>(`/v1/collections/${col.id}/posts`)
    ).data[0]!

    expect(created.status).toBe('draft')
    expect(created.targets).toHaveLength(1)

    // Schedule one: Schedule in the calendar popover sends it.
    await page.getByTestId('new-post').click()
    await sheet.getByTestId('post-title').fill('Composer scheduled')
    await sheet.getByTestId('post-content').fill('Scheduled from the composer')
    await pickChannels(page, sheet, col.channel.name)
    const when = daysAhead(2, 11)

    await pickDateTime(page, sheet, when, 'Schedule')
    await expect(sheet).toBeHidden()
    await expect(rows(page).filter({ hasText: 'Composer scheduled' })).toContainText('Scheduled')
    const scheduled = (
      await api.get<{ data: { content: string; status: string; publish_at: string }[] }>(
        `/v1/collections/${col.id}/posts?status=scheduled`,
      )
    ).data[0]!

    expect(scheduled.content).toBe('Scheduled from the composer')
    expect(new Date(scheduled.publish_at).getTime()).toBe(when.getTime())

    // Publish now: the post goes to the automation.
    await page.getByTestId('new-post').click()
    await sheet.getByTestId('post-title').fill('Composer publish')
    await sheet.getByTestId('post-content').fill('Published from the composer')
    await pickChannels(page, sheet, col.channel.name)
    await sheet.getByTestId('publish-now').click()
    await expect(sheet).toBeHidden()
    // After the Undo window the queue picks it up (its scheduler runs every few seconds).
    let publishing = { id: '' }

    await expect
      .poll(
        async () => {
          publishing = (await api.get<{ data: { id: string }[] }>(`/v1/collections/${col.id}/posts?status=publishing`))
            .data[0] ?? { id: '' }

          return publishing.id
        },
        { timeout: 20_000 },
      )
      .not.toBe('')

    await api.waitRequested(publishing.id)
    await api.result(publishing.id, col.channel.id, { status: 'published', url: 'https://example.com/ui/1' })
    // The list updates on its own.
    await expect(rows(page).filter({ hasText: 'Composer publish' })).toContainText('Published', {
      timeout: 20_000,
    })
  })

  test('per-channel text and a disabled channel in the composer', async ({ page, api, srv }) => {
    const col = await freshCollection(api, { posts: 0 })

    await page.goto(`/posts?collection=${col.id}`)
    await page.getByTestId('new-post').click()
    const sheet = page.getByRole('dialog', { name: 'New post' })

    // A disabled channel is listed, says why, and cannot be picked.
    await sheet.getByRole('button', { name: 'Channels' }).click()
    const old = channelOption(page, 'Old account')

    await expect(old).toHaveAttribute('aria-disabled', 'true')
    await expect(old).toContainText('turned off')
    await page.keyboard.press('Escape')
    await sheet.getByTestId('post-title').fill('Same text')
    await sheet.getByTestId('post-content').fill('Same text everywhere')
    await pickChannels(page, sheet, col.channel.name, 'Company blog')
    // From two channels on, each gets a tab for a text of its own.
    await sheet.getByTestId(`tab-channel-${srv.seed.channels.blog}`).click()
    await expect(sheet.getByText('Company blog uses the text for all channels.')).toBeVisible()
    await sheet.getByTestId(`override-${srv.seed.channels.blog}`).fill('A longer version for the blog')
    await expect(sheet.getByText('Company blog gets its own text.')).toBeVisible()
    await sheet.getByRole('button', { name: 'Save draft' }).click()
    await expect(sheet).toBeHidden()
    const post = (
      await api.get<{ data: { targets: { channel_id: string; content: string | null }[] }[] }>(
        `/v1/collections/${col.id}/posts`,
      )
    ).data[0]!

    expect(post.targets.find((t) => t.channel_id === srv.seed.channels.blog)?.content).toBe(
      'A longer version for the blog',
    )
    expect(post.targets.find((t) => t.channel_id === col.channel.id)?.content).toBeNull()
  })

  test('a post created over REST appears live; the pane counts the scheduled ones', async ({ page, api }) => {
    const col = await freshCollection(api, { posts: 1 })

    await page.goto(`/posts?collection=${col.id}`)
    await expect(rows(page)).toHaveCount(1)
    await api.createPost(col.id, { content: 'Arrived while you looked', targets: [{ channel_id: col.channel.id }] })
    await expect(rows(page).filter({ hasText: 'Arrived while you looked' })).toBeVisible({ timeout: 20_000 })
    await api.schedule(col.posts[0]!.id, daysAhead(1, 12).toISOString())
    // The pane counts every post; its hover names the scheduled ones.
    const pane = page.getByRole('navigation', { name: 'Collections' }).getByRole('link', { name: new RegExp(col.name) })

    await expect(pane).toContainText('2', { timeout: 20_000 })
    await expect(pane.locator('[title$="1 scheduled"]')).toBeAttached({ timeout: 20_000 })
    expect((await getPost(api, col.posts[0]!.id)).status).toBe('scheduled')
    expect(titleOf(col.posts[0]!.content)).toBe(`${col.name} post 1`)
  })
})
