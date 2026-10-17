import { screen, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { describe, expect, it } from 'vitest'

import { ActionsEditor } from '@/components/collections/actions-editor'
import type { Collection } from '@/lib/api'

import { mockApi, renderWithProviders } from './utils'

const PATCH = 'PATCH /v1/collections/col_oalirbz67y4m4cqxq5dw'

const collection = {
  id: 'col_oalirbz67y4m4cqxq5dw',
  name: 'AI Research',
  actions: [
    {
      name: 'regenerate',
      label: 'Regenerate',
      event: 'item.regenerate_requested',
      set_status: 'processing',
      allowed_statuses: ['rejected'],
      requires_feedback: true,
      run_labels: { running: 'Regenerating', done: 'Regenerated', failed: 'Regeneration failed' },
    },
    {
      name: 'publish',
      label: 'Publish',
      event: 'item.publish_requested',
      set_status: 'processing',
      allowed_statuses: ['approved'],
      requires_feedback: false,
      run_labels: { running: 'Publishing', done: 'Published', failed: 'Publish failed' },
    },
    { name: 'archive', label: 'Archive', event: 'item.archived', set_status: 'archived', requires_feedback: false },
  ],
} as unknown as Collection

const row = (name: string) => within(screen.getByTestId(`action-row-${name}`))

describe('actions editor', () => {
  it('lists the fixed catalog with no text inputs; publish and archive are always on', () => {
    renderWithProviders(<ActionsEditor collection={collection} />)

    expect(screen.queryByRole('textbox')).not.toBeInTheDocument()
    expect(screen.getByRole('switch', { name: 'Regenerate' })).toBeChecked()
    expect(screen.getByRole('switch', { name: 'Retry' })).not.toBeChecked()
    expect(screen.getByRole('switch', { name: 'Publish' })).toBeDisabled()
    expect(screen.getByRole('switch', { name: 'Archive' })).toBeDisabled()
    expect(screen.getByRole('switch', { name: 'Archive' })).toBeChecked()
    expect(row('publish').getByText('Always on')).toBeInTheDocument()
    expect(row('publish').getByText('Publishing, then Published or Publish failed')).toBeInTheDocument()
    expect(row('archive').getByText('Runs from any status')).toBeInTheDocument()
    // Allowed from offers only the action's options.
    expect(
      row('regenerate')
        .getAllByRole('checkbox')
        .map((c) => c.id),
    ).toEqual(['action-regenerate-from-rejected', 'action-regenerate-from-failed', 'action-regenerate-from-expired'])
  })

  it('saves the flow: enabled names and allowed statuses only', async () => {
    const user = userEvent.setup()
    const api = mockApi({ [PATCH]: collection })

    renderWithProviders(<ActionsEditor collection={collection} />)
    await user.click(row('regenerate').getByRole('checkbox', { name: 'Expired' }))
    await user.click(screen.getByRole('switch', { name: 'Retry' }))
    expect(row('retry').getByText('Runs from Failed')).toBeInTheDocument()
    await user.click(screen.getByRole('button', { name: 'Save actions' }))
    const body = api.find('PATCH', '/v1/collections/col_oalirbz67y4m4cqxq5dw')[0].body as { actions: unknown[] }

    expect(body.actions).toEqual([
      { name: 'regenerate', allowed_statuses: ['rejected', 'expired'] },
      { name: 'publish', allowed_statuses: ['approved'] },
      { name: 'retry' },
      { name: 'archive' },
    ])
  })

  it('keeps at least one allowed status', async () => {
    const user = userEvent.setup()

    renderWithProviders(<ActionsEditor collection={collection} />)
    const only = row('regenerate').getByRole('checkbox', { name: 'Rejected' })

    expect(only).toBeChecked()
    expect(only).toBeDisabled()
    await user.click(row('regenerate').getByRole('checkbox', { name: 'Failed' }))
    expect(only).toBeEnabled()
  })

  it('starts publish automatically after approval only while it runs from Approved', async () => {
    const user = userEvent.setup()
    const api = mockApi({ [PATCH]: collection })

    renderWithProviders(<ActionsEditor collection={collection} />)
    const auto = screen.getByRole('switch', { name: 'Start automatically after approval' })

    expect(auto).not.toBeChecked()
    await user.click(auto)
    expect(auto).toBeChecked()

    // Moving publish to Completed only turns it off and locks it.
    await user.click(row('publish').getByRole('checkbox', { name: 'Completed' }))
    await user.click(row('publish').getByRole('checkbox', { name: 'Approved' }))
    expect(auto).not.toBeChecked()
    expect(auto).toBeDisabled()
    expect(row('publish').getByText('Needs publish to be allowed from Approved.')).toBeInTheDocument()

    await user.click(row('publish').getByRole('checkbox', { name: 'Approved' }))
    await user.click(auto)
    await user.click(screen.getByRole('button', { name: 'Save actions' }))
    const body = api.find('PATCH', '/v1/collections/col_oalirbz67y4m4cqxq5dw')[0].body as {
      actions: { name: string; auto_run?: boolean }[]
    }

    expect(body.actions.find((a) => a.name === 'publish')).toEqual({
      name: 'publish',
      allowed_statuses: ['approved', 'completed'],
      auto_run: true,
    })
  })

  it('shows a legacy collection as the catalog and the API error under the list', async () => {
    const user = userEvent.setup()

    mockApi({
      [PATCH]: {
        status: 422,
        body: {
          error: {
            code: 'validation_failed',
            message: 'validation failed',
            fields: { actions: 'unknown action ship' },
          },
        },
      },
    })

    renderWithProviders(<ActionsEditor collection={{ ...collection, actions: [] } as unknown as Collection} />)
    // Nothing stored: publish and archive still show on, the optional ones off.
    expect(screen.getByRole('switch', { name: 'Publish' })).toBeChecked()
    expect(screen.getByRole('switch', { name: 'Regenerate' })).not.toBeChecked()
    expect(screen.queryByText(/no custom actions/i)).not.toBeInTheDocument()

    await user.click(screen.getByRole('switch', { name: 'Regenerate' }))
    expect(row('regenerate').getByRole('checkbox', { name: 'Rejected' })).toBeChecked()
    expect(row('regenerate').getByRole('checkbox', { name: 'Failed' })).toBeChecked()
    await user.click(screen.getByRole('button', { name: 'Save actions' }))
    expect(await screen.findByText('Unknown action ship')).toBeInTheDocument()
  })
})
