import { mkdtempSync, rmSync } from 'node:fs'
import { tmpdir } from 'node:os'
import { join } from 'node:path'

import type { Page } from '@playwright/test'

import { Api } from '../../lib/api.js'
import { freshCollection, getPost, titleOf } from '../../lib/data.js'
import { expect, ready, test } from '../../lib/fixtures.js'
import { connectLink, type RunningServer, startServer } from '../../lib/stack.js'
import { state } from '../../lib/state.js'

/** Publish now from the composer footer (it goes out after the short Undo window). */
async function publishNow(page: Page) {
  await page.getByTestId('publish-now').click()
}

const toasts = (page: Page) => page.getByRole('region', { name: /Notifications/ }).getByRole('listitem')

test.describe('loading, server errors and conflicts', () => {
  test('loading: skeletons on a slow network, then content', async ({ page }) => {
    const cdp = await page.context().newCDPSession(page)

    await cdp.send('Network.enable')
    await cdp.send('Network.emulateNetworkConditions', {
      offline: false,
      latency: 1500,
      downloadThroughput: 50_000,
      uploadThroughput: 50_000,
    })
    await page.goto('/webhooks', { waitUntil: 'commit' })
    await expect(page.locator('[data-slot="skeleton"]').first()).toBeVisible({ timeout: 30_000 })
    await ready(page, 'Webhooks')
    await expect(page.getByTestId('webhook-card').first()).toBeVisible({ timeout: 30_000 })
    await cdp.send('Network.emulateNetworkConditions', {
      offline: false,
      latency: 0,
      downloadThroughput: -1,
      uploadThroughput: -1,
    })
  })

  test('a 500 on a list shows an error with Try again, which recovers', async ({ page }) => {
    let fail = true

    await page.route('**/api/v1/channels', (route) =>
      fail
        ? route.fulfill({
            status: 500,
            contentType: 'application/json',
            body: JSON.stringify({ error: { code: 'internal', message: 'database is locked' } }),
          })
        : route.fallback(),
    )
    await page.goto('/channels')
    const alert = page.getByRole('alert').filter({ hasText: 'Could not load' })

    // A 5xx says it's on our side (the server's own text stays in the logs).
    await expect(alert).toContainText(/something went wrong on our side/i)
    fail = false
    await alert.getByRole('button', { name: 'Try again' }).click()
    await expect(page.getByTestId('channel-row').first()).toBeVisible()
  })

  test('a 500 on publish is reported and the post stays a draft', async ({ page, api }) => {
    const col = await freshCollection(api, { posts: 1 })
    const id = col.posts[0]!.id

    // The dashboard's Publish now schedules the post a moment ahead (the Undo window).
    await page.route(`**/api/v1/posts/${id}/schedule`, (route) =>
      route.fulfill({
        status: 500,
        contentType: 'application/json',
        body: JSON.stringify({ error: { code: 'internal', message: 'something broke' } }),
      }),
    )
    await page.goto(`/posts/${id}`)
    await publishNow(page)
    await expect(toasts(page).filter({ hasText: 'something broke' })).toBeVisible()
    // Still a draft: Publish now is still offered.
    await expect(page.getByTestId('publish-now')).toBeVisible()
    expect((await getPost(api, id)).status).toBe('draft')
  })

  test('two tabs: one publishes, the other still shows Publish now and gets a clear conflict', async ({
    page,
    context,
    api,
  }) => {
    const col = await freshCollection(api, { posts: 1 })
    const id = col.posts[0]!.id
    const other = await context.newPage()

    await page.goto(`/posts/${id}`)
    await other.goto(`/posts/${id}`)
    await expect(other.getByRole('dialog').getByRole('heading', { name: titleOf(col.posts[0]!.content) })).toBeVisible()
    // Tab 1 publishes.
    await publishNow(page)
    // After the Undo window the queue picks it up (its scheduler runs every few seconds).
    await expect.poll(async () => (await getPost(api, id)).status, { timeout: 20_000 }).toBe('publishing')
    // Tab 2 may still offer Publish now; a second publish must not start another run.
    const publish = other.getByTestId('publish-now')

    if (await publish.isVisible()) {
      // The sheet follows the post live, so the button may go away under the click: either a clear message or
      // the sheet already shows the post going out.
      await publish.click({ timeout: 2_000 }).catch(() => {})
      await expect(
        toasts(other)
          .filter({ hasText: /being published|publishing|changed|conflict/i })
          .or(other.getByRole('dialog').getByText('Going out now', { exact: false }))
          .first(),
      ).toBeVisible({ timeout: 15_000 })
    } else {
      await other.keyboard.press('Escape')
    }

    await expect(other.getByRole('dialog').getByText('Publishing', { exact: true }).first()).toBeVisible({
      timeout: 15_000,
    })
    expect((await api.get<{ data: unknown[] }>(`/v1/jobs?kind=publish_target`)).data.length).toBeGreaterThan(0)
    await other.close()
  })
})

test.describe('the server goes away mid-session', () => {
  let srv: RunningServer
  const port = Number(process.env.UI_DOWN_PORT ?? 61633)

  test.beforeAll(async () => {
    srv = await startServer(state().bin, {
      mode: 'local',
      port,
      dashboardPort: port + 50,
      env: { DASHBOARD_EXPOSED: 'true' },
      dataDir: mkdtempSync(join(tmpdir(), 'staqly-ui-down-')),
    })
  })

  test.afterAll(async () => {
    await srv?.stop()

    if (srv) {
      rmSync(srv.dataDir, { recursive: true, force: true })
      rmSync(`${srv.dataDir}.log`, { force: true })
    }
  })

  test('actions fail with a clear message, everything recovers after a restart', async ({ browser }) => {
    const api = new Api(srv.api)
    const col = await freshCollection(api, { posts: 1 })
    const ctx = await browser.newContext({ storageState: { cookies: [], origins: [] }, baseURL: srv.dash })
    const page = await ctx.newPage()

    await page.goto(await connectLink(state().bin, srv))
    await ready(page, 'Schedule')
    await page.goto(`/posts/${col.posts[0]!.id}`)
    await expect(page.getByRole('dialog').getByRole('heading', { name: titleOf(col.posts[0]!.content) })).toBeVisible()

    await srv.stop()
    // A publish while the server is down: a clear error; the sheet stays.
    await publishNow(page)
    await expect(toasts(page).filter({ hasText: /Cannot reach the staqly API/ })).toBeVisible()
    await expect(page.getByRole('dialog')).toBeVisible()

    // Back up on the same data: the same button works, no reload needed.
    srv = await startServer(state().bin, {
      mode: 'local',
      port,
      dashboardPort: port + 50,
      env: { DASHBOARD_EXPOSED: 'true' },
      dataDir: srv.dataDir,
    })
    await publishNow(page)
    await expect.poll(async () => (await getPost(api, col.posts[0]!.id)).status, { timeout: 20_000 }).toBe('publishing')
    await ctx.close()
  })
})
