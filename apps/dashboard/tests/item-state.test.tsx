import { screen } from '@testing-library/react'
import { describe, expect, it } from 'vitest'

import { ItemState } from '@/components/common/status-badge'

import { collection, item } from './fixtures'
import { mockApi, renderWithProviders } from './utils'

describe('item state', () => {
  it('shows one badge: a run named by its action replaces the decision, the tooltip keeps both', async () => {
    const publish = {
      name: 'publish',
      label: 'Publish',
      event: 'item.publish_requested',
      requires_feedback: false,
      set_status: 'processing' as const,
      run_labels: { running: 'Publishing', done: 'Published', failed: 'Publish failed' },
    }

    mockApi({ 'GET /v1/collections': { data: [collection({ actions: [publish] })] } })
    renderWithProviders(<ItemState item={item({ status: 'processing', run_action: 'publish' })} />)
    expect(await screen.findByText('Publishing')).toHaveAttribute('title', 'Approved → Processing')
    expect(screen.queryByText('Approved')).not.toBeInTheDocument()
  })

  it('archived ends everything; a finished regeneration shows Pending again', () => {
    mockApi({ 'GET /v1/collections': { data: [collection()] } })
    renderWithProviders(
      <>
        <ItemState item={item({ status: 'archived', decision: 'pending', archived_at: '2026-01-01T00:00:00Z' })} />
        <ItemState item={item({ status: 'completed', decision: 'pending', run: 'completed' })} />
        <ItemState item={item({ status: 'failed', decision: 'pending', run: 'failed' })} />
      </>,
    )
    expect(screen.getAllByTestId('item-state').map((e) => e.textContent)).toEqual(['Archived', 'Pending', 'Failed'])
  })
})
