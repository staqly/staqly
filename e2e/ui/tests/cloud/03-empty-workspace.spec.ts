import { apiKeySignIn, expect, ready, test, uniq } from '../../lib/fixtures.js'
import { state } from '../../lib/state.js'

// A brand-new workspace: every page shows a helpful empty state, never a blank area or an error.
test.use({ storageState: { cookies: [], origins: [] } })

test('empty states of a new workspace, then the first post arrives live', async ({ page }) => {
  const s = state().cloud
  const res = await fetch(`${s.api}/v1/workspaces`, {
    method: 'POST',
    headers: {
      'Content-Type': 'application/json',
      'X-Forwarded-For': `198.51.100.${1 + Math.floor(Math.random() * 250)}`,
    },
    body: JSON.stringify({ name: uniq('Empty ws'), setup_token: s.setupToken }),
  })
  const ws = (await res.json()) as { api_key: { key: string } }

  await apiKeySignIn(page, ws.api_key.key)
  await ready(page, 'Schedule')
  await expect(page.getByTestId('schedule-empty')).toContainText('Nothing scheduled this week')

  for (const [path, h1, empty] of [
    ['/channels', 'Channels', 'No channels yet'],
    ['/alerts', 'Alerts', 'No alerts yet'],
    ['/webhooks', 'Webhooks', 'No webhooks'],
  ] as const) {
    await page.goto(path)
    await ready(page, h1)
    await expect(page.getByText(empty, { exact: true })).toBeVisible()
    await expect(page.getByRole('alert').filter({ hasText: /Could not|error/i })).toHaveCount(0)
  }

  // The Default collection exists from the start; its page explains how posts get there.
  await page.goto('/posts')
  await ready(page, 'Default')
  await expect(page.getByText('No posts yet').first()).toBeVisible()
  await expect(page.getByRole('main')).toContainText('let your agent add posts')
  // Without a channel the composer says where to connect one.
  await page.getByTestId('new-post').click()
  await expect(page.getByRole('dialog', { name: 'New post' })).toContainText('No channels yet')
  await expect(
    page.getByRole('dialog', { name: 'New post' }).getByRole('link', { name: 'Connect a channel' }),
  ).toHaveAttribute('href', '/channels')
  await page.keyboard.press('Escape')

  // An agent adds the first post: the list shows it without a reload.
  const agent = await fetch(`${s.api}/v1/posts`, {
    method: 'POST',
    headers: { 'Content-Type': 'application/json', Authorization: `Bearer ${ws.api_key.key}` },
    body: JSON.stringify({
      collection: 'Default',
      title: 'First post from an agent',
      content: 'Written by an agent over the API.',
      source: 'claude-code',
    }),
  })

  expect(agent.status).toBe(201)
  await expect(page.getByText('First post from an agent')).toBeVisible({ timeout: 20_000 })
})
