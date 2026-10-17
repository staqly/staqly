import { screen, waitFor } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { describe, expect, it } from 'vitest'

import { BackupCard } from '@/components/settings/backup-card'
import { SettingsNav } from '@/components/settings/settings-nav'

import { mockApi, renderWithProviders } from './utils'

function usage(plan: string) {
  return { plan: { id: plan, name: plan }, usage: {}, limits: {} }
}

const READY = {
  id: 'exp_1',
  status: 'ready',
  size_bytes: 2048,
  created_at: '2026-10-16T10:00:00Z',
  started_at: null,
  finished_at: '2026-10-16T10:00:05Z',
  expires_at: '2026-10-17T10:00:00Z',
  download_url: '/v1/workspace/exports/exp_1/download',
  includes_media: true,
}

describe('backup card', () => {
  it('local install: the backup commands for staqly serve and the Docker image', async () => {
    mockApi({})
    renderWithProviders(<BackupCard />, { mode: 'local' })
    expect(screen.getByText('staqly serve backup ~/staqly-backup.db')).toBeInTheDocument()
    await userEvent.setup().click(screen.getByRole('tab', { name: 'Docker' }))
    expect(screen.getByText(/docker exec staqly server backup/)).toBeInTheDocument()
  })

  it('cloud, paid: lists backups with Download and creates one', async () => {
    const api = mockApi({
      'GET /v1/workspace/usage': usage('pro'),
      'GET /v1/workspace/exports': { data: [READY] },
      'POST /v1/workspace/export': { ...READY, id: 'exp_2', status: 'pending', download_url: '' },
    })

    renderWithProviders(<BackupCard />, { mode: 'cloud' })
    expect(await screen.findByRole('link', { name: /download/i })).toHaveAttribute(
      'href',
      '/api/v1/workspace/exports/exp_1/download',
    )
    await userEvent.setup().click(screen.getByRole('button', { name: /create backup/i }))
    await waitFor(() => expect(api.find('POST', '/v1/workspace/export')).toHaveLength(1))
  })

  it('cloud, Free: no backups, a pointer to the data-only export and the plans', async () => {
    const api = mockApi({ 'GET /v1/workspace/usage': usage('free'), 'GET /v1/workspace/exports': { data: [] } })

    renderWithProviders(<BackupCard />, { mode: 'cloud' })
    expect(await screen.findByRole('link', { name: 'See plans' })).toHaveAttribute('href', '/settings/plan')
    expect(screen.getByRole('link', { name: 'Export your data' })).toHaveAttribute('href', '/settings/data')
    expect(screen.getByText(/media files included, come with Pro/)).toBeInTheDocument()
    expect(screen.queryByTestId('cloud-backups')).not.toBeInTheDocument()
    expect(api.find('GET', '/v1/workspace/exports')).toHaveLength(0)
  })

  it('the Backups tab: on every plan, Free included', () => {
    mockApi({ 'GET /v1/workspace/usage': usage('free') })
    renderWithProviders(<SettingsNav />, { mode: 'cloud' })
    expect(screen.getByRole('link', { name: 'Backups' })).toHaveAttribute('href', '/settings/backups')
  })
})
