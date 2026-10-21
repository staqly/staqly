import { screen, waitFor } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'

import { NewWorkspaceDialog } from '@/components/settings/new-workspace-dialog'

import { mockApi, renderWithProviders } from './utils'

function render(billing: boolean) {
  const api = mockApi({
    'GET /v1/billing': {
      enabled: billing,
      plan: 'starter',
      subscription_status: null,
      plan_expires_at: null,
      suspended: false,
      has_customer: false,
      seats: 1,
      trial: billing ? 'available' : 'none',
    },
    'POST /v1/account/workspaces': { status: 201, body: { id: 'ws_new', name: 'Client work', role: 'owner' } },
    'POST /v1/session/workspace': { status: 204, body: null },
  })

  renderWithProviders(<NewWorkspaceDialog open onOpenChange={() => {}} />, { mode: 'cloud' })

  return api
}

describe('new workspace', () => {
  const assign = vi.fn<(url: string) => void>()

  beforeEach(() => vi.stubGlobal('location', { ...window.location, assign }))
  afterEach(() => assign.mockReset())

  it('with billing: opens the welcome, whose last step is the plan', async () => {
    const api = render(true)
    const user = userEvent.setup()

    await user.type(screen.getByLabelText(/Name/), 'Client work')
    await user.click(await screen.findByRole('button', { name: 'Create and choose a plan' }))
    await waitFor(() => expect(assign).toHaveBeenCalledWith('/welcome'))
    expect(api.find('POST', '/v1/session/workspace')[0].body).toEqual({ workspace_id: 'ws_new' })
  })

  it('without billing: opens the schedule', async () => {
    const api = render(false)
    const user = userEvent.setup()

    await user.type(screen.getByLabelText(/Name/), 'Client work')
    await user.click(screen.getByRole('button', { name: 'Create workspace' }))
    await waitFor(() => expect(api.find('POST', '/v1/session/workspace')).toHaveLength(1))
    await waitFor(() => expect(assign).toHaveBeenCalledWith('/schedule'))
  })
})
