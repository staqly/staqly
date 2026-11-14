import { screen, waitFor } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { describe, expect, it } from 'vitest'

import { PostSelectionBar } from '@/components/posts/post-list'

import { post } from './fixtures'
import { mockApi, renderWithProviders } from './utils'

describe('archiving posts', () => {
  it('archives the selected posts, leaving out the scheduled ones', async () => {
    const api = mockApi({
      'POST /v1/posts/pst_1/archive': post({ archived_at: '2026-01-03T10:00:00Z' }),
    })

    renderWithProviders(
      <PostSelectionBar selected={[post(), post({ id: 'pst_2', status: 'scheduled' })]} canWrite onClear={() => {}} />,
    )
    await userEvent.click(screen.getByTestId('posts-bulk-archive'))
    await waitFor(() => expect(api.find('POST', '/v1/posts/pst_1/archive')).toHaveLength(1))
    expect(api.find('POST', '/v1/posts/pst_2/archive')).toHaveLength(0)
  })

  it('brings archived posts back when only archived ones are selected', async () => {
    const api = mockApi({ 'POST /v1/posts/pst_1/unarchive': post() })

    renderWithProviders(
      <PostSelectionBar selected={[post({ archived_at: '2026-01-03T10:00:00Z' })]} canWrite onClear={() => {}} />,
    )
    expect(screen.getByTestId('posts-bulk-archive')).toHaveAccessibleName('Unarchive')
    await userEvent.click(screen.getByTestId('posts-bulk-archive'))
    await waitFor(() => expect(api.find('POST', '/v1/posts/pst_1/unarchive')).toHaveLength(1))
  })
})
