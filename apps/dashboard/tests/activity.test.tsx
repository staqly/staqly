import { screen, waitFor, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { beforeEach, describe, expect, it } from 'vitest'

import { ActivityFeed, localDayStart, nextDay } from '@/components/activity/activity-feed'
import type { StashEvent } from '@/lib/api'

import { collection } from './fixtures'
import { nav } from './navigation'
import { mockApi, renderWithProviders } from './utils'

const ITEM_EVENT: StashEvent = {
  id: 'evt_item1',
  seq: 2,
  type: 'item.approved',
  collection_id: 'col_1',
  item_id: 'itm_abc123',
  actor: { type: 'api_key', id: 'key_agent1', name: 'Writer agent' },
  data: {
    item: { id: 'itm_abc123', title: 'Launch post', status: 'approved' },
    collection: { id: 'col_1', name: 'Blog' },
  },
  created_at: '2026-10-14T09:30:00Z',
}

const WEBHOOK_EVENT: StashEvent = {
  id: 'evt_wh1',
  seq: 1,
  type: 'webhook.updated',
  collection_id: null,
  item_id: null,
  actor: { type: 'session', id: 'ses_dash1', name: 'Dashboard' },
  data: { webhook: { id: 'wh_hook42', url: 'https://example.com/hook', secret: '••••abcd', active: true } },
  created_at: '2026-10-14T09:00:00Z',
}

function setup(events: StashEvent[] = [ITEM_EVENT, WEBHOOK_EVENT]) {
  return mockApi({ 'GET /v1/events': { data: events }, 'GET /v1/collections': { data: [collection()] } })
}

describe('activity audit log', () => {
  beforeEach(() => nav.set('/activity'))

  it('shows actor, entity and time for an item event and a webhook event', async () => {
    setup()
    renderWithProviders(<ActivityFeed />, { mode: 'cloud' })
    const rows = await screen.findAllByTestId('audit-event')

    expect(rows).toHaveLength(2)

    const itemActor = within(rows[0]).getByTestId('event-actor')

    expect(itemActor).toHaveTextContent('API key')
    expect(itemActor).toHaveTextContent('Writer agent')
    expect(itemActor).toHaveTextContent('key_agent1')
    const itemEntity = within(rows[0]).getByTestId('event-entity')

    expect(itemEntity).toHaveTextContent('Item')
    expect(within(itemEntity).getByRole('link', { name: 'Launch post' })).toHaveAttribute('href', '/items/itm_abc123')
    expect(itemEntity).toHaveTextContent('itm_abc123')
    expect(itemEntity).toHaveTextContent('in Blog')
    expect(within(rows[0]).getByTestId('event-time')).toHaveAttribute('dateTime', ITEM_EVENT.created_at)

    expect(within(rows[1]).getByText('Webhook updated')).toBeInTheDocument()
    const hookActor = within(rows[1]).getByTestId('event-actor')

    expect(hookActor).toHaveTextContent('Dashboard')
    expect(hookActor).toHaveTextContent('ses_dash1')
    const hookEntity = within(rows[1]).getByTestId('event-entity')

    expect(hookEntity).toHaveTextContent('Webhook')
    expect(hookEntity).toHaveTextContent('https://example.com/hook')
    expect(hookEntity).toHaveTextContent('wh_hook42')
    expect(within(hookEntity).getByRole('button', { name: 'Copy Webhook ID' })).toBeInTheDocument()
  })

  it('shows the object id from data for non-item events without an embedded object', async () => {
    setup([
      {
        ...WEBHOOK_EVENT,
        id: 'evt_w2',
        type: 'watcher.deleted',
        actor: { type: 'system', id: 'stripe', name: 'Stripe' },
        data: { watcher_id: 'wat_9' },
      },
    ])
    renderWithProviders(<ActivityFeed />, { mode: 'cloud' })
    const row = await screen.findByTestId('audit-event')

    expect(within(row).getByTestId('event-entity')).toHaveTextContent(/Watcher.*wat_9/)
    expect(within(row).getByTestId('event-actor')).toHaveTextContent(/System.*Stripe/)
  })

  it('expands Data to the pretty-printed event JSON', async () => {
    setup()
    const user = userEvent.setup()

    renderWithProviders(<ActivityFeed />, { mode: 'cloud' })
    const rows = await screen.findAllByTestId('audit-event')
    const toggle = within(rows[1]).getByRole('button', { name: 'Data' })

    expect(toggle).toHaveAttribute('aria-expanded', 'false')
    expect(within(rows[1]).queryByTestId('event-data')).not.toBeInTheDocument()
    await user.click(toggle)
    expect(toggle).toHaveAttribute('aria-expanded', 'true')
    const panel = within(rows[1]).getByTestId('event-data')

    expect(panel.querySelector('pre')!.textContent).toBe(JSON.stringify(WEBHOOK_EVENT.data, null, 2))
    // Copy appears in the card header once it is open.
    expect(within(rows[1]).getByRole('button', { name: 'Copy data' })).toBeInTheDocument()
    await user.click(toggle)
    expect(within(rows[1]).queryByTestId('event-data')).not.toBeInTheDocument()
    expect(within(rows[1]).queryByRole('button', { name: 'Copy data' })).not.toBeInTheDocument()
  })

  it('sends actor_id, item_id, from and the day after To from the URL filters', async () => {
    nav.set('/activity', 'actor_id=key_agent1&item=itm_abc123&from=2026-10-01&to=2026-10-31')
    const api = setup()

    renderWithProviders(<ActivityFeed />, { mode: 'cloud' })
    await screen.findAllByTestId('audit-event')
    const q = Object.fromEntries(api.find('GET', '/v1/events').at(-1)!.query)

    expect(q).toEqual({
      actor_id: 'key_agent1',
      item_id: 'itm_abc123',
      from: localDayStart('2026-10-01'),
      to: localDayStart('2026-11-01'),
      limit: '50',
    })
    const chips = screen.getByTestId('active-filters')

    expect(chips).toHaveTextContent('Actor ID:key_agent1')
    expect(chips).toHaveTextContent('Entity ID:itm_abc123')
    expect(chips).toHaveTextContent('Dates:2026-10-01 → 2026-10-31')
    expect(screen.getByLabelText('From date')).toHaveValue('2026-10-01')
    expect(screen.getByLabelText('To date (inclusive)')).toHaveValue('2026-10-31')
  })

  it('adds filters from "Add filter", shows them as chips and removes them', async () => {
    nav.set('/activity', 'from=2026-10-01')
    setup()
    const user = userEvent.setup()

    renderWithProviders(<ActivityFeed />, { mode: 'cloud' })
    await screen.findAllByTestId('audit-event')
    await user.type(screen.getByLabelText('Event type'), 'item.approved{Enter}')
    expect(nav.replace).toHaveBeenLastCalledWith('/activity?from=2026-10-01&type=item.approved', { scroll: false })

    await user.click(screen.getByRole('button', { name: 'Add filter' }))
    await user.click(screen.getByRole('combobox', { name: 'Filter field' }))
    await user.click(await screen.findByRole('option', { name: 'Actor ID' }))
    await user.type(screen.getByLabelText('Filter value'), 'key_x{Enter}')
    // The fake router doesn't re-render, so each call starts from the initial URL.
    expect(nav.replace).toHaveBeenLastCalledWith('/activity?from=2026-10-01&actor_id=key_x', { scroll: false })

    await user.click(screen.getByRole('button', { name: 'Remove Dates filter' }))
    expect(nav.replace).toHaveBeenLastCalledWith('/activity', { scroll: false })
    await user.click(screen.getByRole('button', { name: 'Clear filters' }))
    expect(nav.replace).toHaveBeenLastCalledWith('/activity', { scroll: false })
  })

  it('hides Clear filters without filters', async () => {
    setup()
    renderWithProviders(<ActivityFeed />, { mode: 'cloud' })
    await screen.findAllByTestId('audit-event')
    expect(screen.queryByRole('button', { name: 'Clear filters' })).not.toBeInTheDocument()
  })

  it('exports with every current filter', async () => {
    nav.set(
      '/activity',
      'type=item.approved&actor_type=api_key&actor_id=key_agent1&collection=col_1&item=itm_abc123&from=2026-10-01&to=2026-10-31',
    )
    setup()
    const user = userEvent.setup()

    renderWithProviders(<ActivityFeed />, { mode: 'cloud' })
    await user.click(await screen.findByRole('button', { name: /export/i }))
    const menu = await screen.findByRole('menu')

    for (const format of ['csv', 'ndjson']) {
      const href = within(menu)
        .getByRole('menuitem', { name: new RegExp(format, 'i') })
        .getAttribute('href')!
      const url = new URL(href, 'http://x')

      expect(url.pathname).toBe('/api/v1/events/export')
      expect(Object.fromEntries(url.searchParams)).toEqual({
        format,
        type: 'item.approved',
        actor_type: 'api_key',
        actor_id: 'key_agent1',
        collection_id: 'col_1',
        item_id: 'itm_abc123',
        from: localDayStart('2026-10-01'),
        to: localDayStart('2026-11-01'),
      })
    }
  })

  it('turns a picked day into its local midnight', () => {
    expect(localDayStart('2026-10-01')).toBe(new Date(2026, 9, 1).toISOString())
  })

  it('computes the exclusive day after across month and year ends', () => {
    expect(nextDay('2026-10-31')).toBe('2026-11-01')
    expect(nextDay('2026-12-31')).toBe('2027-01-01')
    expect(nextDay('2028-02-28')).toBe('2028-02-29')
  })

  it('ignores malformed dates in the URL', async () => {
    nav.set('/activity', 'from=yesterday&to=2026-13-45')
    const api = setup()

    renderWithProviders(<ActivityFeed />, { mode: 'cloud' })
    await waitFor(() => expect(api.find('GET', '/v1/events').length).toBeGreaterThan(0))
    expect(Object.fromEntries(api.find('GET', '/v1/events')[0].query)).toEqual({ limit: '50' })
  })
})
