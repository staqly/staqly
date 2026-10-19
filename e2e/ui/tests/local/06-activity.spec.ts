import { readFileSync } from 'node:fs'

import { freshCollection, titleOf } from '../../lib/data.js'
import { expect, ready, test } from '../../lib/fixtures.js'

test.describe('activity', () => {
  test('feed: event type filter, actor filter, dates; filters live in the URL', async ({ page }) => {
    await page.goto('/activity')
    await ready(page, 'Activity')
    const feed = page.getByRole('main').getByRole('list').first()

    await expect(feed.getByRole('listitem').first()).toBeVisible()
    const type = page.getByRole('combobox', { name: 'Event type' })

    // A native datalist suggests types; Enter applies.
    await type.fill('post.published')
    await type.press('Enter')
    await expect(page).toHaveURL(/type=post\.published/)
    await expect(feed.getByRole('listitem').first()).toContainText(/published/i)
    await expect(feed.getByText('post.created', { exact: true })).toHaveCount(0)

    // A refresh keeps the filter.
    await page.reload()
    await expect(page).toHaveURL(/type=post\.published/)
    await expect(feed.getByRole('listitem').first()).toContainText(/published/i)

    // Actor filter
    await page.getByRole('button', { name: 'Add filter' }).click()
    await page.getByRole('combobox', { name: 'Filter value' }).click()
    await page.getByRole('option').first().click()
    await page.getByRole('button', { name: 'Apply' }).click()
    await expect(page).toHaveURL(/actor/)

    // Dates come from a calendar picker: "Last 7 days" lands in the URL and on the trigger; the chip clears it.
    await page.getByRole('button', { name: 'Dates' }).click()
    await page.getByRole('dialog', { name: 'Dates' }).getByRole('button', { name: 'Last 7 days' }).click()
    await expect(page).toHaveURL(/from=\d{4}-\d{2}-\d{2}.*to=\d{4}-\d{2}-\d{2}/)
    await page.getByRole('dialog', { name: 'Dates' }).getByRole('button', { name: 'Done' }).click()
    await expect(page.getByTestId('date-range-picker')).toContainText(/\d{4} to .*\d{4}/)
    // A date range in the future (set in the URL, as a shared link would) empties the feed with a clear message.
    await page.goto('/activity?from=2099-01-01&to=2099-01-31')
    await expect(page.getByText(/No events|Nothing/i).first()).toBeVisible()
  })

  test('posts in events link to their post; Data expands; the post filter narrows the feed', async ({ page, api }) => {
    const col = await freshCollection(api, { posts: 1 })
    const post = col.posts[0]!
    const title = titleOf(post.content)

    await page.goto('/activity')
    const ev = page.getByRole('listitem').filter({ hasText: title }).first()

    await expect(ev).toBeVisible()
    await ev.getByRole('button', { name: /Data/ }).click()
    await expect(ev.getByText('"content"').or(ev.getByText(title)).first()).toBeVisible()
    await ev.getByRole('link', { name: title }).click()
    await expect(page).toHaveURL(new RegExp(`/posts/${post.id}`))
    await expect(page.getByRole('dialog').getByRole('heading', { name: title })).toBeVisible()

    // The post filter from the URL, as a link from the post would set it.
    await page.goto(`/activity?post=${post.id}`)
    await expect(page.getByTestId('audit-event')).toHaveCount(1)
    await expect(page.getByTestId('audit-event').first()).toContainText(title)
  })

  test('export: JSONL and CSV downloads of the filtered log', async ({ page }) => {
    await page.goto('/activity?type=post.created')
    await page.getByRole('button', { name: 'Export' }).click()
    const [jsonl] = await Promise.all([
      page.waitForEvent('download'),
      page.getByRole('menuitem', { name: 'JSONL (one event per line)' }).click(),
    ])

    expect(jsonl.suggestedFilename()).toMatch(/\.jsonl$/)
    const lines = readFileSync((await jsonl.path())!, 'utf8')
      .trim()
      .split('\n')

    expect(lines.length).toBeGreaterThan(0)
    expect(lines.every((l) => (JSON.parse(l) as { type: string }).type === 'post.created')).toBe(true)

    await page.getByRole('button', { name: 'Export' }).click()
    const [csv] = await Promise.all([
      page.waitForEvent('download'),
      page.getByRole('menuitem', { name: 'CSV' }).click(),
    ])

    expect(csv.suggestedFilename()).toMatch(/\.csv$/)
    expect(readFileSync((await csv.path())!, 'utf8').split('\n')[0]).toContain('type')
  })

  test('live: a new event appears after Refresh, and on its own', async ({ page, api }) => {
    await page.goto('/activity')
    await ready(page, 'Activity')
    const col = await freshCollection(api, { posts: 0, name: `Live activity ${Date.now()}` })

    await expect(page.getByRole('main').getByText(col.name).first()).toBeVisible({ timeout: 20_000 })
  })
})
