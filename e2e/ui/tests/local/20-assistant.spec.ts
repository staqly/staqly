import { freshCollection } from '../../lib/data.js'
import { expect, ready, test } from '../../lib/fixtures.js'

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
})
