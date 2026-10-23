import { render, screen, within } from '@testing-library/react'
import { describe, expect, it } from 'vitest'

import { ProviderGrid } from '@/components/channels/provider-grid'

describe('ProviderGrid', () => {
  it('lists available platforms before the ones coming soon, keeping the catalog order within each', () => {
    render(
      <ProviderGrid
        providers={[
          { id: 'x', name: 'X', status: 'available' },
          { id: 'facebook', name: 'Facebook Page', status: 'soon' },
          { id: 'instagram', name: 'Instagram', status: 'available' },
          { id: 'threads', name: 'Threads', status: 'soon' },
          { id: 'linkedin', name: 'LinkedIn', status: 'available' },
        ]}
        onPick={() => {}}
      />,
    )

    const ids = within(screen.getByRole('list', { name: 'Social networks' }))
      .getAllByRole('button')
      .map((b) => b.dataset.testid)

    expect(ids).toEqual([
      'provider-x',
      'provider-instagram',
      'provider-linkedin',
      'provider-facebook',
      'provider-threads',
    ])
  })
})
