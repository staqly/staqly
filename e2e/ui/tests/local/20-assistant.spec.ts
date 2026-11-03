import { freshCollection } from '../../lib/data.js'
import { axe, expect, ready, test } from '../../lib/fixtures.js'

test.describe('assistant', () => {
  test('reads at once, asks before a change, and the change shows in the dashboard', async ({ page, api }) => {
    const col = await freshCollection(api, { posts: 0 })

    await page.goto(`/posts?collection=${col.id}`)
    await ready(page, col.name)
    await page.getByTestId('assistant-toggle').click()
    const panel = page.getByTestId('assistant')

    await panel.getByLabel('Message to the assistant').fill('Write a post for our cafe')
    await panel.getByLabel('Message to the assistant').press('Enter')
    await expect(panel.getByText('Let me check your collections.')).toBeVisible()
    await expect(panel.getByText('List collections')).toBeVisible()
    // The change waits for the person, with what it will do.
    const card = panel.getByTestId('assistant-confirm')

    await expect(card).toContainText('Create post?')
    await expect(card).toContainText('Fresh coffee, fresh week.')
    await expect(panel.getByLabel('Message to the assistant')).toBeDisabled()
    await card.getByRole('button', { name: 'Allow' }).click()
    await expect(panel.getByText('the draft is in your collection')).toBeVisible()
    await expect(card).toBeHidden()
    // It was created as the person (the fake model picks the first collection); the chat survives a reload.
    await expect
      .poll(async () => (await api.get<{ data: { content: string }[] }>('/v1/posts?q=Fresh%20coffee')).data.length)
      .toBe(1)
    await page.reload()
    await expect(page.getByTestId('assistant').getByText('the draft is in your collection')).toBeVisible()
    await page.getByTestId('assistant').getByRole('button', { name: 'New chat' }).click()
    // An empty chat offers what fits the page: here the posts.
    await expect(
      page.getByTestId('assistant').getByText('Rewrite my latest draft to be shorter and punchier'),
    ).toBeVisible()
  })

  test('keeps private tool links masked and copyable after reloading a chat on a phone', async ({
    page,
    api,
    context,
  }) => {
    const col = await freshCollection(api, { posts: 0 })
    const url = `https://app.staqly.ai/preview#${'private-token-'.repeat(16)}`
    const call = { id: 'private_copy', name: 'create_preview_link', arguments: '{}' }
    const answer = { role: 'assistant', content: 'Preview link created.' }
    let saved: { id?: string; messages?: unknown[] } = {}

    await context.grantPermissions(['clipboard-read', 'clipboard-write'])
    await page.setViewportSize({ width: 390, height: 844 })
    await page.route('**/api/v1/assistant/chat', async (route) => {
      const req = route.request().postDataJSON() as { chat_id: string; messages: unknown[] }
      const added = [
        { role: 'assistant', content: '', tool_calls: [call] },
        {
          role: 'tool',
          tool_call_id: call.id,
          content: JSON.stringify({ id: 'pvw_privatecopy', token: 'private-token', url }),
        },
        answer,
      ]

      saved = { id: req.chat_id, messages: [...req.messages, ...added] }
      await route.fulfill({
        contentType: 'text/event-stream',
        body: [...added.map((message) => ({ type: 'message', message })), { type: 'done' }]
          .map((event) => `data: ${JSON.stringify(event)}\n\n`)
          .join(''),
      })
    })
    await page.route('**/api/v1/assistant/chats/*', (route) => route.fulfill({ json: saved }))

    await page.goto(`/posts?collection=${col.id}`)
    await ready(page, col.name)
    await page.getByTestId('assistant-toggle').click()
    const panel = page.getByTestId('assistant')

    await panel.getByLabel('Message to the assistant').fill('Create a private preview link')
    await panel.getByLabel('Message to the assistant').press('Enter')
    await expect(panel.getByText(answer.content)).toBeVisible()
    await expect(panel.getByTestId('secret-Private link')).not.toHaveText(url)
    await panel.getByRole('button', { name: 'Copy Private link' }).click()
    expect(await page.evaluate(() => navigator.clipboard.readText())).toBe(url)
    await page.reload()

    await expect(panel.getByText(answer.content)).toBeVisible()
    await expect(panel.getByTestId('secret-Private link')).not.toHaveText(url)
    await panel.getByRole('button', { name: 'Reveal Private link' }).click()
    await expect(panel.getByTestId('secret-Private link')).toHaveText(url)
    expect(await panel.evaluate((node) => node.scrollWidth <= node.clientWidth)).toBe(true)
    expect(await axe(page)).toEqual([])
  })
})
