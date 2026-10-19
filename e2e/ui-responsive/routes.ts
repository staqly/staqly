// Every dashboard route and the overlays worth checking, for one seeded workspace.
import type { Page } from 'playwright'

import type { SeedResult } from './seed.js'

export interface Route {
  name: string
  path: string
  /** opens a dialog, sheet, menu... after the page loaded */
  open?: (page: Page) => Promise<void>
  cloudOnly?: boolean
  /** cloud: signed out */
  public?: boolean
  /** only at these widths (e.g. the phone tab bar) */
  when?: (width: number) => boolean
}

async function click(page: Page, selector: string) {
  const el = page.locator(selector).first()

  // scrolled to the middle, so no fixed bar covers it
  await el.evaluate((e) => e.scrollIntoView({ block: 'center' }), undefined, { timeout: 10_000 })
  // ponytail: in the tall bottom sheets of touch widths Playwright's own scrolling lands the target under other
  // rows of the same sheet; a DOM click still opens it, and the layout checks are what this suite is about.
  await el.click({ timeout: 3_000 }).catch(() => el.evaluate((e) => (e as HTMLElement).click()))
  await page.waitForTimeout(400)
}

// The composer's channel multiselect: open it, tick each channel, close it.
async function pickChannels(page: Page, ...ids: string[]) {
  await page.getByRole('dialog').getByRole('button', { name: 'Channels' }).click()

  for (const id of ids) {
    await click(page, `[data-testid=pick-channel-${id}]:visible`)
  }

  await page.keyboard.press('Escape')
  await page.waitForTimeout(200)
}

export function routes({
  collectionId: col,
  postIds: ids,
  channelIds,
  previewToken,
  connectLinkToken,
}: SeedResult): Route[] {
  const collection = `/posts?collection=${col}`

  return [
    { name: 'schedule week', path: '/schedule' },
    { name: 'schedule day', path: '/schedule?view=day' },
    { name: 'schedule month', path: '/schedule?view=month' },
    { name: 'schedule list', path: '/schedule?layout=list' },
    { name: 'posts', path: '/posts' },
    { name: 'collection', path: collection },
    { name: 'collection filtered', path: `${collection}&status=scheduled` },
    ...['long', 'image', 'video', 'scheduled', 'publishing', 'published', 'failed', 'draft', 'tags', 'overrides'].map(
      (k) => ({
        name: `post ${k}`,
        path: `/posts/${ids[k]}`,
      }),
    ),
    { name: 'channels', path: '/channels' },
    { name: 'media', path: '/media' },
    { name: 'media file', path: '/media', open: (p) => click(p, '[data-testid=media-tile]:visible') },
    { name: 'activity', path: '/activity' },
    { name: 'activity filtered', path: `/activity?post=${ids.published}` },
    { name: 'alerts', path: '/alerts' },
    { name: 'webhooks', path: '/webhooks' },
    { name: 'integrations', path: '/integrations' },
    { name: 'integrations agents', path: '/integrations?view=agents' },
    ...['claude-code', 'codex', 'opencode', 'n8n', 'api'].map((t) => ({
      name: `integrations ${t}`,
      path: `/integrations?tab=${t}`,
    })),
    { name: 'api keys', path: '/integrations/api-keys', cloudOnly: true },
    { name: 'settings', path: '/settings' },
    { name: 'settings plan', path: '/settings/plan' },
    { name: 'settings data', path: '/settings/data' },
    { name: 'settings backups', path: '/settings/backups' },
    { name: 'settings system', path: '/settings/system' },
    { name: 'settings members', path: '/settings/members', cloudOnly: true },
    { name: 'settings account', path: '/settings/account', cloudOnly: true },
    {
      name: 'user menu',
      path: '/schedule',
      // phones keep the sidebar (and the person) in the menu sheet
      when: (w) => w >= 768,
      open: (p) => click(p, '[data-testid=user-menu-trigger]:visible'),
    },
    { name: 'not found', path: '/does-not-exist' },
    { name: 'missing post', path: '/posts/post_doesnotexist' },
    // overlays
    {
      name: 'command palette',
      path: '/schedule',
      open: async (p) => {
        await p.keyboard.press('Control+k')
        await p.waitForSelector('[cmdk-input]')
        await p.keyboard.type('Collection')
        await p.waitForTimeout(300)
      },
    },
    {
      name: 'workspace menu',
      path: '/posts',
      open: (p) => click(p, '[data-testid=workspace-menu-trigger]:visible, [aria-label="Workspace menu"]:visible'),
    },
    {
      name: 'more drawer',
      path: '/schedule',
      when: (w) => w < 768,
      open: (p) => click(p, '[data-testid=mobile-tabbar] button'),
    },
    {
      name: 'new collection',
      path: '/posts',
      // Desktop: "New" at the top of the collections pane; phones: the "+" chip of the strip.
      open: (p) => click(p, 'nav[aria-label="Collections"] button:visible'),
    },
    { name: 'composer', path: collection, open: (p) => click(p, '[data-testid=new-post]:visible') },
    {
      name: 'composer with channels',
      path: collection,
      open: async (p) => {
        await click(p, '[data-testid=new-post]:visible')
        await pickChannels(p, channelIds[0]!, channelIds[1]!)
        await click(p, `[data-testid=tab-channel-${channelIds[0]}]:visible`)
      },
    },
    {
      name: 'composer counters',
      path: collection,
      open: async (p) => {
        await click(p, '[data-testid=new-post]:visible')
        await pickChannels(p, channelIds[0]!)
        await click(p, '[data-testid=counter]:visible')
      },
    },
    {
      name: 'date-time picker',
      path: collection,
      open: async (p) => {
        await click(p, '[data-testid=new-post]:visible')
        await click(p, '[data-testid=date-time-picker]:visible')
      },
    },
    { name: 'collection settings', path: collection, open: (p) => click(p, 'button:has-text("Settings"):visible') },
    {
      name: 'danger zone',
      path: collection,
      open: async (p) => {
        await click(p, 'button:has-text("Settings"):visible')
        await click(p, '[data-testid=danger-zone] button:visible')
      },
    },
    {
      name: 'new schedule',
      path: collection,
      open: async (p) => {
        await click(p, 'button:has-text("Settings"):visible')
        await click(p, '[data-testid=new-schedule]:visible')
      },
    },
    {
      name: 'custom colour',
      path: '/posts',
      open: async (p) => {
        await click(p, 'nav[aria-label="Collections"] button:visible')
        await click(p, '[data-testid=color-custom]:visible')
      },
    },
    // The posts list: two rows ticked (the floating bar), a row's menu, the filters behind the Filter button.
    {
      name: 'posts selected',
      path: collection,
      open: async (p) => {
        await p
          .locator('[data-testid=post-select]')
          .nth(0)
          .evaluate((e) => (e as HTMLElement).click())
        await p
          .locator('[data-testid=post-select]')
          .nth(1)
          .evaluate((e) => (e as HTMLElement).click())
        await p.waitForTimeout(300)
      },
    },
    { name: 'post row menu', path: collection, open: (p) => click(p, '[aria-label^="More actions for"]:visible') },
    { name: 'posts filter', path: collection, open: (p) => click(p, '[data-testid=filter-button]:visible') },
    {
      name: 'post share',
      path: `/posts/${ids.long}`,
      open: (p) => click(p, '[data-testid=post-preview]:visible'),
    },
    {
      name: 'post stats',
      // Statistics show once something is published.
      path: `/posts/${ids.published}`,
      open: (p) => click(p, '[data-testid=tab-stats]:visible'),
    },
    // The public preview page a shared link opens (long post, long comment).
    { name: 'preview page', path: `/preview#${previewToken}` },
    // The page a connect link opens for someone without access (long note).
    { name: 'connect link page', path: `/add-channel#${connectLinkToken}` },
    { name: 'connect link dialog', path: '/channels', open: (p) => click(p, '[data-testid=connect-link]:visible') },
    // the post sheet is the composer: rescheduling is its date-time picker
    {
      name: 'reschedule',
      path: `/posts/${ids.draft}`,
      open: (p) => click(p, '[data-testid=date-time-picker]:visible'),
    },
    { name: 'add channel', path: '/channels', open: (p) => click(p, '[data-testid=add-channel]:visible') },
    {
      name: 'connect custom',
      path: '/channels',
      open: async (p) => {
        await click(p, '[data-testid=add-channel]:visible')
        await click(p, '[data-testid=provider-custom]:visible')
      },
    },
    { name: 'channel menu', path: '/channels', open: (p) => click(p, '[aria-label^="Actions for"]:visible') },
    { name: 'new alert', path: '/alerts', open: (p) => click(p, 'main button:has-text("New alert"):visible') },
    { name: 'new webhook', path: '/webhooks', open: (p) => click(p, 'button:has-text("New webhook"):visible') },
    { name: 'activity filter', path: '/activity', open: (p) => click(p, 'button:has-text("Add filter"):visible') },
    {
      name: 'slot dialog',
      path: '/schedule',
      // Phones show the schedule as a list: no hour slots to add to.
      when: (width) => width >= 600,
      open: async (p) => {
        const slot = p
          // An hour cell (week, day) or a day cell (month), whichever the default view shows, that can take a post.
          .locator(':is([data-testid^="schedule-slot-"], [data-testid^="schedule-day-"]):has([data-testid=slot-add])')
          .last()

        await slot.scrollIntoViewIfNeeded()
        await slot.locator('[data-testid=slot-add]').evaluate((e) => (e as HTMLElement).click())
        await p.waitForTimeout(400)
      },
    },
    {
      name: 'bell',
      path: '/schedule',
      // Phones: the top bar bell; wider screens: Notifications in the user menu.
      open: async (p) => {
        if ((await p.locator('[data-testid=notifications-bell]:visible').count()) > 0) {
          return click(p, '[data-testid=notifications-bell]:visible')
        }

        await click(p, '[data-testid=user-menu-trigger]')
        await p.getByRole('menuitem', { name: /Notifications/ }).click()
        await p.waitForTimeout(400)
      },
    },
    { name: 'welcome', path: '/welcome' },
    { name: 'login', path: '/login', public: true },
    { name: 'signup', path: '/signup', public: true },
    { name: 'invitation', path: '/invite?token=staqly-invite-unknown', public: true, cloudOnly: true },
  ]
}
