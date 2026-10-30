import { writeFileSync } from 'node:fs'

import type { Page } from '@playwright/test'

import { freshCollection } from '../../lib/data.js'
import { axe, type AxeResult, expect, openNotifications, postOpen, ready, test } from '../../lib/fixtures.js'
import { channelOption, pickChannels } from '../../lib/pick.js'

const PAGES = [
  '/schedule',
  '/schedule?view=month',
  '/schedule?layout=list',
  '/posts',
  '/media',
  '/channels',
  '/activity',
  '/alerts',
  '/webhooks',
  '/integrations',
  '/integrations?view=agents',
  '/integrations/api',
  '/integrations/n8n',
  '/settings',
  '/settings/plan',
  '/settings/data',
  '/settings/backups',
  '/settings/system',
]

async function scan(page: Page, found: Record<string, AxeResult[]>, key: string) {
  const v = await axe(page)

  if (v.length) {
    found[key] = v
  }
}

test.describe('accessibility', () => {
  test('axe: no serious or critical WCAG 2.2 AA violations on any page (light and dark)', async ({ page, srv }) => {
    test.setTimeout(240_000)
    const s = srv.seed
    const pages = [
      ...PAGES,
      `/posts?collection=${s.collections.instagram}`,
      `/posts/${s.posts.avatar0}`,
      `/posts/${s.posts.publishing}`,
      `/posts/${s.posts.failed}`,
      `/posts/${s.posts.published}`,
      `/posts/${s.posts.text}`,
    ]
    const found: Record<string, AxeResult[]> = {}

    for (const theme of ['light', 'dark']) {
      await page.goto('/settings')
      await page
        .getByRole('radiogroup', { name: 'Theme' })
        .getByRole('radio', { name: theme === 'dark' ? 'Dark' : 'Light' })
        .click()

      for (const path of pages) {
        await page.goto(path)
        await (path.startsWith('/posts/post_') ? postOpen(page) : ready(page))
        await page.waitForLoadState('networkidle')
        await scan(page, found, `${theme} ${path}`)
      }
    }

    await page.goto('/settings')
    await page.getByRole('radiogroup', { name: 'Theme' }).getByRole('radio', { name: 'System' }).click()
    writeFileSync(test.info().outputPath('axe.json'), JSON.stringify(found, null, 2))
    expect(found).toEqual({})
  })

  test('axe: dialogs, sheets and menus', async ({ page, srv }) => {
    const found: Record<string, AxeResult[]> = {}

    // Overlays only fade with reduced motion; scan once the fade is over (half-faded text has no contrast).
    await page.emulateMedia({ reducedMotion: 'reduce' })
    const open = async (key: string, path: string, act: () => Promise<unknown>) => {
      await page.goto(path)
      await (path.startsWith('/posts/post_') ? postOpen(page) : ready(page))
      await act()
      await page.waitForTimeout(700)
      await scan(page, found, key)
      await page.keyboard.press('Escape')
    }

    const col = `/posts?collection=${srv.seed.collections.instagram}`

    await open('palette', '/schedule', () => page.keyboard.press('ControlOrMeta+k'))
    await open('composer', col, () => page.getByTestId('new-post').click())
    await open('composer with channels', col, async () => {
      await page.getByTestId('new-post').click()
      await pickChannels(page, page.getByRole('dialog'), 'Company blog', 'Brand social')
    })
    await open('composer channel list', col, async () => {
      await page.getByTestId('new-post').click()
      await page.getByRole('dialog').getByRole('button', { name: 'Channels' }).click()
    })
    await open('preview and feedback', `/posts/${srv.seed.posts.draft}`, () =>
      page
        .getByTestId('post-actions')
        .getByRole('button', { name: /Preview and feedback/ })
        .click(),
    )
    await open('reschedule', `/posts/${srv.seed.posts.draft}`, () =>
      page.getByRole('dialog').getByTestId('date-time-picker').click(),
    )
    await open('new collection', '/posts', () =>
      page.getByRole('navigation', { name: 'Collections' }).getByRole('button', { name: 'New' }).click(),
    )
    await open('collection settings', col, () => page.getByRole('button', { name: 'Settings' }).click())
    await open('new schedule', col, async () => {
      await page.getByRole('button', { name: 'Settings' }).click()
      await page.getByTestId('new-schedule').click()
    })
    await open('add channel', '/channels', () => page.getByTestId('add-channel').click())
    await open('connect custom', '/channels', async () => {
      await page.getByTestId('add-channel').click()
      await page.getByTestId('provider-custom').click()
    })
    // (Dropdown menus are not scanned: while one is open Radix hides the rest of the page from assistive tech,
    // which axe reports as aria-hidden-focus on the sidebar.)
    await open('new alert', '/alerts', () => page.getByRole('main').getByRole('button', { name: 'New alert' }).click())
    await open('new webhook', '/webhooks', () => page.getByRole('button', { name: 'New webhook' }).first().click())
    await open('connect device', '/settings', () => page.getByRole('button', { name: 'Connect a device' }).click())
    await open('activity filter', '/activity', () => page.getByRole('button', { name: 'Add filter' }).click())
    await open('slot dialog', '/schedule', async () => {
      const slot = page.locator('[data-testid^="schedule-slot-"]:not([data-past])').last()

      await slot.hover()
      // The add button shows on focus (a click on the cell's empty space does the same).
      await slot.getByTestId('slot-add').focus()
      await page.keyboard.press('Enter')
    })
    await open('bell', '/schedule', () => openNotifications(page))
    writeFileSync(test.info().outputPath('axe.json'), JSON.stringify(found, null, 2))
    expect(found).toEqual({})
  })

  test('keyboard only: write and save a draft without a mouse', async ({ page, api }) => {
    const col = await freshCollection(api, { posts: 0 })

    await page.goto(`/posts?collection=${col.id}`)
    await ready(page, col.name)
    const newPost = page.getByTestId('new-post')

    // The skip link lands in the page; from there New post is a few stops away.
    await page.keyboard.press('Tab')
    await page.keyboard.press('Enter')

    for (let i = 0; i < 40 && !(await newPost.evaluate((el) => el === document.activeElement)); i++) {
      await page.keyboard.press('Tab')
    }

    await expect(newPost).toBeFocused()
    const outline = await newPost.evaluate((el) => {
      const cs = getComputedStyle(el)

      return `${cs.outlineStyle} ${cs.outlineWidth} ${cs.boxShadow}`
    })

    expect(outline).not.toBe('none 0px none')
    await page.keyboard.press('Enter')
    const sheet = page.getByRole('dialog', { name: 'New post' })

    await expect(sheet).toBeVisible()
    expect(await sheet.evaluate((d) => d.contains(document.activeElement))).toBe(true)
    // Tab reaches the Title first, then the Channels list: Enter opens it, typing finds the channel and Enter
    // picks it, Escape closes the list back onto its button; Tab then reaches the editor.
    const title = sheet.getByTestId('post-title')

    for (let i = 0; i < 20 && !(await title.evaluate((el) => el === document.activeElement)); i++) {
      await page.keyboard.press('Tab')
    }

    await expect(title).toBeFocused()
    await page.keyboard.type('Keyboard post')
    const channels = sheet.getByRole('button', { name: 'Channels' })

    for (let i = 0; i < 20 && !(await channels.evaluate((el) => el === document.activeElement)); i++) {
      await page.keyboard.press('Tab')
    }

    await expect(channels).toBeFocused()
    await page.keyboard.press('Enter')
    await page.keyboard.type(col.channel.name)
    await page.keyboard.press('Enter')
    await expect(channelOption(page, col.channel.name)).toHaveAttribute('data-checked', 'true')
    await page.keyboard.press('Escape')
    await expect(channels).toBeFocused()
    await expect(channels).toContainText(col.channel.name)
    const editor = sheet.getByTestId('post-content')

    for (
      let i = 0;
      i < 20 && !(await editor.evaluate((el) => el.contains(document.activeElement) || el === document.activeElement));
      i++
    ) {
      await page.keyboard.press('Tab')
    }

    await page.keyboard.type('Typed without a mouse')

    // Tab cycles inside the sheet.
    for (let i = 0; i < 12; i++) {
      await page.keyboard.press('Tab')
      expect(await sheet.evaluate((d) => d.contains(document.activeElement))).toBe(true)
    }

    const save = sheet.getByRole('button', { name: 'Save draft' })

    await save.focus()
    await page.keyboard.press('Enter')
    await expect(sheet).toBeHidden()
    await expect(
      page.getByTestId('posts-list').getByTestId('post-row').filter({ hasText: 'Keyboard post' }),
    ).toBeVisible()
    // Focus went back to the button that opened the sheet.
    await expect(newPost).toBeFocused()
  })

  test('dialogs trap focus and give it back: the delete confirmation and the collection settings sheet', async ({
    page,
    api,
  }) => {
    const col = await freshCollection(api, { posts: 1 })

    await page.goto(`/posts/${col.posts[0]!.id}`)
    const del = page.getByTestId('post-actions').getByRole('button', { name: 'Delete' })

    await del.focus()
    await page.keyboard.press('Enter')
    const dialog = page.getByRole('alertdialog')

    await expect(dialog).toBeVisible()

    for (let i = 0; i < 6; i++) {
      await page.keyboard.press('Tab')
      expect(await dialog.evaluate((d) => d.contains(document.activeElement))).toBe(true)
    }

    await page.keyboard.press('Escape')
    await expect(dialog).toBeHidden()
    await expect(del).toBeFocused()

    await page.goto(`/posts?collection=${col.id}`)
    const settings = page.getByRole('button', { name: 'Settings' })

    await settings.focus()
    await page.keyboard.press('Enter')
    const sheet = page.getByRole('dialog', { name: 'Collection settings' })

    await expect(sheet).toBeVisible()
    expect(await sheet.evaluate((d) => d.contains(document.activeElement))).toBe(true)
    await page.keyboard.press('Escape')
    await expect(settings).toBeFocused()
  })
})
