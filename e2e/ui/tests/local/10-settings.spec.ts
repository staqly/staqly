import { readFileSync } from 'node:fs'

import { daysAhead, freshCollection } from '../../lib/data.js'
import { expect, ready, test } from '../../lib/fixtures.js'
import { connectLink } from '../../lib/stack.js'
import { state } from '../../lib/state.js'

test.describe('settings', () => {
  test('sections navigate and mark the current one', async ({ page }) => {
    await page.goto('/settings')
    const nav = page.getByRole('navigation', { name: 'Settings sections' })

    for (const [name, path, region] of [
      ['Plan & usage', '/settings/plan', /Community Edition/],
      ['Data & privacy', '/settings/data', 'Activity log'],
      ['Backups', '/settings/backups', 'Backup & restore'],
      ['System', '/settings/system', 'Health'],
      ['General', '/settings', 'Workspace'],
    ] as const) {
      await nav.getByRole('link', { name }).click()
      await expect(page).toHaveURL(new RegExp(`${path}$`))
      await expect(nav.getByRole('link', { name })).toHaveAttribute('aria-current', 'page')
      await expect(page.getByRole('heading', { level: 2, name: region }).first()).toBeVisible()
    }
  })

  test('general: rename the workspace (validation, Save only when changed)', async ({ page }) => {
    await page.goto('/settings')
    const ws = page.getByRole('region', { name: 'Workspace' })
    const name = ws.getByRole('textbox', { name: 'Name' })
    const save = ws.getByRole('button', { name: 'Save' })

    await expect(save).toBeDisabled()
    await name.fill('')
    await save.click()
    await expect(name).toHaveAttribute('aria-invalid', 'true')
    await name.fill('Local workspace (UI)')
    await save.click()
    await expect(page.getByRole('button', { name: 'Workspace menu' })).toContainText('Local workspace (UI)')
    await name.fill('Local workspace')
    await save.click()
    await expect(save).toBeDisabled()
  })

  test('devices: connect a device dialog with a QR and a fresh code; this device is marked', async ({ page }) => {
    await page.goto('/settings')
    const devices = page.getByRole('region', { name: 'Devices' })

    await expect(devices.getByText('This device')).toBeVisible()
    await devices.getByRole('button', { name: 'Connect a device' }).click()
    const dialog = page.getByRole('dialog', { name: 'Connect a device' })
    const link = async () => (await dialog.textContent())?.match(/\/connect#\S+/)?.[0]

    await expect(dialog.getByRole('img', { name: 'Device link QR code' })).toBeVisible()
    const first = await link()

    await dialog.getByRole('button', { name: 'Generate new code' }).click()
    await expect.poll(link).not.toBe(first)
    await expect(dialog).toContainText(/Expires in [0-5]:\d\d/)
    await dialog.getByRole('button', { name: 'Close' }).first().click()
    await expect(dialog).toBeHidden()
    await expect(devices.getByRole('button', { name: 'Connect a device' })).toBeFocused()
  })

  test('devices: another browser signed in with a link shows up and can be signed out from here', async ({
    page,
    browser,
  }) => {
    const s = state()
    const other = await browser.newContext({
      storageState: { cookies: [], origins: [] },
      userAgent: 'Mozilla/5.0 (iPhone; CPU iPhone OS 18_0 like Mac OS X) Mobile/15E148 Safari/604.1',
    })
    const phone = await other.newPage()

    await phone.goto(await connectLink(s.bin, { ...s.local, mode: 'local' }))
    await ready(phone, 'Schedule')

    await page.goto('/settings')
    const devices = page.getByRole('region', { name: 'Devices' })
    const entry = devices
      .getByRole('listitem')
      .filter({ hasText: /iPhone|Safari|iOS/ })
      .last()

    await expect(entry).toBeVisible()
    await entry.getByRole('button', { name: /Sign out|Revoke|Remove/ }).click()
    const confirm = page.getByRole('alertdialog')

    if (await confirm.isVisible().catch(() => false)) {
      await confirm.getByRole('button', { name: /Sign out|Revoke|Remove/ }).click()
    }

    await expect(entry).toHaveCount(0)
    await phone.reload()
    await expect(phone.getByRole('heading', { level: 1, name: 'Sign in to Staqly' })).toBeVisible()
    await other.close()
  })

  test('plan & usage (local): unlocked, usage meters for posts, channels and alerts', async ({ page }) => {
    await page.goto('/settings/plan')
    await expect(page.getByRole('heading', { level: 2, name: 'Community Edition', exact: true })).toBeVisible()
    await expect(page.getByRole('progressbar', { name: 'Webhooks' })).toBeVisible()
    await expect(page.getByRole('progressbar', { name: /Posts/ })).toBeVisible()
    await expect(page.getByRole('progressbar', { name: 'Channels' })).toBeVisible()
    await expect(page.getByRole('progressbar', { name: 'Alerts' })).toBeVisible()
  })

  test('system: the queue shows counts, filters by status, and a queued publish can be cancelled', async ({
    page,
    api,
  }) => {
    const col = await freshCollection(api, { posts: 1 })
    const post = col.posts[0]!

    await api.schedule(post.id, daysAhead(3, 9).toISOString())
    await page.goto('/settings/system')
    const queue = page.getByRole('region', { name: 'Queue' })

    await expect(queue.getByTestId('queue-stats')).toContainText('Queued')
    await expect(queue.getByTestId('queue-stats')).toContainText('Done today')
    await queue.getByRole('radiogroup', { name: 'Job filter' }).getByRole('radio', { name: 'queued' }).click()
    const row = queue.getByTestId('job-row').filter({ hasText: post.id })

    await expect(row).toContainText('Publish a post')
    await expect(row).toContainText('queued')
    await row.getByRole('button', { name: 'Cancel job' }).click()
    await expect(page.getByText(/was cancelled/)).toBeVisible()
    await expect(row).toHaveCount(0)
    expect(
      (await api.get<{ data: { status: string }[] }>(`/v1/jobs?kind=publish_post`)).data.find(
        (j) => j.status === 'cancelled',
      ),
    ).toBeTruthy()
    await queue.getByRole('radiogroup', { name: 'Job filter' }).getByRole('radio', { name: 'failed' }).click()
    await expect(queue.getByText(/No failed jobs|failed/)).toBeVisible()
  })

  test('data & privacy: activity downloads and a full workspace export', async ({ page }) => {
    await page.goto('/settings/data')
    await expect(page.getByRole('link', { name: 'Download JSONL' })).toHaveAttribute(
      'href',
      /\/api\/v1\/events\/export/,
    )
    const [csv] = await Promise.all([
      page.waitForEvent('download'),
      page.getByRole('link', { name: 'Download CSV' }).click(),
    ])

    expect(readFileSync((await csv.path())!, 'utf8').length).toBeGreaterThan(10)
    await page.getByRole('button', { name: /^Export (workspace|again)$/ }).click()
    const zipLink = page.getByRole('link', { name: 'Download ZIP' })

    await expect(zipLink).toBeVisible({ timeout: 30_000 })
    const [zip] = await Promise.all([page.waitForEvent('download'), zipLink.click()])

    expect(
      readFileSync((await zip.path())!)
        .subarray(0, 2)
        .toString(),
    ).toBe('PK')
  })

  test('backups and system pages', async ({ page }) => {
    await page.goto('/settings/backups')
    await expect(page.getByRole('tab', { name: 'staqly serve' })).toHaveAttribute('aria-selected', 'true')
    await expect(page.getByText('staqly serve backup ~/staqly-backup.db')).toBeVisible()
    await page.getByRole('tab', { name: 'Docker' }).click()
    await expect(page.getByRole('tabpanel', { name: 'Docker' })).toContainText('docker')

    await page.goto('/settings/system')
    await expect(page.getByRole('region', { name: 'Health' })).toContainText('Ready')
    await page.getByRole('button', { name: 'Check now' }).click()
    await expect(page.getByRole('listitem').filter({ hasText: 'SQLite answers a ping' })).toContainText('ok')
  })
})

test.describe('integrations', () => {
  test('setup: client tabs with snippets, transport choice, copy, test connection', async ({ page, context, srv }) => {
    await context.grantPermissions(['clipboard-read', 'clipboard-write'])
    await page.goto('/integrations')
    await ready(page, 'Integrations')
    const clients = page.getByRole('tablist', { name: 'Client' })

    for (const [tab, snippet] of [
      ['Claude Code', `claude mcp add --transport http staqly ${srv.api}/mcp`],
      ['Codex', `codex mcp add staqly --url ${srv.api}/mcp`],
      ['OpenCode', '"codemode": false'],
      ['n8n', 'staqly'],
      ['REST & MCP', `${srv.api}`],
    ] as const) {
      await clients.getByRole('tab', { name: tab }).click()
      await expect(clients.getByRole('tab', { name: tab })).toHaveAttribute('aria-selected', 'true')
      await expect(page.getByRole('tabpanel', { name: tab })).toContainText(snippet)
    }

    await clients.getByRole('tab', { name: 'Claude Code' }).click()
    const transport = page.getByRole('radiogroup', { name: 'MCP transport' })

    await transport.getByRole('radio', { name: 'STDIO (Docker)' }).click()
    await expect(page.getByRole('tabpanel', { name: 'Claude Code' })).toContainText('mcp-stdio')
    await transport.getByRole('radio', { name: 'HTTP' }).click()
    await page.getByRole('tabpanel', { name: 'Claude Code' }).getByRole('button', { name: 'Copy code' }).click()
    expect(await page.evaluate(() => navigator.clipboard.readText())).toContain('claude mcp add')

    await page.getByRole('button', { name: 'Test connection' }).click()
    await expect(page.getByText(/Connected|reachable|works/i).first()).toBeVisible()
  })

  test('agents & activity tab lists the agents that talked to the workspace', async ({ page }) => {
    await page.goto('/integrations')
    await page.getByRole('link', { name: 'Agents & activity' }).click()
    await expect(page).toHaveURL(/view=agents/)
    await expect(page.getByRole('link', { name: 'Agents & activity' })).toHaveAttribute('aria-current', 'page')
    await expect(page.getByRole('main')).toContainText(/higgsfield-agent|research-bot|n8n|node/i)
  })
})
