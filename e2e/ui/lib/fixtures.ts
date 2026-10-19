import { AxeBuilder } from '@axe-core/playwright'
import { test as base, expect, type Page } from '@playwright/test'

import { Api } from './api.js'
import { type ServerState, state } from './state.js'

export interface Fixtures {
  /** The server of this project (local or cloud). */
  srv: ServerState
  /** REST client for this project's workspace (cloud: the admin key). */
  api: Api
  /** Console errors and failed same-origin requests collected during the test. */
  problems: string[]
}

export const test = base.extend<Fixtures>({
  // oxlint-disable-next-line no-empty-pattern -- Playwright fixtures destructure the fixtures they need (none here)
  srv: async ({}, use, info) => {
    await use(info.project.name === 'cloud' ? state().cloud : state().local)
  },
  // oxlint-disable-next-line no-empty-pattern -- as above
  api: async ({}, use, info) => {
    const s = state()

    await use(info.project.name === 'cloud' ? new Api(s.cloud.api, s.cloud.adminKey) : new Api(s.local.api))
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
