import type { Page } from '@playwright/test'

import { daysAhead, freshCollection, getPost, titleOf } from '../../lib/data.js'
import { expect, ready, test } from '../../lib/fixtures.js'
import { pickDateTime } from '../../lib/pick.js'

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

    await expect(list.filter({ hasText: 'Posted yesterday' })).toContainText('Published')
    await expect(list.filter({ hasText: 'Upload failed' })).toContainText('Failed')
    await expect(list.filter({ hasText: 'Beach day' })).toContainText('Scheduled')
    await expect(list.filter({ hasText: 'Draft carousel' })).toContainText('Draft')
    // A row names its channels for the screen reader.
    await expect(
      list.filter({ hasText: 'Posted yesterday' }).getByRole('img', { name: /Company blog/ }),
    ).toHaveAttribute('aria-label', /Brand social/)
    // Status filter
    await page.getByRole('combobox', { name: 'Status filter' }).click()
    await page.getByRole('option', { name: 'Failed' }).click()
    await expect(page).toHaveURL(/status=failed/)
    await expect(list).toHaveCount(1)
    await expect(list.first()).toContainText('Upload failed')
    // Search
    await page.getByRole('combobox', { name: 'Status filter' }).click()
    await page.getByRole('option', { name: 'Any status' }).click()
    await page.getByRole('textbox', { name: 'Search posts' }).fill('coffee')
    await expect(list).toHaveCount(1)
    await expect(list.first()).toContainText('Slow morning, good coffee')
    // The search is read from the address too.
    await page.goto(`/posts?collection=${s.collections.instagram}&q=coffee`)
    await expect(rows(page)).toHaveCount(1)
    await expect(page.getByRole('textbox', { name: 'Search posts' })).toHaveValue('coffee')
    // Channel filter: only posts with a target on that channel.
    await page.getByRole('button', { name: 'Clear' }).click()
    await page.getByRole('button', { name: 'Channel filter', exact: true }).click()
    await page.getByRole('option', { name: 'Weekly newsletter' }).click()
    await page.keyboard.press('Escape')
    await expect(page.getByText('No posts match')).toBeVisible()
    await page.getByRole('button', { name: 'Clear' }).click()
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
    await rows(page).filter({ hasText: 'Posted yesterday' }).click()
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
    await sheet.getByTestId('post-content').fill('Hello from the composer')
    await expect(sheet.getByRole('button', { name: 'Save draft' })).toBeEnabled()
    // No channel yet: publishing is not offered, a draft is.
    await expect(sheet.getByTestId('publish-now')).toBeDisabled()
    const channel = sheet.getByRole('group', { name: 'Channels' }).getByRole('checkbox', { name: col.channel.name })

    await channel.click()
    await expect(channel).toBeChecked()
    // The character counter per channel and the tab for that channel's own text appear.
    await sheet.getByRole('button', { name: 'Characters per channel' }).first().click()
    await expect(page.getByTestId('counters')).toContainText(col.channel.name)
    await page.keyboard.press('Escape')
    await expect(sheet.getByRole('tab', { name: `Text for ${col.channel.name}` })).toBeVisible()
    await sheet.getByRole('button', { name: 'Save draft' }).click()
    await expect(sheet).toBeHidden()
    const row = rows(page).filter({ hasText: 'Hello from the composer' })

    await expect(row).toContainText('Draft')
    const created = (
      await api.get<{ data: { id: string; status: string; targets: unknown[] }[] }>(`/v1/collections/${col.id}/posts`)
    ).data[0]!

    expect(created.status).toBe('draft')
    expect(created.targets).toHaveLength(1)

    // Schedule one: a time turns the primary button into Schedule.
    await page.getByTestId('new-post').click()
    await sheet.getByTestId('post-content').fill('Scheduled from the composer')
    await sheet.getByRole('group', { name: 'Channels' }).getByRole('checkbox', { name: col.channel.name }).click()
    const when = daysAhead(2, 11)

    await pickDateTime(page, sheet, when)
    await expect(sheet.getByTestId('schedule-post')).toBeVisible()
    await expect(sheet.getByTestId('publish-now')).toHaveCount(0)
    await sheet.getByTestId('schedule-post').click()
    await expect(sheet).toBeHidden()
    await expect(rows(page).filter({ hasText: 'Scheduled from the composer' })).toContainText('Scheduled')
    const scheduled = (
      await api.get<{ data: { content: string; status: string; publish_at: string }[] }>(
        `/v1/collections/${col.id}/posts?status=scheduled`,
      )
    ).data[0]!

    expect(scheduled.content).toBe('Scheduled from the composer')
    expect(new Date(scheduled.publish_at).getTime()).toBe(when.getTime())

    // Publish now: the post goes to the automation.
    await page.getByTestId('new-post').click()
    await sheet.getByTestId('post-content').fill('Published from the composer')
    await sheet.getByRole('group', { name: 'Channels' }).getByRole('checkbox', { name: col.channel.name }).click()
    await sheet.getByTestId('publish-now').click()
    await expect(sheet).toBeHidden()
    await expect(rows(page).filter({ hasText: 'Published from the composer' })).toContainText('Publishing')
    const publishing = (await api.get<{ data: { id: string }[] }>(`/v1/collections/${col.id}/posts?status=publishing`))
      .data[0]!

    await api.waitRequested(publishing.id)
    await api.result(publishing.id, col.channel.id, { status: 'published', url: 'https://example.com/ui/1' })
    // The list updates on its own.
    await expect(rows(page).filter({ hasText: 'Published from the composer' })).toContainText('Published', {
      timeout: 20_000,
    })
  })

  test('per-channel text and a disabled channel in the composer', async ({ page, api, srv }) => {
    const col = await freshCollection(api, { posts: 0 })

    await page.goto(`/posts?collection=${col.id}`)
    await page.getByTestId('new-post').click()
    const sheet = page.getByRole('dialog', { name: 'New post' })

    // A disabled channel cannot be picked.
    await expect(
      sheet.getByRole('group', { name: 'Channels' }).getByRole('checkbox', { name: 'Old account' }),
    ).toBeDisabled()
    await sheet.getByTestId('post-content').fill('Same text everywhere')
    await sheet.getByRole('group', { name: 'Channels' }).getByRole('checkbox', { name: col.channel.name }).click()
    await sheet.getByRole('group', { name: 'Channels' }).getByRole('checkbox', { name: 'Company blog' }).click()
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
    await expect(
      page.getByRole('navigation', { name: 'Collections' }).getByRole('link', { name: new RegExp(col.name) }),
    ).toContainText('1', { timeout: 20_000 })
    expect((await getPost(api, col.posts[0]!.id)).status).toBe('scheduled')
    expect(titleOf(col.posts[0]!.content)).toBe(`${col.name} post 1`)
  })
})
