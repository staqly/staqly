import { screen } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { describe, expect, it } from 'vitest'

import { ActionsEditor } from '@/components/collections/actions-editor'
import type { Collection } from '@/lib/api'

import { mockApi, renderWithProviders } from './utils'

const collection = {
  id: 'col_oalirbz67y4m4cqxq5dw',
  name: 'AI Research',
  actions: [
    {
      name: 'regenerate',
      label: 'Regenerate',
      event: 'item.regenerate_requested',
      set_status: 'processing',
      allowed_statuses: ['pending', 'rejected'],
      requires_feedback: true,
    },
  ],
} as unknown as Collection

describe('actions editor', () => {
  it('shows each action as a one-line summary and expands it to edit', async () => {
    const user = userEvent.setup()

    renderWithProviders(<ActionsEditor collection={collection} />)

    expect(screen.getByText('Regenerate')).toBeInTheDocument()
    expect(
      screen.getByText('starts a run (Processing) · from Pending, Rejected · feedback required'),
    ).toBeInTheDocument()
    expect(screen.queryByLabelText('Label')).not.toBeInTheDocument()

    await user.click(screen.getByRole('button', { name: 'Edit action Regenerate' }))
    expect(screen.getByLabelText('Label')).toHaveValue('Regenerate')
  })

  it('picks allowed statuses from a multi-select and saves them', async () => {
    const user = userEvent.setup()
    const api = mockApi({ 'PATCH /v1/collections/col_oalirbz67y4m4cqxq5dw': collection })

    renderWithProviders(<ActionsEditor collection={collection} />)
    await user.click(screen.getByRole('button', { name: 'Edit action Regenerate' }))
    const picker = screen.getByRole('button', { name: /allowed from/i })

    // A run starts after a decision: the older "pending" is dropped and not offered.
    expect(picker).toHaveTextContent(/^Rejected/)
    await user.click(picker)
    await user.click(await screen.findByRole('option', { name: 'Failed' }))
    expect(screen.queryByRole('option', { name: 'Pending' })).not.toBeInTheDocument()
    await user.keyboard('{Escape}')
    expect(picker).toHaveTextContent('RejectedFailed')

    await user.click(screen.getByRole('button', { name: 'Save actions' }))
    const body = api.find('PATCH', '/v1/collections/col_oalirbz67y4m4cqxq5dw')[0].body as Collection

    expect(body.actions?.[0].allowed_statuses).toEqual(['rejected', 'failed'])
  })

  it('opens a new action ready to fill in', async () => {
    const user = userEvent.setup()

    renderWithProviders(<ActionsEditor collection={collection} />)
    await user.click(screen.getByRole('button', { name: /add action/i }))

    expect(screen.getByText('Untitled action')).toBeInTheDocument()
    expect(screen.getByLabelText('Label')).toHaveValue('')
  })

  it('marks an invalid name on save; the event follows the name and is read-only', async () => {
    const user = userEvent.setup()

    renderWithProviders(<ActionsEditor collection={collection} />)
    await user.click(screen.getByRole('button', { name: 'Edit action Regenerate' }))
    const name = screen.getByRole('textbox', { name: 'Name' })

    expect(name).toHaveAttribute('aria-required', 'true')
    expect(screen.getByText('Name', { selector: 'label' })).toHaveTextContent('Name*')
    expect(screen.queryByRole('textbox', { name: 'Event' })).not.toBeInTheDocument()

    await user.clear(name)
    // Nothing is flagged before the first save.
    expect(name).toHaveAttribute('aria-invalid', 'false')
    await user.click(screen.getByRole('button', { name: 'Save actions' }))
    expect(screen.getByRole('alert')).toHaveTextContent('Name is required')
    expect(name.closest('[data-invalid]')).toHaveAttribute('data-invalid', 'true')
    await user.type(name, 'approve')
    expect(screen.getByRole('alert')).toHaveTextContent('“approve” is a built-in action')
    await user.clear(name)
    await user.type(name, 'redo')
    expect(screen.queryByRole('alert')).not.toBeInTheDocument()
    expect(screen.getByText('item.redo_requested')).toBeInTheDocument()
  })

  it('names the run of an action that starts one', async () => {
    const user = userEvent.setup()
    const api = mockApi({ 'PATCH /v1/collections/col_oalirbz67y4m4cqxq5dw': collection })

    renderWithProviders(<ActionsEditor collection={collection} />)
    await user.click(screen.getByRole('button', { name: 'Edit action Regenerate' }))
    await user.type(screen.getByRole('textbox', { name: /while it runs/i }), 'Regenerating')
    await user.click(screen.getByRole('button', { name: 'Save actions' }))
    const body = api.find('PATCH', '/v1/collections/col_oalirbz67y4m4cqxq5dw')[0].body as Collection

    expect(body.actions?.[0].run_labels?.running).toBe('Regenerating')
  })

  it('opens an invalid new action on save and shows the API error under the list', async () => {
    const user = userEvent.setup()

    const api = mockApi({
      'PATCH /v1/collections/col_oalirbz67y4m4cqxq5dw': {
        status: 422,
        body: {
          error: {
            code: 'validation_failed',
            message: 'validation failed',
            fields: { actions: 'event item.approved is reserved for staqly' },
          },
        },
      },
    })

    renderWithProviders(<ActionsEditor collection={collection} />)
    await user.click(screen.getByRole('button', { name: /add action/i }))
    await user.click(screen.getByRole('button', { name: 'Edit action unnamed' }))
    expect(screen.queryByRole('textbox', { name: 'Name' })).not.toBeInTheDocument()

    await user.click(screen.getByRole('button', { name: 'Save actions' }))
    const name = await screen.findByRole('textbox', { name: 'Name' })

    expect(name).toHaveAttribute('aria-invalid', 'true')
    expect(screen.getByRole('alert')).toHaveTextContent('Name is required')
    expect(api.find('PATCH', '/v1/collections/col_oalirbz67y4m4cqxq5dw')).toHaveLength(0)

    await user.type(name, 'publish')
    await user.click(screen.getByRole('button', { name: 'Save actions' }))
    expect(await screen.findByText('Event item.approved is reserved for staqly')).toBeInTheDocument()
  })
})
