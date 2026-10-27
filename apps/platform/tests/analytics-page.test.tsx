import { screen, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { beforeEach, describe, expect, it } from 'vitest'

import type { Analytics, ChannelAnalytics } from '@/components/analytics/analytics-data'
import { AnalyticsPage, readParams } from '@/components/analytics/analytics-page'
import { numbersNote } from '@/components/analytics/analytics-sections'

import { nav } from './navigation'
import { mockApi, renderWithProviders } from './utils'

function channel(over: Partial<ChannelAnalytics> = {}): ChannelAnalytics {
  return {
    channel_id: 'ch_x',
    name: 'Brand on X',
    provider: 'x',
    handle: '@brand',
    avatar_url: '',
    status: 'connected',
    published: 8,
    failed: 2,
    success_rate: 0.8,
    last_published_at: '2026-10-26T09:00:00Z',
    top_failure: 'rate limit hit',
    metrics: {
      impressions: 1200,
      engagements: 40,
      likes: 30,
      replies: 10,
      reposts: null,
      quotes: null,
      bookmarks: null,
      link_clicks: null,
    },
    metrics_coverage: { support: 'platform', published: 8, with_data: 5, updated_at: '2026-10-27T08:00:00Z' },
    avg_engagements: 8,
    ...over,
  }
}

function report(over: Partial<Analytics> = {}): Analytics {
  const totals = {
    posts_created: 9,
    posts_published: 7,
    targets_published: 12,
    targets_failed: 2,
    success_rate: 12 / 14,
    retried: 1,
    delay_avg_seconds: 42,
    delay_p90_seconds: 120,
    metrics: null,
  }

  return {
    range: {
      from: '2026-10-21',
      to: '2026-10-27',
      days: 7,
      timezone: 'Europe/Warsaw',
      start: '2026-10-20T22:00:00.000000Z',
      end: '2026-10-27T23:00:00.000000Z',
      previous_from: '2026-10-14',
      previous_to: '2026-10-20',
    },
    totals,
    previous: { ...totals, posts_published: 5, targets_published: 10, targets_failed: 4, success_rate: 10 / 14 },
    queue: { scheduled: 3, drafts: 2, next_publish_at: '2026-10-28T09:00:00Z' },
    daily: ['21', '22', '23', '24', '25', '26', '27'].map((d, i) => ({
      date: `2026-10-${d}`,
      published: i,
      failed: i === 3 ? 2 : 0,
    })),
    channels: [
      channel(),
      channel({
        channel_id: 'ch_t',
        name: 'Updates on Telegram',
        provider: 'telegram',
        published: 4,
        failed: 0,
        success_rate: 1,
        top_failure: null,
        metrics: null,
        avg_engagements: null,
        metrics_coverage: { support: 'none', published: 4, with_data: 0, updated_at: null },
      }),
    ],
    sources: [
      {
        source: 'dashboard',
        kind: 'dashboard',
        label: 'Dashboard',
        posts_created: 5,
        posts_published: 4,
        targets_published: 8,
        targets_failed: 0,
        success_rate: 1,
      },
      {
        source: 'Claude Code',
        kind: 'mcp',
        label: 'Claude Code',
        posts_created: 4,
        posts_published: 3,
        targets_published: 4,
        targets_failed: 2,
        success_rate: 4 / 6,
      },
    ],
    posting_times: {
      cells: [{ weekday: 1, hour: 10, published: 6, samples: 2, avg_engagements: 8.5, low_confidence: true }],
      best: [{ weekday: 1, hour: 10, published: 6, samples: 2, avg_engagements: 8.5, low_confidence: true }],
      min_samples: 5,
    },
    top_posts: {
      by: 'engagements',
      posts: [
        {
          post_id: 'pst_top',
          excerpt: 'Launch day',
          published_at: '2026-10-22T08:00:00Z',
          channels: [{ channel_id: 'ch_x', provider: 'x', external_url: 'https://x.com/brand/status/1' }],
          metrics: {
            impressions: 900,
            engagements: 33,
            likes: 30,
            replies: 3,
            reposts: null,
            quotes: null,
            bookmarks: null,
            link_clicks: null,
          },
        },
      ],
    },
    failures: [
      {
        reason: 'rate limit hit, retry after 30 seconds',
        count: 2,
        channels: ['ch_x'],
        last_at: '2026-10-24T10:00:00Z',
        sample_post_id: 'pst_bad',
      },
    ],
    feedback: { opened: 1, resolved: 1, open: 0 },
    ...over,
  }
}

function setup(data: Analytics = report()) {
  return mockApi({
    'GET /v1/analytics': data,
    'GET /v1/channels': {
      data: [
        { id: 'ch_x', provider: 'x', name: 'Brand on X', status: 'connected' },
        { id: 'ch_t', provider: 'telegram', name: 'Updates on Telegram', status: 'connected' },
      ],
    },
  })
}

describe('analytics page', () => {
  beforeEach(() => nav.set('/analytics'))

  it('shows the tiles with real comparisons, the chart summary, channels, sources, failures and top posts', async () => {
    const api = setup()

    renderWithProviders(<AnalyticsPage />, { mode: 'local' })
    const tiles = await screen.findAllByTestId('kpi-tile')

    expect(api.find('GET', '/v1/analytics')[0].query.get('days')).toBe('30')
    expect(tiles).toHaveLength(5)
    expect(tiles[0]).toHaveTextContent('Posts published')
    expect(tiles[0]).toHaveTextContent('Period total')
    expect(tiles[0]).toHaveTextContent('+2 vs previous 7 days')
    expect(tiles[2]).toHaveTextContent('-2 vs previous 7 days')
    // Right now has nothing to compare with.
    expect(within(tiles[3]).queryByTestId('kpi-delta')).toBeNull()
    expect(tiles[4]).toHaveTextContent('42 s')

    expect(screen.getByTestId('daily-chart')).toHaveAccessibleName(/21 published and 2 failed over 7 days/)
    expect(screen.getByRole('table', { name: /published and failed channels per day/i })).toBeInTheDocument()

    const rows = screen.getAllByTestId('channel-row')

    expect(rows[0]).toHaveTextContent('Brand on X')
    expect(rows[0]).toHaveTextContent('1,200')
    expect(rows[1]).toHaveTextContent("This platform doesn't report numbers to staqly yet")

    expect(screen.getAllByTestId('source-row')[1]).toHaveTextContent('Claude Code')
    expect(screen.getAllByTestId('source-row')[1]).toHaveTextContent('MCP')
    expect(
      within(screen.getByTestId('failures-list')).getByRole('link', { name: 'Open an example post' }),
    ).toHaveAttribute('href', '/posts/pst_bad')
    expect(within(screen.getByTestId('failures-list')).getByRole('link', { name: /Brand on X/ })).toHaveAttribute(
      'href',
      '/posts?status=failed&channel=ch_x',
    )
    expect(within(screen.getByTestId('top-posts')).getByRole('link', { name: 'Launch day' })).toHaveAttribute(
      'href',
      '/posts/pst_top',
    )
    expect(screen.getByTestId('best-times')).toHaveTextContent('Tuesday 10:00')
    expect(screen.getByTestId('best-times')).toHaveTextContent('low confidence')
  })

  it('keeps the range and filters in the URL', async () => {
    const api = setup()

    nav.set('/analytics', 'days=7&channel=ch_x')
    renderWithProviders(<AnalyticsPage />, { mode: 'local' })

    await screen.findAllByTestId('kpi-tile')
    const q = api.find('GET', '/v1/analytics')[0].query

    expect(q.get('days')).toBe('7')
    expect(q.get('channel')).toBe('ch_x')
    expect(screen.getByRole('radio', { name: 'Last 7 days' })).toHaveAttribute('aria-checked', 'true')

    await userEvent.click(screen.getByRole('radio', { name: 'Last 90 days' }))
    expect(nav.replace).toHaveBeenLastCalledWith('/analytics?days=90&channel=ch_x', { scroll: false })

    await userEvent.selectOptions(screen.getByRole('combobox', { name: 'Source' }), 'Claude Code')
    expect(nav.replace).toHaveBeenLastCalledWith('/analytics?days=7&channel=ch_x&source=Claude+Code', {
      scroll: false,
    })
  })

  it('reads custom ranges and ignores unknown presets', () => {
    expect(readParams(new URLSearchParams('from=2026-10-01&to=2026-10-10'))).toEqual({
      from: '2026-10-01',
      to: '2026-10-10',
    })
    expect(readParams(new URLSearchParams('days=12'))).toEqual({ days: 30 })
    expect(readParams(new URLSearchParams('source='))).toEqual({ days: 30, source: '' })
  })

  it('says what to do when there is nothing yet', async () => {
    const empty = report({
      totals: { ...report().totals, posts_created: 0, posts_published: 0, targets_published: 0, targets_failed: 0 },
      queue: { scheduled: 0, drafts: 0, next_publish_at: null },
    })

    setup(empty)
    renderWithProviders(<AnalyticsPage />, { mode: 'local' })

    expect(await screen.findByText('No data yet in this range')).toBeInTheDocument()
    expect(screen.getByRole('link', { name: /write a post/i })).toHaveAttribute('href', '/posts?compose=1')
  })

  it('asks for a channel first when there is none', async () => {
    mockApi({
      'GET /v1/analytics': report({
        channels: [],
        totals: { ...report().totals, posts_created: 0, targets_published: 0, targets_failed: 0 },
        queue: { scheduled: 0, drafts: 0, next_publish_at: null },
      }),
      'GET /v1/channels': { data: [] },
    })
    renderWithProviders(<AnalyticsPage />, { mode: 'cloud' })

    expect(await screen.findByRole('link', { name: 'Connect a channel' })).toHaveAttribute('href', '/channels')
  })

  it('tells apart no numbers from the platform, none yet and none in the range', () => {
    expect(
      numbersNote(channel({ metrics_coverage: { support: 'none', published: 3, with_data: 0, updated_at: null } })),
    ).toMatch(/doesn't report numbers/)
    expect(
      numbersNote(channel({ metrics_coverage: { support: 'platform', published: 0, with_data: 0, updated_at: null } })),
    ).toBe('No data yet in this range')
    expect(
      numbersNote(channel({ metrics_coverage: { support: 'platform', published: 3, with_data: 0, updated_at: null } })),
    ).toMatch(/Statistics tab/)
    expect(numbersNote(channel())).toBeNull()
  })

  it('shows the error with a way to try again', async () => {
    mockApi({
      'GET /v1/analytics': { status: 403, body: { error: { code: 'forbidden', message: 'missing scope posts:read' } } },
      'GET /v1/channels': { data: [] },
    })
    renderWithProviders(<AnalyticsPage />, { mode: 'cloud' })

    expect(await screen.findByText('Could not load analytics')).toBeInTheDocument()
    expect(screen.getByRole('button', { name: /try again/i })).toBeInTheDocument()
  })
})
