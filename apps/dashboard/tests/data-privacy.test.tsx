import { screen, waitFor, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { beforeEach, describe, expect, it, vi } from 'vitest'

import { ActivityFeed } from '@/components/activity/activity-feed'
import { Inbox } from '@/components/inbox/inbox'
import { ItemDetail } from '@/components/items/item-detail'
import { DataPrivacyPage } from '@/components/settings/data-privacy-page'
import { SettingsNav } from '@/components/settings/settings-nav'
import { SystemPage, checkOk } from '@/components/settings/system-page'
import type { WorkspaceExport } from '@/lib/api'
import { browser } from '@/lib/browser'

import { APPROVE, REJECT, collection, item } from './fixtures'
import { nav } from './navigation'
import { mockApi, renderWithProviders } from './utils'

function exportJob(over: Partial<WorkspaceExport> = {}): WorkspaceExport {
  return {
    id: 'exp_1',
    status: 'pending',
    size_bytes: 0,
    created_at: '2026-10-13T10:00:00Z',
    started_at: null,
    finished_at: null,
    expires_at: '2026-10-14T10:00:00Z',
    ...over,
  }
}

const READY = exportJob({
  status: 'ready',
  size_bytes: 4096,
  started_at: '2026-10-13T10:00:01Z',
  finished_at: '2026-10-13T10:00:02Z',
  download_url: '/v1/workspace/exports/exp_1/download',
})

describe('settings navigation', () => {
  it('has Notifications, Data & privacy and System sections', () => {
    nav.set('/settings/data')
    renderWithProviders(<SettingsNav />)
    expect(screen.getByRole('link', { name: 'Notifications' })).toHaveAttribute('href', '/settings/notifications')
    expect(screen.getByRole('link', { name: 'Data & privacy' })).toHaveAttribute('aria-current', 'page')
    expect(screen.getByRole('link', { name: 'System' })).toHaveAttribute('href', '/settings/system')
  })
})

describe('activity export', () => {
  beforeEach(() => nav.set('/activity', 'type=item.approved&actor_type=api_key&collection=col_1'))

  it('exports NDJSON and CSV for the current filters through the proxy', async () => {
    mockApi({ 'GET /v1/events': { data: [] }, 'GET /v1/collections': { data: [collection()] } })
    const user = userEvent.setup()

    renderWithProviders(<ActivityFeed />, { mode: 'cloud' })
    await user.click(await screen.findByRole('button', { name: /export/i }))
    const menu = await screen.findByRole('menu')

    expect(within(menu).getByText(/matching the current filters/)).toBeInTheDocument()
    const ndjson = new URL(
      within(menu)
        .getByRole('menuitem', { name: /ndjson/i })
        .getAttribute('href')!,
      'http://x',
    )

    expect(ndjson.pathname).toBe('/api/v1/events/export')
    expect(Object.fromEntries(ndjson.searchParams)).toEqual({
      format: 'ndjson',
      type: 'item.approved',
      actor_type: 'api_key',
      collection_id: 'col_1',
    })
    const csv = within(menu).getByRole('menuitem', { name: /csv/i })

    expect(csv.getAttribute('href')).toContain('format=csv')
    expect(csv).toHaveAttribute('download')
  })
})

describe('data & privacy', () => {
  beforeEach(() => nav.set('/settings/data'))

  it('starts a workspace export, polls it and offers the ZIP download', async () => {
    const api = mockApi({
      'POST /v1/workspace/export': { status: 202, body: exportJob() },
      'GET /v1/workspace/exports/exp_1': READY,
    })
    const user = userEvent.setup()

    renderWithProviders(<DataPrivacyPage />, { mode: 'local' })
    expect(screen.getByTestId('export-all-csv')).toHaveAttribute('href', '/api/v1/events/export?format=csv')
    await user.click(screen.getByRole('button', { name: /export workspace/i }))
    await waitFor(() => expect(api.find('POST', '/v1/workspace/export')).toHaveLength(1))
    const link = await screen.findByRole('link', { name: /download zip/i })

    expect(link).toHaveAttribute('href', '/api/v1/workspace/exports/exp_1/download')
    expect(screen.getByTestId('export-status')).toHaveTextContent(/4 KB · available until/)
    expect(api.find('GET', '/v1/workspace/exports/exp_1').length).toBeGreaterThan(0)
  })

  it('shows the export that is already running on 409', async () => {
    mockApi({
      'POST /v1/workspace/export': {
        status: 409,
        body: { error: { code: 'conflict', message: 'an export is already running', details: { export_id: 'exp_9' } } },
      },
      'GET /v1/workspace/exports/exp_9': exportJob({
        id: 'exp_9',
        status: 'running',
        started_at: '2026-10-13T10:00:01Z',
      }),
    })
    const user = userEvent.setup()

    renderWithProviders(<DataPrivacyPage />, { mode: 'cloud' })
    await user.click(screen.getByRole('button', { name: /export workspace/i }))
    expect(await screen.findByText('An export is already running')).toBeInTheDocument()
    expect(await screen.findByText(/Building your archive/)).toBeInTheDocument()
    expect(screen.getByTestId('export-status')).toHaveAttribute('data-status', 'running')
    expect(screen.getByRole('button', { name: /export again/i })).toBeDisabled()
  })

  it('cloud: deletes the workspace only after typing its name, then goes to /setup', async () => {
    const api = mockApi({ 'DELETE /v1/workspace': { status: 204 } })
    const assign = vi.spyOn(browser, 'assign').mockImplementation(() => {})
    const user = userEvent.setup()

    renderWithProviders(<DataPrivacyPage />, { mode: 'cloud' })
    expect(screen.queryByTestId('local-delete')).not.toBeInTheDocument()
    await user.click(within(screen.getByTestId('danger-zone')).getByRole('button', { name: /delete workspace/i }))
    const dialog = await screen.findByRole('dialog')
    const confirm = within(dialog).getByRole('button', { name: 'Delete workspace' })

    const box = within(dialog).getByLabelText(/to confirm/)

    expect(within(dialog).getByText(/to confirm/, { selector: 'label' })).toHaveTextContent(/confirm\*$/)
    expect(box).toHaveAttribute('aria-required', 'true')
    await user.type(box, 'Test W')
    await user.click(confirm)
    expect(within(dialog).getByRole('alert')).toHaveTextContent('Type “Test WS” exactly to confirm')
    expect(box).toHaveAttribute('aria-invalid', 'true')
    expect(box.closest('[data-slot=field]')).toHaveAttribute('data-invalid', 'true')
    expect(api.find('DELETE', '/v1/workspace')).toHaveLength(0)
    await user.type(box, 'S')
    expect(within(dialog).queryByRole('alert')).not.toBeInTheDocument()
    await user.click(confirm)
    await waitFor(() => expect(api.find('DELETE', '/v1/workspace')).toHaveLength(1))
    expect(api.find('DELETE', '/v1/workspace')[0].body).toEqual({ confirm: 'Test WS' })
    await waitFor(() => expect(assign).toHaveBeenCalledWith('/setup'))
    assign.mockRestore()
  })

  it('cloud: a server error on the confirmation shows under the field until it changes', async () => {
    const api = mockApi({
      'DELETE /v1/workspace': {
        status: 422,
        body: { error: { code: 'validation_failed', message: 'x', fields: { confirm: 'does not match' } } },
      },
    })
    const user = userEvent.setup()

    renderWithProviders(<DataPrivacyPage />, { mode: 'cloud' })
    await user.click(within(screen.getByTestId('danger-zone')).getByRole('button', { name: /delete workspace/i }))
    const dialog = await screen.findByRole('dialog')
    const box = within(dialog).getByLabelText(/to confirm/)

    await user.type(box, 'Test WS')
    await user.click(within(dialog).getByRole('button', { name: 'Delete workspace' }))
    await waitFor(() => expect(api.find('DELETE', '/v1/workspace')).toHaveLength(1))
    expect(await within(dialog).findByRole('alert')).toHaveTextContent('Does not match')
    await user.type(box, ' ')
    expect(within(dialog).getByRole('alert')).toHaveTextContent('Type “Test WS” exactly to confirm')
    await user.type(box, '{Backspace}')
    expect(within(dialog).queryByRole('alert')).not.toBeInTheDocument()
  })

  it('local: explains deleting the data (staqly serve or the Docker image) instead of offering a delete button', async () => {
    mockApi({})
    renderWithProviders(<DataPrivacyPage />, { mode: 'local' })
    expect(screen.queryByTestId('danger-zone')).not.toBeInTheDocument()
    expect(screen.getByTestId('local-delete')).toHaveTextContent('rm -rf ~/.local/share/staqly')
    await userEvent.setup().click(screen.getByRole('tab', { name: 'Docker' }))
    expect(screen.getByTestId('local-delete')).toHaveTextContent('docker volume rm staqly-data')
    expect(screen.queryByRole('button', { name: /delete workspace/i })).not.toBeInTheDocument()
  })
})

describe('delete and erase the history', () => {
  it('one Delete; ticking "Also erase its history" purges instead', async () => {
    const api = mockApi({
      'GET /v1/items/itm_1': item({ title: 'Avatar v1' }),
      'GET /v1/items/itm_1/actions': { data: [APPROVE, REJECT] },
      'GET /v1/items/itm_1/events': { data: [] },
      'GET /v1/items/itm_1/versions': { data: [] },
      'GET /v1/collections/col_1': collection(),
      'POST /v1/items/itm_1/purge': { item_id: 'itm_1', events_deleted: 3, media_deleted: true },
    })
    const user = userEvent.setup()

    renderWithProviders(<ItemDetail id="itm_1" />, { mode: 'local' })
    await user.click(await screen.findByRole('button', { name: 'More actions' }))
    expect(screen.queryByRole('menuitem', { name: /purge/i })).not.toBeInTheDocument()
    await user.click(await screen.findByRole('menuitem', { name: /delete item/i }))
    const dialog = await screen.findByRole('alertdialog')

    await user.click(within(dialog).getByRole('checkbox', { name: 'Also erase its history' }))
    expect(dialog).toHaveTextContent('This cannot be undone.')
    await user.click(within(dialog).getByRole('button', { name: 'Delete and erase history' }))
    await waitFor(() => expect(api.find('POST', '/v1/items/itm_1/purge')).toHaveLength(1))
    expect(await screen.findByText('“Avatar v1” was deleted and its history erased')).toBeInTheDocument()
    expect(screen.getByText('3 events erased, file deleted.')).toBeInTheDocument()
    expect(nav.push).toHaveBeenCalledWith('/collections/col_1')
  })
})

describe('system status', () => {
  it('shows readiness checks from /readyz (503 still renders the checks)', async () => {
    mockApi({
      'GET /readyz': {
        status: 503,
        body: { status: 'unavailable', checks: { database: 'ok', migrations: 'pending (3 < 4)', workers: 'stale' } },
      },
      'GET /health': { status: 'ok', version: '1.4.0' },
    })
    renderWithProviders(<SystemPage />, { mode: 'local' })
    const card = await screen.findByTestId('readiness')

    expect(card).toHaveAttribute('data-status', 'unavailable')
    expect(card).toHaveTextContent('Not ready')
    expect(card).toHaveTextContent('pending (3 < 4)')
    expect(await within(card).findByText('v1.4.0')).toBeInTheDocument()
    expect(checkOk('workers', 'not_started')).toBe(true)
    expect(checkOk('workers', 'stale')).toBe(false)
  })
})

describe('rejected step', () => {
  it('the inbox lists rejected and expired items under step=rejected', async () => {
    nav.set('/inbox', 'step=rejected')
    const api = mockApi({
      'GET /v1/items': { data: [] },
      'GET /v1/collections': { data: [collection()] },
    })

    renderWithProviders(<Inbox />, { mode: 'local' })
    await waitFor(() =>
      expect(api.find('GET', '/v1/items').some((c) => c.query.get('decision') === 'rejected')).toBe(true),
    )
    expect((await screen.findAllByText('Nothing rejected')).length).toBeGreaterThan(0)
  })
})
