import { screen, waitFor, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { beforeAll, describe, expect, it } from 'vitest'

import { TableView } from '@/components/collections/item-views'
import { ExternalContentBadge } from '@/components/common/external-badge'
import { ItemPreview } from '@/components/items/item-preview'
import { WatchersPage } from '@/components/watchers/watchers-page'
import type { Watcher } from '@/lib/api'
import { runResultMessage } from '@/lib/watchers'

import { collection, item } from './fixtures'
import { PLANS, UNLOCKED, usage } from './plans-fixtures'
import { mockApi, renderWithProviders, type Call } from './utils'

function watcher(over: Partial<Watcher> = {}): Watcher {
  return {
    id: 'wat_1',
    name: 'Hacker News – Front page',
    kind: 'rss',
    url: 'https://hnrss.org/frontpage',
    collection_id: 'col_1',
    collection_ids: ['col_1'],
    keywords: [],
    exclude_keywords: [],
    every: '30m0s',
    every_seconds: 1800,
    status: 'active',
    last_error: '',
    last_run_at: null,
    next_run_at: '2026-10-13T12:00:00Z',
    items_created: 0,
    fail_count: 0,
    created_at: '2026-10-01T00:00:00Z',
    updated_at: '2026-10-01T00:00:00Z',
    ...over,
  }
}

const COLLECTIONS = { data: [collection(), collection({ id: 'col_2', name: 'Inspiration' })] }

describe('watcher helpers', () => {
  it('describes check-now results', () => {
    expect(runResultMessage({ status: 'ok', new_items: 0, entries_seen: 20, baseline: true }).title).toBe(
      'Baseline recorded — new posts will appear from now on',
    )
    expect(runResultMessage({ status: 'ok', new_items: 3, entries_seen: 20, baseline: false }).title).toBe(
      '3 new items',
    )
    expect(runResultMessage({ status: 'ok', new_items: 1, entries_seen: 20, baseline: false }).title).toBe('1 new item')
    expect(
      runResultMessage({ status: 'error', new_items: 0, entries_seen: 0, baseline: false, error: 'HTTP 404' }),
    ).toEqual({
      tone: 'error',
      title: 'Check failed',
      description: 'HTTP 404',
    })
  })
})

describe('watchers page', () => {
  // The add/edit sheet is loaded on first open (next/dynamic); compile it before the first test clicks.
  beforeAll(async () => {
    await import('@/components/watchers/watcher-sheet')
  })

  it('creates a watcher in one request; staqly detects the kind from the URL', async () => {
    const api = mockApi({
      'GET /v1/watchers': { data: [] },
      'GET /v1/collections': COLLECTIONS,
      'GET /v1/workspace/usage': usage(UNLOCKED),
      'POST /v1/watchers': (c: Call) => ({ status: 201, body: watcher({ ...(c.body as object), id: 'wat_2' }) }),
    })
    const user = userEvent.setup()

    renderWithProviders(<WatchersPage />, { mode: 'local' })
    await user.click((await screen.findAllByRole('button', { name: /add watcher/i }))[0])
    const sheet = await screen.findByRole('dialog')

    expect(within(sheet).queryByRole('radio')).not.toBeInTheDocument()
    await user.type(within(sheet).getByLabelText('URL*'), 'not a url')
    await user.type(within(sheet).getByLabelText(/^name/i), 'Pricing page')
    await user.click(within(sheet).getByRole('button', { name: /start watching/i }))
    expect(await within(sheet).findByText('Enter an absolute http(s) URL')).toBeInTheDocument()
    await user.clear(within(sheet).getByLabelText('URL*'))
    await user.type(within(sheet).getByLabelText('URL*'), 'https://example.com/pricing')
    // Local install: every interval is available.
    expect(within(within(sheet).getByLabelText('Check')).getByRole('option', { name: 'Every minute' })).toBeEnabled()
    await user.click(within(sheet).getByText('Advanced: filter entries'))
    await user.type(within(sheet).getByLabelText('Keywords'), 'price, plan{Enter}')
    // Several collections: each new entry goes to all of them.
    await user.click(within(sheet).getByRole('button', { name: 'Collections' }))
    await user.click(await screen.findByRole('option', { name: 'Inspiration' }))
    await user.keyboard('{Escape}')
    await user.click(within(sheet).getByRole('button', { name: /start watching/i }))
    await waitFor(() => expect(api.find('POST', '/v1/watchers')).toHaveLength(1))
    expect(api.find('POST', '/v1/watchers')[0].body).toEqual({
      name: 'Pricing page',
      url: 'https://example.com/pricing',
      collections: ['col_1', 'col_2'],
      every: '1h',
      keywords: ['price', 'plan'],
      exclude_keywords: [],
    })
  })

  it('shows watcher cards and runs Check now / Pause with result toasts', async () => {
    let runs = 0
    const api = mockApi({
      'GET /v1/watchers': {
        data: [
          watcher({ last_run_at: '2026-10-13T09:00:00Z', items_created: 7 }),
          watcher({
            id: 'wat_2',
            name: 'Design blog',
            url: 'https://blog.example.com/feed.xml',
            status: 'degraded',
            fail_count: 5,
            last_error: 'HTTP 503 from blog.example.com',
          }),
          watcher({ id: 'wat_3', name: 'Old feed', status: 'paused' }),
        ],
      },
      'GET /v1/collections': COLLECTIONS,
      'GET /v1/workspace/usage': usage(PLANS[0], { watchers: 2 }),
      'POST /v1/watchers/wat_1/run': () =>
        ++runs === 1
          ? { status: 'ok', new_items: 0, entries_seen: 30, baseline: true }
          : { status: 'ok', new_items: 3, entries_seen: 30, baseline: false },
      'POST /v1/watchers/wat_2/run': {
        status: 'error',
        new_items: 0,
        entries_seen: 0,
        baseline: false,
        error: 'HTTP 503 from blog.example.com',
      },
      'PATCH /v1/watchers/*': (c: Call) =>
        watcher({ id: c.path.split('/').pop(), name: 'Old feed', ...(c.body as object) }),
      'GET /v1/items': { data: [] },
      'GET /v1/events': { data: [] },
    })
    const user = userEvent.setup()

    renderWithProviders(<WatchersPage />, { mode: 'cloud' })

    const cards = await screen.findAllByTestId('watcher-card')

    expect(cards).toHaveLength(3)
    expect(cards[0]).toHaveTextContent('hnrss.org')
    expect(cards[0]).toHaveTextContent('Instagram Avatar')
    expect(cards[0]).toHaveTextContent('every 30 min')
    expect(within(cards[0]).getByTestId('watcher-items')).toHaveTextContent('7')
    expect(within(cards[1]).getByTestId('watcher-status')).toHaveTextContent('Degraded')
    expect(cards[1]).toHaveTextContent('5 failed checks in a row. HTTP 503 from blog.example.com')
    expect(within(cards[2]).getByTestId('watcher-status')).toHaveTextContent('Paused')

    await user.click(within(cards[0]).getByRole('button', { name: /check now/i }))
    expect(await screen.findByText('Baseline recorded — new posts will appear from now on')).toBeInTheDocument()
    await user.click(within(cards[0]).getByRole('button', { name: /check now/i }))
    expect(await screen.findByText('3 new items')).toBeInTheDocument()
    await user.click(within(cards[1]).getByRole('button', { name: /check now/i }))
    expect(await screen.findByText('Check failed')).toBeInTheDocument()

    await user.click(within(cards[2]).getByRole('button', { name: /resume/i }))
    await waitFor(() => expect(api.find('PATCH', '/v1/watchers/wat_3')[0]?.body).toEqual({ status: 'active' }))
    await user.click(within(cards[0]).getByRole('button', { name: /pause/i }))
    await waitFor(() => expect(api.find('PATCH', '/v1/watchers/wat_1')[0]?.body).toEqual({ status: 'paused' }))
  })

  it('shows the plan-limit toast when a watcher would exceed the plan', async () => {
    mockApi({
      'GET /v1/watchers': { data: [] },
      'GET /v1/collections': COLLECTIONS,
      'GET /v1/workspace/usage': usage(PLANS[0], { watchers: 3 }),
      'GET /v1/plans': { data: PLANS },
      'POST /v1/watchers': {
        status: 402,
        body: {
          error: {
            code: 'plan_limit_exceeded',
            message: 'The Free plan allows 3 watchers.',
            details: { limit: 'watchers', max: 3, used: 3, plan: 'free' },
          },
        },
      },
    })
    const user = userEvent.setup()

    renderWithProviders(<WatchersPage />, { mode: 'cloud' })
    await user.click((await screen.findAllByRole('button', { name: /add watcher/i }))[0])
    const sheet = await screen.findByRole('dialog')

    await user.type(await within(sheet).findByLabelText('URL*'), 'https://example.com/feed.xml')
    await user.type(within(sheet).getByLabelText(/^name/i), 'Blog')
    await user.click(within(sheet).getByRole('button', { name: /start watching/i }))
    expect(await screen.findByText('Plan limit reached')).toBeInTheDocument()
    expect(screen.getByText('Your Free plan includes 3 active watchers (3 used).')).toBeInTheDocument()
  })
})

describe('untrusted external content', () => {
  const external = item({
    id: 'itm_ext',
    type: 'link',
    title: 'Show HN: a thing',
    url: 'https://news.example.com/post',
    media: null,
    content: 'Summary from the feed',
    metadata: {
      untrusted: true,
      watcher: { id: 'wat_1', name: 'HN', kind: 'rss', source_url: 'https://hnrss.org/frontpage' },
    },
  })

  it('marks only untrusted items', () => {
    renderWithProviders(
      <>
        <ExternalContentBadge item={external} />
        <ExternalContentBadge item={item()} />
        <ExternalContentBadge item={item({ metadata: { untrusted: 'yes' } })} />
      </>,
    )
    const badges = screen.getAllByTestId('external-badge')

    expect(badges).toHaveLength(1)
    expect(badges[0]).toHaveTextContent('External content')
    expect(badges[0]).toHaveAttribute('title', expect.stringContaining('hnrss.org'))
  })

  it('shows the badge in the collection table and opens links with nofollow', () => {
    renderWithProviders(
      <>
        <TableView items={[external, item()]} onOpen={() => {}} />
        <ItemPreview item={external} />
      </>,
    )
    expect(screen.getAllByTestId('external-badge')).toHaveLength(1)
    const links = screen.getAllByRole('link').filter((a) => a.getAttribute('href') === 'https://news.example.com/post')

    expect(links.length).toBeGreaterThan(0)

    for (const a of links) {
      expect(a).toHaveAttribute('rel', 'noopener noreferrer nofollow')
      expect(a).toHaveAttribute('target', '_blank')
    }
  })
})
