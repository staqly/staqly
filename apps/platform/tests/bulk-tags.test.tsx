import { screen, waitFor } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { describe, expect, it } from 'vitest'

import { PostSelectionBar } from '@/components/posts/post-list'

import { post } from './fixtures'
import { type Call, mockApi, renderWithProviders } from './utils'

describe('tags of the selected posts', () => {
  it('adds a tag to the post as the server has it now, keeping tags added elsewhere since the list loaded', async () => {
    // The list still holds version 3; an agent tagged the post since.
    const now = post({ tags: ['news', 'by-agent'], version: 4 })
    const api = mockApi({
      'GET /v1/tags': { data: [{ name: 'launch', count: 1 }] },
      'GET /v1/posts/pst_1': now,
      'PATCH /v1/posts/pst_1': { ...now, tags: ['news', 'by-agent', 'launch'], version: 5 },
    })
    const user = userEvent.setup()

    renderWithProviders(
      <PostSelectionBar selected={[post({ tags: ['news'], version: 3 })]} canWrite onClear={() => {}} />,
    )
    await user.click(screen.getByRole('button', { name: 'Tags' }))
    await user.click(await screen.findByRole('button', { name: 'Add tags' }))
    await user.click(await screen.findByRole('option', { name: '#launch' }))
    await user.keyboard('{Escape}')
    await user.click(screen.getByRole('button', { name: 'Apply' }))
    await waitFor(() => expect(api.find('PATCH', '/v1/posts/pst_1')).toHaveLength(1))
    expect(api.find('PATCH', '/v1/posts/pst_1')[0]!.body).toEqual({
      tags: ['news', 'by-agent', 'launch'],
      expected_version: 4,
    })
  })

  it('reads the post again when it changed between reading and writing', async () => {
    let version = 4
    const api = mockApi({
      'GET /v1/tags': { data: [{ name: 'launch', count: 1 }] },
      'GET /v1/posts/pst_1': () => post({ tags: version === 4 ? ['news'] : ['news', 'late'], version }),
      'PATCH /v1/posts/pst_1': (call: Call) => {
        if ((call.body as { expected_version: number }).expected_version === 4) {
          version = 5

          return { status: 409, body: { error: { code: 'post_changed', message: 'post was modified' } } }
        }

        return post({ tags: ['news', 'late', 'launch'], version: 6 })
      },
    })
    const user = userEvent.setup()

    renderWithProviders(
      <PostSelectionBar selected={[post({ tags: ['news'], version: 3 })]} canWrite onClear={() => {}} />,
    )
    await user.click(screen.getByRole('button', { name: 'Tags' }))
    await user.click(await screen.findByRole('button', { name: 'Add tags' }))
    await user.click(await screen.findByRole('option', { name: '#launch' }))
    await user.keyboard('{Escape}')
    await user.click(screen.getByRole('button', { name: 'Apply' }))
    await waitFor(() => expect(api.find('PATCH', '/v1/posts/pst_1')).toHaveLength(2))
    expect(api.find('PATCH', '/v1/posts/pst_1')[1]!.body).toEqual({
      tags: ['news', 'late', 'launch'],
      expected_version: 5,
    })
  })
})
