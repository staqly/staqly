import { screen } from '@testing-library/react'
import { describe, expect, it } from 'vitest'

import { PostActions } from '@/components/posts/post-actions'
import { DataPrivacyPage } from '@/components/settings/data-privacy-page'

import { post } from './fixtures'
import { mockApi, renderWithProviders } from './utils'

describe('data and privacy', () => {
  it('points to the Delete button the post actually has', async () => {
    mockApi({ 'GET /v1/posts/pst_1/comments': { data: [] } })

    renderWithProviders(<DataPrivacyPage />, { mode: 'local' })
    const hint = await screen.findByText(/To remove a post that hasn.t gone out/)

    expect(hint.textContent).not.toMatch(/More menu/)
    expect(hint.textContent).toMatch(/choose Delete \(the bin icon at the top of the post\)/)

    // The post's action row does carry that Delete button.
    renderWithProviders(<PostActions post={post({ id: 'pst_1' })} onDeleted={() => {}} />)
    expect(screen.getByRole('button', { name: 'Delete' })).toBeInTheDocument()
  })
})
