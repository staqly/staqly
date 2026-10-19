import { AxeBuilder } from '@axe-core/playwright'
import { test as base, expect, type Page } from '@playwright/test'

import { Api } from './api.js'
import { type ServerState, state } from './state.js'

export interface Fixtures {
  /** The server of this project (local or cloud). */
  srv: ServerState
  /** REST client for this project's workspace (local: the access token, cloud: the admin key). */
  api: Api
  /** Console errors and failed same-origin requests collected during the test. */
  problems: string[]
  /** Publish now's Undo window, shortened so specs don't wait 7 s (the dashboard reads window.staqlyUndoMs). */
  undoWindow: void
}

export const test = base.extend<Fixtures>({
  // oxlint-disable-next-line no-empty-pattern -- Playwright fixtures destructure the fixtures they need (none here)
  srv: async ({}, use, info) => {
    await use(info.project.name === 'cloud' ? state().cloud : state().local)
  },
  // oxlint-disable-next-line no-empty-pattern -- as above
  api: async ({}, use, info) => {
    const s = state()

    await use(
      info.project.name === 'cloud' ? new Api(s.cloud.api, s.cloud.adminKey) : new Api(s.local.api, s.local.token),
    )
  },
  problems: [
    async ({ page }, use) => {
      const problems: string[] = []

      page.on('console', (msg) => {
        if (msg.type() === 'error') {
          problems.push(`console: ${msg.text()}`)
        }
      })
      page.on('pageerror', (err) => problems.push(`pageerror: ${err.message}`))
      await use(problems)
    },
    { auto: true },
  ],
  undoWindow: [
    async ({ page }, use) => {
      await page.addInitScript(() => {
        ;(window as { staqlyUndoMs?: number }).staqlyUndoMs = 300
      })
      await use()
    },
    { auto: true },
  ],
})

export { expect }

/** Waits until the app shell has rendered a page heading. */
export async function ready(page: Page, heading?: string | RegExp): Promise<void> {
  await expect(
    heading ? page.getByRole('heading', { level: 1, name: heading }) : page.getByRole('heading', { level: 1 }),
  ).toBeVisible()
}

/** Waits until a post's sheet is open (its heading is the post's first line; the page's h1 sits behind it). */
export async function postOpen(page: Page, title?: string | RegExp): Promise<void> {
  await expect(page.getByRole('dialog').getByRole('heading', { level: 2, name: title ?? /.+/ })).toBeVisible()
}

export interface AxeResult {
  id: string
  impact: string | null | undefined
  help: string
  nodes: string[]
  /** The first offending element, shortened (to find it in the code). */
  html?: string
  /** axe's own explanation for the first node (contrast ratios). */
  why?: string
}

/** Serious and critical axe-core (WCAG 2.2 A/AA) violations on the current page. */
export async function axe(page: Page, opts: { exclude?: string[] } = {}): Promise<AxeResult[]> {
  let builder = new AxeBuilder({ page }).withTags(['wcag2a', 'wcag2aa', 'wcag21a', 'wcag21aa', 'wcag22aa'])

  for (const sel of opts.exclude ?? []) {
    builder = builder.exclude(sel)
  }

  const res = await builder.analyze()

  return res.violations
    .filter((v) => v.impact === 'serious' || v.impact === 'critical')
    .map((v) => ({
      id: v.id,
      impact: v.impact,
      help: v.help,
      nodes: v.nodes.slice(0, 5).map((n) => n.target.join(' ')),
      html: v.nodes[0]?.html.slice(0, 200),
      why: v.nodes[0]?.failureSummary?.split('\n')[1]?.trim(),
    }))
}

/** Unique, readable names for things a test creates. */
export const uniq = (prefix: string) => `${prefix} ${Date.now().toString(36).slice(-5)}`

/** Closes every toast (they sit over the bottom-right corner, where form buttons often are). */
export async function dismissToasts(page: Page): Promise<void> {
  const close = page.getByRole('region', { name: /Notifications/ }).getByRole('button', { name: 'Close toast' })

  for (let i = 0; i < 10 && (await close.count()) > 0; i++) {
    await close
      .first()
      .click({ force: true })
      .catch(() => {})
  }
}

/**
 * Opens the API key sign-in. The test servers run with AUTH_DEV_SIGNIN, so /login shows the account sign-in
 * (email code) first and keeps the API key form one click away.
 */
/**
 * Signs this browser in with an admin API key over the API (the test server runs with AUTH_DEV_SIGNIN; the
 * dashboard itself offers no API key sign-in), then opens `next`.
 */
export async function apiKeySignIn(page: Page, key: string, next = '/schedule'): Promise<void> {
  const res = await page.request.post('/api/v1/sessions/api-key', { data: { api_key: key } })

  expect(res.status()).toBe(201)
  await page.goto(next)
}

/** Opens the notifications list: the bell in the phone top bar, or Notifications in the desktop user menu. */
export async function openNotifications(page: Page): Promise<void> {
  const bell = page.locator('[data-testid=notifications-bell]:visible')

  if ((await bell.count()) > 0) {
    await bell.first().click()

    return
  }

  await page.getByTestId('user-menu-trigger').click()
  await page.getByRole('menuitem', { name: /Notifications/ }).click()
}
