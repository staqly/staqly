import { expect, openNotifications, ready, test } from '../../lib/fixtures.js'

/** Every route, with the h1 it must show after a hard load (deep link / refresh). */
const ROUTES: [string, string | RegExp][] = [
  ['/schedule', 'Schedule'],
  ['/schedule?view=month', 'Schedule'],
  ['/posts', /.+/], // the h1 is the open collection's name; the pane heading is an h2
  ['/channels', 'Channels'],
  ['/activity', 'Activity'],
  ['/notifications', 'Notifications'],
  ['/alerts', 'Alerts'],
  ['/webhooks', 'Webhooks'],
  ['/integrations', 'Integrations'],
  ['/integrations?view=agents', 'Integrations'],
  ['/integrations/api', 'Integrations'],
  ['/integrations/mcp', 'Integrations'],
  ['/integrations/claude-code', 'Integrations'],
  ['/integrations/codex', 'Integrations'],
  ['/integrations/opencode', 'Integrations'],
  ['/integrations/n8n', 'Integrations'],
  ['/settings', 'Settings'],
  ['/settings/plan', 'Settings'],
  ['/settings/data', 'Settings'],
  ['/settings/backups', 'Settings'],
  ['/settings/system', 'Settings'],
]

test.describe('shell and navigation', () => {
  test('every route loads from a deep link and survives a refresh, without console errors', async ({
    page,
    srv,
    problems,
  }) => {
    const routes: [string, string | RegExp][] = [
      ...ROUTES,
      [`/posts?collection=${srv.seed.collections.instagram}`, 'Instagram posts'],
    ]

    const copy: string[] = []

    for (const [path, h1] of routes) {
      await page.goto(path)
      await ready(page, h1)
      await page.reload()
      await ready(page, h1)
      // The document title names the page and the product.
      await expect(page).toHaveTitle(/staqly/)
      // Copy: never the Unicode ellipsis (also in placeholders and labels), the product is "staqly".
      const text = await page.evaluate(() =>
        [
          document.body.innerText,
          ...[...document.querySelectorAll('[placeholder],[aria-label],[title]')].map(
            (e) =>
              `${e.getAttribute('placeholder') ?? ''} ${e.getAttribute('aria-label') ?? ''} ${e.getAttribute('title') ?? ''}`,
          ),
        ].join('\n'),
      )

      if (text.includes('…')) {
        copy.push(`${path}: Unicode ellipsis in "${/.{0,40}….{0,10}/.exec(text)?.[0]}"`)
      }

      // "staqly" mid-sentence, "Staqly" only where a sentence (or a label) starts.
      if (/(?<!(?:^|[.!?:]|[\n"'([>])\s*)Staqly\b|\bSTAQLY\b(?!_)/m.test(text)) {
        copy.push(`${path}: product name not written "staqly" (capital only at a sentence start)`)
      }

      // The review era is gone from the copy.
      if (/\b(Inbox|Approve|Reject|Watcher)\b/.test(text)) {
        copy.push(`${path}: review-era word in "${/.{0,30}\b(Inbox|Approve|Reject|Watcher)\b.{0,30}/.exec(text)?.[0]}"`)
      }
    }

    expect(copy).toEqual([])

    // A post open over the posts page: the sheet comes back after a refresh.
    await page.goto(`/posts/${srv.seed.posts.avatar1}`)
    await expect(page.getByRole('dialog').getByRole('heading', { name: 'Neon nights in the city' })).toBeVisible()
    await page.reload()
    await expect(page.getByRole('dialog').getByRole('heading', { name: 'Neon nights in the city' })).toBeVisible()

    // The old routes land on the new ones.
    for (const [from, to] of [
      ['/inbox', /\/schedule$/],
      ['/collections', /\/posts/],
      [
        `/collections/${srv.seed.collections.research}`,
        new RegExp(`/posts\\?collection=${srv.seed.collections.research}`),
      ],
    ] as const) {
      await page.goto(from)
      await expect(page).toHaveURL(to)
    }

    expect(problems).toEqual([])
  })

  test('sidebar: Schedule is home, Posts leads to the two-pane view, notifications open from the user menu', async ({
    page,
  }) => {
    await page.goto('/')
    await ready(page, 'Schedule')
    await expect(page).toHaveURL(/\/schedule$/)
    const nav = page.getByRole('navigation', { name: 'Main' })

    await expect(nav.getByRole('link', { name: 'Schedule' })).toHaveAttribute('aria-current', 'page')
    await expect(nav.getByRole('link', { name: 'Posts' })).toBeVisible()
    await expect(nav.getByRole('link', { name: 'Channels' })).toBeVisible()
    await expect(nav.getByRole('link', { name: 'Alerts' })).toBeVisible()
    await expect(nav.getByRole('link', { name: /Inbox|Watchers|Notifications/ })).toHaveCount(0)
    await nav.getByRole('link', { name: 'Posts' }).click()
    await expect(
      page.getByRole('navigation', { name: 'Collections' }).getByRole('link', { name: 'Instagram posts' }),
    ).toBeVisible()
    await expect(nav.getByRole('link', { name: 'Posts' })).toHaveAttribute('aria-current', 'page')
    await openNotifications(page)
    await expect(page.getByRole('dialog', { name: 'Notifications' })).toBeVisible()
  })

  test('back and forward walk through pages', async ({ page }) => {
    await page.goto('/schedule')
    await ready(page, 'Schedule')
    const nav = page.getByRole('navigation', { name: 'Main' })

    await nav.getByRole('link', { name: 'Channels' }).click()
    await ready(page, 'Channels')
    await nav.getByRole('link', { name: 'Webhooks' }).click()
    await ready(page, 'Webhooks')
    await nav.getByRole('link', { name: 'Integrations' }).click()
    await ready(page, 'Integrations')
    await page.goBack()
    await ready(page, 'Webhooks')
    await page.goBack()
    await ready(page, 'Channels')
    await page.goForward()
    await ready(page, 'Webhooks')
    await page.goBack()
    await page.goBack()
    await ready(page, 'Schedule')
  })

  test('command palette: opens with Cmd+K and the sidebar search, filters, navigates, closes with Escape', async ({
    page,
  }) => {
    await page.goto('/schedule')
    await ready(page, 'Schedule')
    await page.keyboard.press('ControlOrMeta+k')
    const palette = page.getByRole('dialog', { name: 'Command palette' })

    await expect(palette).toBeVisible()
    const input = palette.getByRole('combobox')

    await expect(input).toBeFocused()
    await input.fill('chann')
    await expect(palette.getByRole('option', { name: 'Channels' })).toBeVisible()
    await page.keyboard.press('Enter')
    await ready(page, 'Channels')
    await expect(palette).toBeHidden()

    await page.getByRole('button', { name: /Search or jump to/ }).click()
    await expect(palette).toBeVisible()
    await palette.getByRole('combobox').fill('Research')
    await palette.getByRole('option', { name: /Research notes/ }).click()
    await ready(page, 'Research notes')

    await page.keyboard.press('ControlOrMeta+k')
    await palette.getByRole('combobox').fill('zzzz nothing')
    await expect(palette.getByText('No results.')).toBeVisible()
    await page.keyboard.press('Escape')
    await expect(palette).toBeHidden()
  })

  test('theme: Settings switches light/dark and survives reload', async ({ page }) => {
    await page.goto('/settings')
    const theme = page.getByRole('radiogroup', { name: 'Theme' })

    await theme.getByRole('radio', { name: 'Dark' }).click()
    await expect(page.locator('html')).toHaveClass(/dark/)
    await page.reload()
    await expect(page.locator('html')).toHaveClass(/dark/)
    await expect(theme.getByRole('radio', { name: 'Dark' })).toBeChecked()
    await theme.getByRole('radio', { name: 'Light' }).click()
    await expect(page.locator('html')).not.toHaveClass(/dark/)
    await theme.getByRole('radio', { name: 'System' }).click()
  })

  test('workspace menu: plan, connect a device, settings', async ({ page }) => {
    await page.goto('/schedule')
    await page.getByRole('button', { name: 'Workspace menu' }).click()
    const menu = page.getByRole('menu')

    await expect(menu).toContainText('Community Edition · no plan limits')
    // Local mode: no log out, no other workspaces.
    await expect(menu.getByRole('menuitem', { name: 'Log out' })).toHaveCount(0)
    await menu.getByRole('menuitem', { name: 'Connect a device' }).click()
    const dialog = page.getByRole('dialog', { name: 'Connect a device' })

    await expect(dialog.getByRole('img', { name: 'Device link QR code' })).toBeVisible()
    await expect(dialog).toContainText('/connect#')
    await page.keyboard.press('Escape')
    await expect(dialog).toBeHidden()
    // Focus goes back to the menu trigger.
    await expect(page.getByRole('button', { name: 'Workspace menu' })).toBeFocused()
  })

  test('skip link: the first Tab offers "Skip to content", which lands in the page', async ({ page }) => {
    await page.goto('/webhooks')
    await ready(page, 'Webhooks')
    await page.keyboard.press('Tab')
    const skip = page.getByRole('link', { name: 'Skip to content' })

    await expect(skip).toBeFocused()
    await expect(skip).toBeVisible()
    await page.keyboard.press('Enter')
    await expect(page.locator('#main')).toBeFocused()
    await page.keyboard.press('Tab')
    // The next stop is inside the page, not the sidebar.
    expect(await page.evaluate(() => !!document.activeElement?.closest('#main'))).toBe(true)
  })
})
