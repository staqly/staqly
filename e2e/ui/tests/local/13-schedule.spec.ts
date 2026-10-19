import type { Page } from '@playwright/test'

import { dayKey, daysAhead, freshCollection, getPost, titleOf } from '../../lib/data.js'
import { dismissToasts, expect, postOpen, ready, test } from '../../lib/fixtures.js'

/** HTML5 drag and drop through the real events with one shared DataTransfer, as a mouse drag produces. */
async function drag(page: Page, from: ReturnType<Page['locator']>, to: ReturnType<Page['locator']>) {
  const dt = await page.evaluateHandle(() => new DataTransfer())

  await from.dispatchEvent('dragstart', { dataTransfer: dt })
  await to.dispatchEvent('dragover', { dataTransfer: dt })
  // No dragend: after the drop the entry has already moved to its new cell.
  await to.dispatchEvent('drop', { dataTransfer: dt })
}

const entry = (page: Page, id: string) => page.locator(`[data-testid="schedule-entry"][data-post-id="${id}"]`)

/** Walks the week view forward until the day is on screen (a post two days ahead may be next week). */
async function showWeekOf(page: Page, day: Date) {
  const key = dayKey(day)

  await expect(page.getByTestId('schedule-grid')).toBeVisible()
  await expect(page.locator('[data-testid^="schedule-slot-"]').first()).toBeVisible()

  for (let i = 0; i < 3 && (await page.getByTestId(`schedule-slot-${key}-12`).count()) === 0; i++) {
    await page.getByRole('button', { name: 'Next week' }).click()
  }

  await expect(page.getByTestId(`schedule-slot-${key}-12`)).toBeVisible()
}

test.describe('the schedule', () => {
  test('home: the week with the seeded posts, the count, day/week/month and list views, schedule runs as chips', async ({
    page,
    srv,
  }) => {
    await page.goto('/')
    await ready(page, 'Schedule')
    await expect(page.getByTestId('schedule-count')).toHaveText(/\d+ posts? scheduled/)
    const tomorrow = daysAhead(1, 10)

    await showWeekOf(page, tomorrow)
    const beach = entry(page, srv.seed.posts.scheduledTomorrow!)

    await expect(beach).toBeVisible()
    await expect(beach).toHaveAttribute('data-state', 'scheduled')
    await expect(beach).toContainText('10:00')
    await expect(beach).toContainText('Beach day')
    await expect(beach).toContainText('Instagram posts')
    // The channels as a stack of avatars, named for the screen reader.
    await expect(beach.getByRole('img', { name: /Company blog/ })).toHaveAttribute('aria-label', /Brand social/)
    // The collection schedules' runs are hollow chips in their slots.
    await expect(page.getByTestId('schedule-slot-chip').first()).toBeVisible()
    await expect(page.getByTestId('schedule-slot-chip').filter({ hasText: 'Weekday mornings' }).first()).toContainText(
      'next post',
    )
    // The "now" line sits on today's grid.
    await page.getByRole('button', { name: 'Today' }).click()
    await expect(page.getByTestId('now-line')).toBeAttached()

    // Day view: one column, the range names the day.
    await page.getByRole('radio', { name: 'Day' }).click()
    await expect(page).toHaveURL(/view=day/)
    await expect(page.getByTestId('schedule-range')).toContainText(new Date().getFullYear().toString())
    await page.getByRole('button', { name: 'Next day' }).click()
    await expect(entry(page, srv.seed.posts.scheduledTomorrow!)).toBeVisible()
    // Month view: the posts are in their day cells.
    await page.getByRole('radio', { name: 'Month' }).click()
    await expect(page).toHaveURL(/view=month/)
    await expect(page.getByTestId(`schedule-day-${dayKey(new Date())}`)).toBeVisible()

    if ((await page.getByTestId(`schedule-day-${dayKey(tomorrow)}`).count()) === 0) {
      await page.getByRole('button', { name: 'Next month' }).click()
    }

    await expect(
      page
        .getByTestId(`schedule-day-${dayKey(tomorrow)}`)
        .locator(`[data-post-id="${srv.seed.posts.scheduledTomorrow}"]`),
    ).toBeVisible()
    // List layout: the same posts as rows.
    await page.getByRole('radio', { name: 'Week' }).click()
    await page.getByRole('radio', { name: 'List' }).click()
    await expect(page).toHaveURL(/list=1/)
    await showWeekOf(page, tomorrow).catch(() => undefined)
    await expect(page.getByTestId('schedule-list')).toContainText('Beach day')
    await page.getByRole('radio', { name: 'Calendar' }).click()
    await expect(page.getByTestId('schedule-grid')).toBeVisible()
  })

  test('an entry opens the post; drag it to another hour; the + on a slot schedules a draft or writes a post', async ({
    page,
    api,
  }) => {
    test.setTimeout(120_000)
    const col = await freshCollection(api, { posts: 3 })
    const scheduled = col.posts[0]!
    const draft = col.posts[1]!
    const written = col.posts[2]!
    const when = daysAhead(2, 10)

    await api.schedule(scheduled.id, when.toISOString())
    await page.goto('/schedule')
    await ready(page, 'Schedule')
    await showWeekOf(page, when)
    const e = entry(page, scheduled.id)

    await expect(e).toBeVisible()
    await e.click()
    await postOpen(page, titleOf(scheduled.content))
    await page.keyboard.press('Escape')
    await expect(page.getByRole('dialog')).toBeHidden()

    // Drag to 14:00 the same day: the post is rescheduled.
    const target = page.getByTestId(`schedule-slot-${dayKey(when)}-14`)

    await drag(page, e, target)
    await expect(target.getByTestId('schedule-entry')).toHaveCount(1)
    await expect.poll(async () => new Date((await getPost(api, scheduled.id)).publish_at!).getHours()).toBe(14)
    await dismissToasts(page)
    // A past slot is not a drop target and has no +.
    const yesterday = daysAhead(-1, 9)

    await page.getByRole('button', { name: 'Today' }).click()

    if ((await page.getByTestId(`schedule-slot-${dayKey(yesterday)}-9`).count()) > 0) {
      await expect(page.getByTestId(`schedule-slot-${dayKey(yesterday)}-9`)).toHaveAttribute('data-past', 'true')
      await expect(page.getByTestId(`schedule-slot-${dayKey(yesterday)}-9`).getByTestId('slot-add')).toHaveCount(0)
    }

    // + on a future slot: pick a waiting draft for that time.
    await showWeekOf(page, when)
    const slot = page.getByTestId(`schedule-slot-${dayKey(when)}-16`)

    await slot.hover()
    // The add button shows on focus (a click on the cell's empty space does the same).
    await slot.getByTestId('slot-add').focus()
    await page.keyboard.press('Enter')
    const dialog = page.getByRole('dialog')

    await expect(dialog).toContainText('Pick a draft to publish at this time')
    await dialog.getByRole('textbox', { name: 'Search drafts' }).fill(titleOf(draft.content))
    await dialog
      .getByTestId('slot-drafts')
      .getByRole('button', { name: new RegExp(titleOf(draft.content)) })
      .click()
    await expect(dialog).toBeHidden()
    await expect.poll(async () => (await getPost(api, draft.id)).status).toBe('scheduled')
    expect(new Date((await getPost(api, draft.id)).publish_at!).getHours()).toBe(16)
    // The grid re-reads its range on a reload.
    await page.reload()
    await showWeekOf(page, when)
    await expect(slot.getByTestId('schedule-entry')).toHaveCount(1)
    await dismissToasts(page)

    // + then "New post for this time": the composer with the time set; Schedule puts it in the slot.
    const slot2 = page.getByTestId(`schedule-slot-${dayKey(when)}-18`)

    await slot2.hover()
    // The add button shows on focus (a click on the cell's empty space does the same).
    await slot2.getByTestId('slot-add').focus()
    await page.keyboard.press('Enter')
    await page.getByRole('dialog').getByRole('button', { name: 'New post for this time' }).click()
    const composer = page.getByRole('dialog', { name: 'New post' })

    await expect(composer.getByTestId('date-time-picker')).toContainText(/\d/)
    await composer.getByRole('combobox', { name: 'Collection' }).click()
    await page.getByRole('option', { name: col.name }).click()
    await composer.getByTestId('post-content').fill('Written from the calendar')
    await composer.getByRole('group', { name: 'Channels' }).getByRole('checkbox', { name: col.channel.name }).click()
    await composer.getByTestId('schedule-post').click()
    await expect(composer).toBeHidden()
    await expect(slot2.getByTestId('schedule-entry').filter({ hasText: 'Written from the calendar' })).toBeVisible()
    expect(written.id).toBeTruthy()
  })

  test('entries follow the post: publishing, published and failed states; an empty week says so', async ({
    page,
    api,
  }) => {
    const col = await freshCollection(api, { posts: 2 })
    const a = col.posts[0]!
    const b = col.posts[1]!
    const when = daysAhead(1, 9)

    await api.schedule(a.id, when.toISOString())
    await api.schedule(b.id, daysAhead(1, 11).toISOString())
    await page.goto('/schedule')
    await showWeekOf(page, when)
    await expect(entry(page, a.id)).toHaveAttribute('data-state', 'scheduled')
    await api.publishThrough(a.id)
    await api.publishFailing(b.id)
    // The calendar re-reads its range on a reload (live updates refresh the open post, not the grid).
    await page.reload()
    await showWeekOf(page, when)
    await expect(entry(page, a.id)).toHaveAttribute('data-state', 'published')
    await expect(entry(page, a.id)).toHaveAttribute('draggable', 'false')
    await expect(entry(page, b.id)).toHaveAttribute('data-state', 'failed')
    await expect(entry(page, b.id)).toContainText('Failed')

    // Far in the future nothing is scheduled: the grid stays, with its explanation.
    await page.goto(`/schedule?at=${dayKey(daysAhead(400, 9))}`)
    await expect(page.getByTestId('schedule-empty')).toContainText('Nothing scheduled this week')
    await expect(page.getByTestId('schedule-grid')).toBeVisible()
  })
})
