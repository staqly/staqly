import { render, screen, within } from '@testing-library/react'
import { describe, expect, it } from 'vitest'

import { ProviderGrid } from '@/components/channels/provider-grid'

/** The Communities and chat tiles, with Discord's bot available or not. */
const chatTiles = (discord: 'available' | 'soon') => {
  const { unmount } = render(
    <ProviderGrid
      providers={[
        { id: 'discord', name: 'Discord', status: discord },
        { id: 'discord_webhook', name: 'Discord webhook', status: 'available' },
        { id: 'slack', name: 'Slack', status: 'soon' },
      ]}
      onPick={() => {}}
    />,
  )
  const ids = within(screen.getByRole('list', { name: 'Communities and chat' }))
    .getAllByRole('button')
    .map((b) => b.dataset.testid)

  unmount()

  return ids
}

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

  it('shows the Discord webhook only while the server has no Discord bot', () => {
    expect(chatTiles('available')).toEqual(['provider-discord', 'provider-slack'])
    expect(chatTiles('soon')).toEqual(['provider-discord_webhook', 'provider-slack'])
  })
})
