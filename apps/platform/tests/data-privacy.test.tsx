import { screen, waitFor, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { afterEach, describe, expect, it, vi } from 'vitest'

import { PostActions } from '@/components/posts/post-actions'
import { DataPrivacyPage } from '@/components/settings/data-privacy-page'
import type { WorkspaceExport } from '@/lib/api'
import { browser } from '@/lib/browser'
import { qk } from '@/lib/queries'

import { post } from './fixtures'
import { PLANS, usage } from './plans-fixtures'
import { makeClient, mockApi, renderWithProviders, sessionAs } from './utils'

const [STARTER, PLUS] = PLANS as [(typeof PLANS)[0], (typeof PLANS)[0]]

function exported(over: Partial<WorkspaceExport> = {}): WorkspaceExport {
  return {
    id: 'exp_1',
    status: 'ready',
    includes_media: true,
    size_bytes: 5 * 1024 ** 2,
    created_at: '2026-11-13T10:00:00Z',
    started_at: '2026-11-13T10:00:01Z',
    finished_at: '2026-11-13T10:00:09Z',
    expires_at: '2026-11-14T10:00:09Z',
    download_url: '/v1/workspace/exports/exp_1/download',
    ...over,
  }
}

afterEach(() => vi.restoreAllMocks())

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

describe('deleting a workspace', () => {
  it('asks for its name: a wrong one is said under the field and sends nothing, the right one deletes and signs out', async () => {
    const leave = vi.spyOn(browser, 'assign').mockImplementation(() => {})
    const api = mockApi({
      'GET /v1/workspace/usage': usage(PLUS),
      'DELETE /v1/workspace': { status: 204 },
    })
    const user = userEvent.setup()

    renderWithProviders(<DataPrivacyPage />, { role: 'owner' })
    await user.click(await screen.findByRole('button', { name: 'Delete workspace...' }))

    const dialog = await screen.findByRole('dialog')

    expect(within(dialog).getByRole('heading')).toHaveTextContent('Delete “Test WS” forever?')
    await user.type(within(dialog).getByLabelText(/Type the workspace name to confirm/), 'test ws')
    await user.click(within(dialog).getByRole('button', { name: 'Delete workspace' }))
    expect(await within(dialog).findByText('Type “Test WS” exactly to confirm')).toBeInTheDocument()
    expect(api.find('DELETE', '/v1/workspace')).toHaveLength(0)

    await user.clear(within(dialog).getByLabelText(/Type the workspace name to confirm/))
    await user.type(within(dialog).getByLabelText(/Type the workspace name to confirm/), 'Test WS')
    await user.click(within(dialog).getByRole('button', { name: 'Delete workspace' }))

    await waitFor(() => expect(leave).toHaveBeenCalledWith('/login'))
    expect(api.find('DELETE', '/v1/workspace')).toHaveLength(1)
    expect(api.find('DELETE', '/v1/workspace')[0]!.body).toEqual({ confirm: 'Test WS' })
    expect(await screen.findByText('Workspace deleted')).toBeInTheDocument()
  })

  it('an empty name deletes nothing, and Cancel closes without a request', async () => {
    const api = mockApi({ 'GET /v1/workspace/usage': usage(PLUS), 'DELETE /v1/workspace': { status: 204 } })
    const user = userEvent.setup()

    renderWithProviders(<DataPrivacyPage />, { role: 'owner' })
    await user.click(await screen.findByRole('button', { name: 'Delete workspace...' }))
    await user.click(within(await screen.findByRole('dialog')).getByRole('button', { name: 'Delete workspace' }))
    expect(await screen.findByText('Type “Test WS” exactly to confirm')).toBeInTheDocument()
    await user.click(screen.getByRole('button', { name: 'Cancel' }))
    await waitFor(() => expect(screen.queryByRole('dialog')).not.toBeInTheDocument())
    expect(api.find('DELETE', '/v1/workspace')).toHaveLength(0)
  })

  it('a refusal by the server is said and the person stays signed in', async () => {
    const leave = vi.spyOn(browser, 'assign').mockImplementation(() => {})

    mockApi({
      'GET /v1/workspace/usage': usage(PLUS),
      'DELETE /v1/workspace': {
        status: 409,
        body: { error: { code: 'conflict', message: 'cancel the subscription first' } },
      },
    })
    const user = userEvent.setup()

    renderWithProviders(<DataPrivacyPage />, { role: 'owner' })
    await user.click(await screen.findByRole('button', { name: 'Delete workspace...' }))
    await user.type(await screen.findByLabelText(/Type the workspace name to confirm/), 'Test WS')
    await user.click(within(screen.getByRole('dialog')).getByRole('button', { name: 'Delete workspace' }))

    expect(await screen.findByText('Could not delete the workspace')).toBeInTheDocument()
    expect(screen.getByText('Cancel the subscription first')).toBeInTheDocument()
    expect(leave).not.toHaveBeenCalled()
    expect(screen.getByRole('dialog')).toBeInTheDocument()
  })

  it('needs a signed-in person: a session made from an API key has the button off and says why', async () => {
    const owner = sessionAs('owner')
    const client = makeClient()

    client.setQueryData(qk.session, { ...owner, principal: { ...owner.principal, type: 'api_key' } })
    mockApi({ 'GET /v1/workspace/usage': usage(PLUS) })

    renderWithProviders(<DataPrivacyPage />, { client })

    expect(await screen.findByRole('button', { name: 'Delete workspace...' })).toBeDisabled()
    expect(screen.getByText('Only a signed-in app session can delete a workspace.')).toBeInTheDocument()
  })

  it('a local install has one workspace and no delete button: it says how to remove the data', async () => {
    mockApi({})

    renderWithProviders(<DataPrivacyPage />, { mode: 'local' })

    expect(await screen.findByTestId('local-delete')).toHaveTextContent('rm -rf ~/.local/share/staqly')
    expect(screen.queryByRole('button', { name: 'Delete workspace...' })).not.toBeInTheDocument()
  })
})

describe('the workspace export', () => {
  it('starts, says it is building, then offers the ZIP with its size', async () => {
    let job = exported({ status: 'pending', size_bytes: 0, started_at: null, finished_at: null })
    const api = mockApi({
      'GET /v1/workspace/usage': usage(PLUS),
      'POST /v1/workspace/export': () => ({ status: 202, body: job }),
      'GET /v1/workspace/exports/exp_1': () => job,
    })
    const user = userEvent.setup()
    const { client } = renderWithProviders(<DataPrivacyPage />, { role: 'admin' })

    await user.click(await screen.findByRole('button', { name: 'Export workspace' }))

    const status = await screen.findByTestId('export-status')

    expect(status).toHaveTextContent('Queued...')
    expect(screen.getByRole('button', { name: 'Export again' })).toBeDisabled()

    job = exported()
    await client.invalidateQueries({ queryKey: qk.workspaceExport('exp_1') })

    const download = await within(status).findByRole('link', { name: 'Download ZIP' })

    expect(download).toHaveAttribute('href', '/api/v1/workspace/exports/exp_1/download')
    expect(status).toHaveTextContent('5 MB')
    expect(screen.getByRole('button', { name: 'Export again' })).toBeEnabled()
    expect(api.find('POST', '/v1/workspace/export')).toHaveLength(1)
  })

  it('an export already running is shown instead of starting a second one', async () => {
    mockApi({
      'GET /v1/workspace/usage': usage(PLUS),
      'POST /v1/workspace/export': {
        status: 409,
        body: { error: { code: 'conflict', message: 'an export is running', details: { export_id: 'exp_1' } } },
      },
      'GET /v1/workspace/exports/exp_1': exported({ status: 'running', finished_at: null }),
    })
    const user = userEvent.setup()

    renderWithProviders(<DataPrivacyPage />, { role: 'owner' })
    await user.click(await screen.findByRole('button', { name: 'Export workspace' }))

    expect(await screen.findByText('An export is already running')).toBeInTheDocument()
    expect(await screen.findByTestId('export-status')).toHaveTextContent('Building your archive...')
  })

  it('an export that failed says why, and one that cannot start says so', async () => {
    let start: unknown = { status: 202, body: exported({ status: 'failed', error: 'disk full' }) }

    mockApi({
      'GET /v1/workspace/usage': usage(PLUS),
      'POST /v1/workspace/export': () => start,
      'GET /v1/workspace/exports/exp_1': exported({ status: 'failed', error: 'disk full' }),
    })
    const user = userEvent.setup()

    renderWithProviders(<DataPrivacyPage />, { role: 'owner' })
    await user.click(await screen.findByRole('button', { name: 'Export workspace' }))
    expect(await screen.findByText('The export failed: disk full')).toBeInTheDocument()

    start = { status: 402, body: { error: { code: 'plan_expired', message: 'the plan of this workspace has ended' } } }
    await user.click(screen.getByRole('button', { name: 'Export again' }))
    expect(await screen.findByText('This workspace is read-only')).toBeInTheDocument()
  })

  it('Starter gets its data without the media files, and is told what Plus adds', async () => {
    mockApi({ 'GET /v1/workspace/usage': usage(STARTER) })

    renderWithProviders(<DataPrivacyPage />, { role: 'owner' })

    expect(await screen.findByTestId('export-data-only')).toHaveTextContent(
      'Media files, backups and the activity log export come with Plus and above.',
    )
    expect(screen.getByText(/Media files are listed but not included/)).toBeInTheDocument()
    expect(screen.queryByTestId('export-all-csv')).not.toBeInTheDocument()
  })
})
