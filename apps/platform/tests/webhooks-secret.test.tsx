import { screen, waitFor } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { describe, expect, it } from 'vitest'

import { WebhooksPage } from '@/components/webhooks/webhooks-page'

import { mockApi, renderWithProviders } from './utils'

const webhook = (secret: string) => ({
  id: 'wh_q2vbm4kz7xw3dn5hjr6c',
  url: 'https://example.com/hook',
  description: 'Receiver',
  secret,
  events: ['item.approved'],
  collection_ids: [],
  active: true,
  timeout_ms: 10000,
  max_attempts: 8,
  created_at: '2026-10-13T12:00:00Z',
  updated_at: '2026-10-13T12:00:00Z',
})

describe('webhook signing secret', () => {
  it('shows the full secret once after a rotation, then only the masked one', async () => {
    const user = userEvent.setup()

    mockApi({
      'GET /v1/webhooks': { data: [webhook('whsec_****r2xa')] },
      'GET /v1/webhook-deliveries': { data: [] },
      'GET /v1/collections': { data: [] },
      'PATCH /v1/webhooks/wh_q2vbm4kz7xw3dn5hjr6c': webhook('whsec_k7fq2m9xw4zt8vn3hr6c'),
    })
    renderWithProviders(<WebhooksPage />, { mode: 'local' })

    expect(await screen.findByText('whsec_****r2xa')).toBeInTheDocument()
    expect(screen.getByTitle(/Lost it\? Rotate the secret/)).toBeInTheDocument()

    await user.click(screen.getByRole('button', { name: 'Webhook options' }))
    await user.click(await screen.findByRole('menuitem', { name: /rotate secret/i }))

    expect(await screen.findByText('Copy the signing secret now. It is not shown again.')).toBeInTheDocument()
    expect(screen.getByTestId('secret-Signing secret')).toHaveTextContent('whsec_k7fq2m9xw4zt8vn3hr6c')

    await user.click(screen.getByRole('button', { name: 'Done' }))
    await waitFor(() => expect(screen.queryByText(/Copy the signing secret now/)).not.toBeInTheDocument())
  })
})
