import { screen, waitFor } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { describe, expect, it } from 'vitest'

import { ProcessingNote } from '@/components/items/processing-note'
import { SettingsPage } from '@/components/settings/settings-page'

import { item } from './fixtures'
import { mockApi, renderWithProviders } from './utils'

describe('processing timeout', () => {
  it('shows how long the item has waited of the limit, and when it fails', () => {
    mockApi({})
    const since = new Date(Date.now() - 20 * 60_000).toISOString()

    renderWithProviders(<ProcessingNote item={item({ status: 'processing', updated_at: since })} />, { mode: 'local' })
    expect(screen.getByTestId('processing-timing')).toHaveTextContent(
      /Waiting 20 min 0\d s of 1 h · fails automatically at/,
    )
    expect(screen.getByRole('progressbar')).toHaveAttribute('aria-valuemax', '3600')
    expect(screen.getByRole('button', { name: 'Change the limit' })).toBeInTheDocument()
  })

  it('is changed in Settings, up to 24 h', async () => {
    const api = mockApi({
      'PATCH /v1/workspace': {
        id: 'ws_1',
        name: 'Test WS',
        plan: 'unlocked',
        recovery_rotated_at: '2026-01-01T00:00:00Z',
        created_at: '2026-01-01T00:00:00Z',
        processing_timeout_seconds: 86400,
      },
    })
    const user = userEvent.setup()

    renderWithProviders(<SettingsPage />, { mode: 'local' })
    await user.click(await screen.findByRole('combobox', { name: /fail items still processing after/i }))
    await user.click(await screen.findByRole('option', { name: '1 day' }))
    await waitFor(() => expect(api.find('PATCH', '/v1/workspace')).toHaveLength(1))
    expect(api.find('PATCH', '/v1/workspace')[0].body).toEqual({ processing_timeout_seconds: 86400 })
  })

  it('sets a limit for this item only', async () => {
    const since = new Date(Date.now() - 5_000).toISOString()
    const itm = item({ status: 'processing', updated_at: since })
    const api = mockApi({ 'PATCH /v1/items/itm_1': { ...itm, processing_timeout_seconds: 7200 } })
    const user = userEvent.setup()

    renderWithProviders(<ProcessingNote item={itm} />, { mode: 'local' })
    expect(screen.getByTestId('processing-timing')).toHaveTextContent(/^Waiting \d s of 1 h/)
    await user.click(screen.getByRole('button', { name: 'Change the limit' }))
    await user.click(await screen.findByRole('button', { name: '2 h' }))
    await waitFor(() => expect(api.find('PATCH', '/v1/items/itm_1')).toHaveLength(1))
    expect(api.find('PATCH', '/v1/items/itm_1')[0].body).toEqual({ processing_timeout_seconds: 7200 })
  })
})
