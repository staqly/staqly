import { screen, waitFor, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { describe, expect, it } from 'vitest'

import { GalleryView, ListView } from '@/components/collections/item-views'
import { ExternalContentBadge } from '@/components/common/external-badge'
import { ItemPreview } from '@/components/items/item-preview'
import { WatchersPage } from '@/components/watchers/watchers-page'
import type { Watcher, WatcherPreset } from '@/lib/api'
import { defaultInterval, parseDuration, runResultMessage } from '@/lib/watchers'

import { collection, item } from './fixtures'
import { PLANS, UNLOCKED, usage } from './plans-fixtures'
import { mockApi, renderWithProviders, type Call } from './utils'

const PRESETS: WatcherPreset[] = [
  {
    id: 'hn-front-page',
    title: 'Hacker News – Front page',
    category: 'Community',
    kind: 'rss',
    description: 'Stories reaching the Hacker News front page.',
    default_every: '30m',
    params: [],
  },
  {
    id: 'reddit-subreddit',
    title: 'Reddit – Subreddit',
    category: 'Community',
    kind: 'rss',
    description: 'New posts in a subreddit.',
    default_every: '30m',
    params: [{ name: 'subreddit', label: 'Subreddit', required: true, placeholder: 'midjourney' }],
  },
  {
    id: 'pinterest-board',
    title: 'Pinterest – Board',
    category: 'Inspiration',
    kind: 'rss',
    description: 'New pins on a public Pinterest board.',
    default_every: '6h',
    params: [
      { name: 'user', label: 'User', required: true },
      { name: 'board', label: 'Board', required: true },
    ],
  },
  {
    id: 'rss-feed',
    title: 'Any RSS / Atom feed',
    category: 'Content',
    kind: 'rss',
    description: 'Any RSS or Atom feed URL.',
    default_every: '1h',
    params: [
      { name: 'url', label: 'Feed URL', required: true },
      { name: 'keywords', label: 'Keywords (comma-separated, optional)', required: false },
    ],
  },
  {
    id: 'url-changed',
    title: 'Web page changed',
    category: 'Monitoring',
    kind: 'url_diff',
    description: 'Fires when the visible text of a page changes.',
    default_every: '6h',
    params: [{ name: 'url', label: 'Page URL', required: true }],
  },
]

function watcher(over: Partial<Watcher> = {}): Watcher {
  return {
    id: 'wat_1',
    name: 'Hacker News – Front page',
    kind: 'rss',
    url: 'https://hnrss.org/frontpage',
    collection_id: 'col_1',
    keywords: [],
    exclude_keywords: [],
    every: '30m0s',
    every_seconds: 1800,
    preset_id: 'hn-front-page',
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
  it('parses Go durations and respects the plan minimum', () => {
    expect(parseDuration('30m')).toBe(1800)
    expect(parseDuration('1h0m0s')).toBe(3600)
    expect(parseDuration('soon')).toBeNaN()
    expect(defaultInterval('5m', 900).every).toBe('15m')
    expect(defaultInterval('6h', 900).every).toBe('6h')
  })

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
  it('adds a watcher from a preset form generated from its params', async () => {
    const api = mockApi({
      'GET /v1/watchers': { data: [] },
      'GET /v1/watcher-presets': { data: PRESETS },
      'GET /v1/collections': COLLECTIONS,
      'GET /v1/workspace/usage': usage(PLANS[0], { watchers: 1 }),
      'GET /v1/plans': { data: PLANS },
      'POST /v1/watchers/from-preset': (c: Call) => ({
        status: 201,
        body: watcher({
          id: 'wat_9',
          name: 'r/midjourney',
          preset_id: 'reddit-subreddit',
          collection_id: (c.body as { collection: string }).collection,
        }),
      }),
      'PATCH /v1/watchers/wat_9': (c: Call) => watcher({ id: 'wat_9', name: 'r/midjourney', ...(c.body as object) }),
    })
    const user = userEvent.setup()

    renderWithProviders(<WatchersPage />, { mode: 'cloud' })

    expect(await screen.findByText('No watchers yet')).toBeInTheDocument()
    expect(screen.getByTestId('inspiration-tip')).toHaveTextContent(
      'Watch Pinterest boards or subreddits for inspiration and review them in the Inbox',
    )
    expect(await screen.findByTestId('usage-chip-watchers')).toHaveTextContent('1 / 3 watchers')

    await user.click(screen.getAllByRole('button', { name: /add watcher/i })[0])
    const sheet = await screen.findByRole('dialog')
    const gallery = await within(sheet).findByTestId('preset-gallery')

    // Grouped by category in the product order.
    expect(
      within(gallery)
        .getAllByRole('heading', { level: 3 })
        .map((h) => h.textContent?.trim()),
    ).toEqual(['Community', 'Inspiration', 'Content', 'Monitoring', 'Advanced'])
    await user.click(within(gallery).getByTestId('preset-reddit-subreddit'))

    const form = await within(sheet).findByTestId('watcher-form')
    const sub = within(form).getByLabelText(/subreddit/i)

    expect(sub).toHaveAttribute('placeholder', 'midjourney')
    expect(sub).toHaveAttribute('aria-required', 'true')

    // Required param is validated before anything is sent.
    await user.click(within(form).getByRole('button', { name: /start watching/i }))
    expect(await within(form).findByText('Subreddit is required')).toBeInTheDocument()
    expect(api.find('POST', '/v1/watchers/from-preset')).toHaveLength(0)

    await user.type(sub, 'midjourney')
    await user.selectOptions(within(form).getByLabelText('Add new entries to'), 'col_2')
    // Free plan: checks faster than 15 minutes are disabled and point at the Indie plan.
    const every = within(form).getByLabelText('Check')

    expect(every).toHaveValue('1800')
    expect(within(every).getByRole('option', { name: /every 5 minutes — indie plan/i })).toBeDisabled()
    expect(within(every).getByRole('option', { name: /every minute — team plan/i })).toBeDisabled()
    expect(within(every).getByRole('option', { name: /every 15 minutes/i })).toBeEnabled()
    expect(within(form).getByTestId('interval-hint')).toHaveTextContent('Faster checks need the Indie plan')
    await user.selectOptions(every, '3600')
    await user.type(within(form).getByLabelText('Exclude keywords'), 'meme{Enter}')

    await user.click(within(form).getByRole('button', { name: /start watching/i }))
    await waitFor(() => expect(api.find('POST', '/v1/watchers/from-preset')).toHaveLength(1))
    expect(api.find('POST', '/v1/watchers/from-preset')[0].body).toEqual({
      preset_id: 'reddit-subreddit',
      params: { subreddit: 'midjourney' },
      collection: 'col_2',
      every: '1h',
    })
    await waitFor(() => expect(api.find('PATCH', '/v1/watchers/wat_9')).toHaveLength(1))
    expect(api.find('PATCH', '/v1/watchers/wat_9')[0].body).toEqual({ exclude_keywords: ['meme'] })
    expect(await screen.findByText('Watching r/midjourney')).toBeInTheDocument()
  })

  it('creates a custom feed watcher in one request', async () => {
    const api = mockApi({
      'GET /v1/watchers': { data: [] },
      'GET /v1/watcher-presets': { data: PRESETS },
      'GET /v1/collections': COLLECTIONS,
      'GET /v1/workspace/usage': usage(UNLOCKED),
      'POST /v1/watchers': (c: Call) => ({ status: 201, body: watcher({ ...(c.body as object), id: 'wat_2' }) }),
    })
    const user = userEvent.setup()

    renderWithProviders(<WatchersPage />, { mode: 'local' })
    await user.click((await screen.findAllByRole('button', { name: /add watcher/i }))[0])
    const sheet = await screen.findByRole('dialog')

    await user.click(await within(sheet).findByRole('button', { name: /custom feed \/ page/i }))
    await user.click(within(sheet).getByRole('radio', { name: /page changes/i }))
    await user.type(within(sheet).getByLabelText('Page URL'), 'not a url')
    await user.type(within(sheet).getByLabelText(/^name/i), 'Pricing page')
    await user.click(within(sheet).getByRole('button', { name: /start watching/i }))
    expect(await within(sheet).findByText('Enter an absolute http(s) URL')).toBeInTheDocument()
    await user.clear(within(sheet).getByLabelText('Page URL'))
    await user.type(within(sheet).getByLabelText('Page URL'), 'https://example.com/pricing')
    // Local install: every interval is available.
    expect(within(within(sheet).getByLabelText('Check')).getByRole('option', { name: 'Every minute' })).toBeEnabled()
    await user.type(within(sheet).getByLabelText('Keywords'), 'price, plan{Enter}')
    await user.click(within(sheet).getByRole('button', { name: /start watching/i }))
    await waitFor(() => expect(api.find('POST', '/v1/watchers')).toHaveLength(1))
    expect(api.find('POST', '/v1/watchers')[0].body).toEqual({
      name: 'Pricing page',
      kind: 'url_diff',
      url: 'https://example.com/pricing',
      collection: 'col_1',
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
            preset_id: '',
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
      'GET /v1/watcher-presets': { data: PRESETS },
      'GET /v1/collections': COLLECTIONS,
      'GET /v1/workspace/usage': usage(PLANS[0], { watchers: 3 }),
      'GET /v1/plans': { data: PLANS },
      'POST /v1/watchers/from-preset': {
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

    await user.click(await within(sheet).findByTestId('preset-hn-front-page'))
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

  it('shows the badge in gallery and list views and opens links with nofollow', () => {
    renderWithProviders(
      <>
        <GalleryView items={[external, item()]} onOpen={() => {}} />
        <ListView items={[external, item()]} onOpen={() => {}} />
        <ItemPreview item={external} />
      </>,
    )
    expect(screen.getAllByTestId('external-badge')).toHaveLength(2)
    const links = screen.getAllByRole('link').filter((a) => a.getAttribute('href') === 'https://news.example.com/post')

    expect(links.length).toBeGreaterThan(0)

    for (const a of links) {
      expect(a).toHaveAttribute('rel', 'noopener noreferrer nofollow')
      expect(a).toHaveAttribute('target', '_blank')
    }
  })
})
