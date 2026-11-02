import { screen, waitFor } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { describe, expect, it } from 'vitest'

import { PostStatusBadge } from '@/components/common/status-badge'
import { PostActions } from '@/components/posts/post-actions'
import { PostStats } from '@/components/posts/post-stats'
import { TargetStatus } from '@/components/posts/target-status'
import { canUnpublish } from '@/lib/posts'

import { post, target } from './fixtures'
import { mockApi, renderWithProviders } from './utils'

const out = (over = {}) =>
  post({
    status: 'published',
    published_at: '2026-01-02T10:00:00Z',
    targets: [
      target({ status: 'published', external_url: 'https://m.example/1' }),
      target({ channel_id: 'ch_2', channel_name: 'Brand on Bluesky', provider: 'bluesky', status: 'published' }),
    ],
    ...over,
  })

describe('unpublish', () => {
  it('shows Unpublish only for went-out, not yet unpublished posts', () => {
    expect(canUnpublish(out())).toBe(true)
    expect(canUnpublish(out({ unpublished_at: '2026-01-03T10:00:00Z' }))).toBe(false)
    expect(canUnpublish(post())).toBe(false)
  })

  it('confirm dialog lists channels and sends the picked ones', async () => {
    const api = mockApi({
      'GET /v1/session': { mode: 'local', role: 'owner' },
      'GET /v1/posts/pst_1/comments': { data: [] },
      'POST /v1/posts/pst_1/unpublish': out({ unpublished_at: '2026-01-03T10:00:00Z' }),
    })

    renderWithProviders(<PostActions post={out()} onDeleted={() => {}} />)
    await userEvent.click(await screen.findByTestId('post-unpublish'))
    const boxes = await screen.findAllByRole('checkbox')

    expect(boxes).toHaveLength(2)
    expect(screen.getByText(/Remove by hand/)).toBeInTheDocument()
    await userEvent.click(boxes[1]!)
    await userEvent.click(screen.getByRole('button', { name: 'Unpublish' }))
    const url = '/v1/posts/pst_1/unpublish'

    await waitFor(() => expect(api.find('POST', url)).toHaveLength(1))
    expect(api.find('POST', url)[0]?.body).toEqual({ channel_ids: ['ch_1'] })
  })

  it('hides Unpublish for viewers', async () => {
    mockApi({ 'GET /v1/session': { mode: 'local', role: 'viewer' }, 'GET /v1/posts/pst_1/comments': { data: [] } })
    renderWithProviders(<PostActions post={out()} onDeleted={() => {}} />)
    await screen.findByTestId('post-preview')
    await waitFor(() => expect(screen.queryByTestId('post-unpublish')).toBeNull())
  })

  it('badge reads Unpublished', () => {
    renderWithProviders(<PostStatusBadge status="published" unpublishedAt="2026-01-03T10:00:00Z" />)
    expect(screen.getByText('Unpublished')).toBeInTheDocument()
  })

  it('statistics of a post taken down keep the last numbers, say so on top and read nothing more', async () => {
    const down = out({
      unpublished_at: '2026-01-03T10:00:00Z',
      targets: [target({ status: 'published', removal: 'removed', removed_at: '2026-01-03T10:00:00Z' })],
    })

    mockApi({
      'GET /v1/session': { mode: 'local', role: 'owner' },
      'GET /v1/posts/pst_1/metrics': {
        data: [
          {
            channel_id: 'ch_1',
            channel_name: 'Brand on X',
            provider: 'x',
            status: 'published',
            external_url: '',
            available: true,
            metrics: { impressions: 42 },
            updated_at: '2026-01-03T09:59:00Z',
          },
        ],
      },
      'GET /v1/posts/pst_1/responses': { overall: null, channels: [] },
    })
    renderWithProviders(<PostStats post={down} active />)
    expect(await screen.findByText('42')).toBeInTheDocument()
    expect(screen.getByTestId('stats-unpublished')).toHaveTextContent('last ones read')
    expect(screen.queryByRole('button', { name: /Refresh/ })).toBeNull()
  })

  it('target row reads Published, then Unpublished with when', () => {
    const removed = target({ status: 'published', removal: 'removed', removed_at: '2026-01-03T10:00:00Z' })

    renderWithProviders(<TargetStatus post={out()} target={removed} />)
    expect(screen.getByText('Published')).toBeInTheDocument()
    expect(screen.getByTestId('target-unpublished')).toHaveTextContent('Unpublished')
    expect(screen.queryByText('Removed')).toBeNull()
  })

  it('target row shows Unpublishing as a badge while the channel is taken down', () => {
    renderWithProviders(<TargetStatus post={out()} target={target({ status: 'published', removal: 'removing' })} />)
    expect(screen.getByTestId('target-unpublishing')).toHaveTextContent('Unpublishing')
  })

  it('target row shows the removal error as text and the link for manual removal', () => {
    const failed = target({ status: 'published', removal: 'failed', removal_error: '<b>x</b> refused' })
    const { container, unmount } = renderWithProviders(<TargetStatus post={out()} target={failed} />)

    expect(screen.getByTestId('removal-error')).toHaveTextContent('Unpublish failed: <b>x</b> refused')
    expect(container.querySelector('b')).toBeNull()
    unmount()

    const manual = target({ status: 'published', removal: 'manual', external_url: 'https://m.example/1' })

    renderWithProviders(<TargetStatus post={out()} target={manual} />)
    expect(screen.getByText(/Remove by hand/)).toBeInTheDocument()
    expect(screen.getByRole('link')).toHaveAttribute('href', 'https://m.example/1')
  })
})
