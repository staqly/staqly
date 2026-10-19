import type { Page } from '@playwright/test'

import { freshCollection, titleOf } from '../../lib/data.js'
import { expect, test } from '../../lib/fixtures.js'

const actions = (page: Page) => page.getByTestId('post-actions')

test.describe('sharing a post', () => {
  test('a preview link opens the post per channel; reviewers comment on selected words, reply and resolve; the team reads the threads', async ({
    page,
    browser,
    api,
  }) => {
    const col = await freshCollection(api, { contents: ['Launch day: v1.4 is out'] })
    const post = col.posts[0]!

    await page.goto(`/posts/${post.id}`)
    await actions(page)
      .getByRole('button', { name: /Preview and feedback/ })
      .click()
    const share = page.getByTestId('post-share')

    // No feedback yet: the dialog is just the share link.
    await expect(share.getByRole('heading', { name: /Feedback/ })).toHaveCount(0)
    await share.getByTestId('create-preview').click()
    const url = (await share.getByTestId('preview-url').textContent())!.trim()

    expect(url).toMatch(/\/preview#[a-z2-7]{20,}$/)

    // Someone without a session: a fresh browser context. They select words and comment on them.
    const guest = await browser.newContext()
    const reviewer = await guest.newPage()

    await reviewer.goto(url)
    const card = reviewer.getByRole('article', { name: new RegExp(col.channel.name) })

    await expect(card).toContainText('Launch day: v1.4 is out')
    await card
      .locator('[data-annotate]')
      .first()
      .evaluate((el) => {
        const text = el.firstChild!
        const start = text.textContent!.indexOf('v1.4 is out')
        const range = document.createRange()

        range.setStart(text, start)
        range.setEnd(text, start + 'v1.4 is out'.length)
        window.getSelection()!.removeAllRanges()
        window.getSelection()!.addRange(range)
      })
    await reviewer.getByTestId('comment-selection').click()
    const box = reviewer.getByRole('textbox', { name: 'Comment on the selection' })

    await expect(box).toBeFocused()
    await box.fill('Can we add the changelog link?')
    await reviewer.getByRole('button', { name: 'Post comment' }).click()
    const who = reviewer.getByRole('dialog', { name: 'Who is commenting?' })

    await who.getByRole('textbox', { name: /Your name/ }).fill('Ana from the client')
    await who.getByRole('textbox', { name: /Email/ }).fill('ana@example.com')
    await who.getByRole('button', { name: 'Continue' }).click()
    const open = reviewer.getByRole('list', { name: 'Open comments' })

    await expect(open).toContainText('Can we add the changelog link?')
    await expect(open).not.toContainText('ana@example.com')
    // The words stay marked; clicking them opens the thread.
    const mark = card.getByRole('button', { name: 'v1.4 is out' })

    await expect(mark).toBeVisible()
    await mark.click()
    await expect(open.getByTestId('feedback-thread').first()).toBeFocused()
    await guest.close()

    // A second reviewer replies and resolves.
    const second = await browser.newContext()
    const ben = await second.newPage()

    await ben.goto(url)
    const thread = ben.getByTestId('feedback-thread').first()

    await thread.getByRole('button', { name: 'Reply', exact: true }).click()
    await thread.getByRole('textbox', { name: 'Reply to Ana from the client' }).fill('Agreed, and the docs')
    await thread.getByRole('button', { name: 'Reply', exact: true }).click()
    await ben
      .getByRole('dialog', { name: 'Who is commenting?' })
      .getByRole('textbox', { name: /Your name/ })
      .fill('Ben')
    await ben.getByRole('dialog', { name: 'Who is commenting?' }).getByRole('button', { name: 'Continue' }).click()
    await expect(thread).toContainText('Agreed, and the docs')
    await thread.getByRole('button', { name: 'Resolve', exact: true }).click()
    await ben.getByRole('button', { name: 'Resolved (1)' }).click()
    await expect(ben.getByTestId('resolved-threads')).toContainText('Resolved by Ben')
    await expect(
      ben.getByRole('article', { name: new RegExp(col.channel.name) }).getByRole('button', { name: 'v1.4 is out' }),
    ).toHaveCount(0)
    await second.close()

    // The team sees the thread on the post: the quote, the email, the reply; it reopens and replies as itself.
    await page.reload()
    await actions(page)
      .getByRole('button', { name: /Preview and feedback/ })
      .click()
    const feedback = page.getByTestId('post-feedback')

    await feedback.getByRole('button', { name: 'Resolved (1)' }).click()
    await expect(feedback).toContainText('Ana from the client')
    await expect(feedback).toContainText('ana@example.com')
    await expect(feedback).toContainText('v1.4 is out')
    await expect(feedback).toContainText('Agreed, and the docs')
    const teamThread = feedback.getByTestId('feedback-thread').first()

    await teamThread.getByRole('button', { name: 'Reopen' }).click()
    await expect(page.getByText(/was reopened/)).toBeVisible()
    await feedback.getByTestId('feedback-thread').first().getByRole('button', { name: 'Reply', exact: true }).click()
    await feedback.getByRole('textbox', { name: /Reply to/ }).fill('Added, thanks')
    await feedback.getByTestId('feedback-thread').first().getByRole('button', { name: 'Reply', exact: true }).click()
    await expect(feedback).toContainText('Added, thanks')

    // Revoked: the link stops working.
    await page.getByTestId('post-share').getByRole('button', { name: 'Revoke' }).click()
    await expect(page.getByText(/was revoked/)).toBeVisible()
    const later = await browser.newContext()
    const gone = await later.newPage()

    await gone.goto(url)
    await expect(gone.getByText('This preview is not available')).toBeVisible()
    await later.close()
  })

  test('statistics say which platforms give numbers; duplicate opens the new draft', async ({ page, api }) => {
    const col = await freshCollection(api, { contents: ['Numbers please'] })
    const post = col.posts[0]!

    await api.publishThrough(post.id, 'https://example.com/live/1')
    await page.goto(`/posts/${post.id}`)
    await expect(
      actions(page).getByRole('link', { name: new RegExp(`Open post on ${col.channel.name}`) }),
    ).toHaveAttribute('href', 'https://example.com/live/1')
    await page.getByRole('dialog').getByRole('tab', { name: 'Statistics' }).click()
    await expect(page.getByTestId('post-stats')).toContainText('Your automation has not sent numbers yet.')

    await actions(page).getByRole('button', { name: 'Duplicate' }).click()
    await expect(page.getByText(/was duplicated as a draft/)).toBeVisible()
    await expect(page).not.toHaveURL(new RegExp(post.id))
    await expect(page.getByRole('dialog').getByRole('heading', { name: titleOf('Numbers please') })).toBeVisible()
    await expect(page.getByRole('dialog').getByText('Draft', { exact: true }).first()).toBeVisible()
  })
})
