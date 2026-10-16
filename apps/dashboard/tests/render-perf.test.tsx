import { act, fireEvent, screen, waitFor, within } from '@testing-library/react'
import { useSyncExternalStore } from 'react'
import { beforeEach, describe, expect, it, vi } from 'vitest'

import { ActivityFeed } from '@/components/activity/activity-feed'
import { CollectionPage } from '@/components/collections/collection-page'
import { Inbox } from '@/components/inbox/inbox'
import { AppShell } from '@/components/shell/app-shell'
import type { Item, StashEvent } from '@/lib/api'

import { APPROVE, REJECT, collection, item } from './fixtures'
import { nav } from './navigation'
import { UNLOCKED, usage } from './plans-fixtures'
import { renders } from './render-count'
import { findActiveRow, mockApi, queryRow, renderWithProviders } from './utils'

/**
 * Render counts (React DevTools' "did render" rule, see render-count.ts) for the hot paths, with the React
 * Compiler applied as in production (vitest.config.ts). The bounds are what an update should cost: only
 * the rows it concerns. A failure usually means a compiler bail-out or an unstable prop / query result.
 */

// next/navigation as a store, like the real one: components re-render when the URL they read changes, and
// nothing else does (the shared mock in navigation.ts only changes on an explicit rerender).
const listeners = new Set<() => void>()
const subscribe = (cb: () => void) => {
  listeners.add(cb)

  return () => listeners.delete(cb)
}

function go(pathname: string, search = '') {
  act(() => {
    nav.set(pathname, search)
    listeners.forEach((l) => l())
  })
}

vi.mock('next/navigation', async () => {
  const { navigationModule } = await import('./navigation')
  const router = navigationModule.useRouter()
  const storeRouter = {
    ...router,
    replace: (href: string) => {
      const [p, q] = href.split('?')

      nav.set(p, q ?? '')
      listeners.forEach((l) => l())
    },
  }

  return {
    ...navigationModule,
    useRouter: () => storeRouter,
    usePathname: () => useSyncExternalStore(subscribe, () => nav.pathname),
    useSearchParams: () => useSyncExternalStore(subscribe, () => nav.search),
  }
})

const N = 300

function items(n = N): Item[] {
  return Array.from({ length: n }, (_, i) =>
    item({ id: `itm_${i}`, title: `Item ${i}`, created_at: `2026-01-01T10:${String(i % 60).padStart(2, '0')}:00Z` }),
  )
}

/** Published items: each row shows a run badge and the collection's main action. */
function completed(): Item[] {
  return Array.from({ length: 100 }, (_, i) =>
    item({ id: `itm_${i}`, title: `Item ${i}`, status: 'completed', run_action: 'publish' }),
  )
}

function events(from: number, n: number): StashEvent[] {
  return Array.from({ length: n }, (_, k) => {
    const i = from + k

    return {
      id: `evt_${i}`,
      seq: 1000 - i,
      type: 'item.approved',
      collection_id: 'col_1',
      item_id: `itm_${i}`,
      actor: { type: 'api_key', id: 'key_1', name: 'Agent' },
      data: {
        item: { id: `itm_${i}`, title: `Item ${i}`, status: 'approved' },
        collection: { id: 'col_1', name: 'Blog' },
      },
      created_at: '2026-10-14T09:30:00Z',
    }
  })
}

async function settle() {
  await act(async () => {
    await new Promise((r) => setTimeout(r, 20))
  })
}

/**
 * Check the renders since the last reset: exact counts for the named components and an upper bound for the
 * total over all components (Radix internals included). Failures print every count.
 */
function expectRenders(exact: Record<string, number>, maxTotal: number) {
  const all = renders.all()
  const total = Object.values(all).reduce((a, b) => a + b, 0)
  const counts = Object.fromEntries(Object.keys(exact).map((name) => [name, renders.of(name)]))

  // `all` is only there so a failure prints every count.
  expect({ ...counts, withinBound: total <= maxTotal, total, all }).toMatchObject({ ...exact, withinBound: true })
}

describe('render counts', () => {
  beforeEach(() => nav.set('/inbox'))

  it('inbox', async () => {
    mockApi({
      // Fresh objects on every request, as JSON from the server would be.
      'GET /v1/items': () => ({ data: items() }),
      'GET /v1/collections': { data: [collection({ status_counts: { pending: N }, item_count: N })] },
      'GET /v1/events': { data: [] },
      'POST /v1/items/itm_0/actions/approve': item({ id: 'itm_0', title: 'Item 0', status: 'approved', version: 2 }),
    })
    const { client } = renderWithProviders(<Inbox />, { mode: 'local' })
    const row = await findActiveRow()

    await within(row).findByRole('button', { name: /^approve/i })
    await settle()

    renders.reset()
    fireEvent.keyDown(window, { key: 'j' })
    await settle()
    // Only the two rows whose active state flipped; filters untouched. Was 252 in total.
    expectRenders({ InboxRow: 2, SelectItem: 0, ToggleGroupItem: 0 }, 65)

    renders.reset()
    fireEvent.keyDown(window, { key: 'x' })
    await settle()
    // One row, the bulk bar (with its More menu and Delete, mounted once) and the select-all checkbox (now
    // indeterminate). Was 270.
    expectRenders({ InboxRow: 1, ItemPreview: 0, SelectItem: 0 }, 100)

    renders.reset()
    await act(() => client.refetchQueries({ queryKey: ['items'] }))
    await settle()
    // Same data from the server: nothing renders.
    expectRenders({}, 0)

    // Without a selection Shift+A approves the active item.
    fireEvent.keyDown(window, { key: 'Escape' })
    fireEvent.keyDown(window, { key: 'k' })
    await settle()
    renders.reset()
    fireEvent.keyDown(window, { key: 'A', shiftKey: true })
    await waitFor(() => expect(queryRow(/item 0$/i)).not.toBeInTheDocument())
    await settle()
    // The approved row leaves, the next one becomes active; the preview shows it. Was 266.
    expectRenders({ InboxRow: 1, SelectItem: 0 }, 75)
  }, 30_000) // Thousands of renders; slow when the whole repo is tested in parallel.

  it('inbox, a decided step', async () => {
    nav.set('/inbox', 'step=done')
    const PUBLISH = {
      name: 'publish',
      label: 'Publish',
      event: 'item.publish_requested',
      requires_feedback: false,
      set_status: 'processing' as const,
      allowed_statuses: ['approved' as const, 'completed' as const],
      run_labels: { running: 'Publishing', done: 'Published', failed: 'Publish failed' },
    }
    let pending = 1

    mockApi({
      'GET /v1/items': () => ({ data: completed() }),
      // Pending counts change with every decision anywhere; the rows don't show them.
      'GET /v1/collections': () => ({ data: [collection({ actions: [PUBLISH], status_counts: { pending } })] }),
      'GET /v1/events': { data: [] },
    })
    const { client } = renderWithProviders(<Inbox />, { mode: 'local' })

    expect(await within(await findActiveRow()).findByRole('button', { name: /publish/i })).toBeInTheDocument()
    await settle()

    renders.reset()
    fireEvent.keyDown(window, { key: 'j' })
    await settle()
    expectRenders({ InboxRow: 2, RunBadge: 0 }, 65)

    pending = 2
    renders.reset()
    await act(() => client.refetchQueries({ queryKey: ['collections'] }))
    await settle()
    // New counts, same actions: no row and no status badge renders.
    expectRenders({ InboxRow: 0, RunBadge: 0 }, 20)
  })

  it('collection table', async () => {
    nav.set('/collections/col_1')
    mockApi({
      'GET /v1/collections/col_1': collection({ default_view: 'table', item_count: N }),
      'GET /v1/collections': { data: [collection()] },
      'GET /v1/items': () => ({ data: items() }),
      'GET /v1/events': { data: [] },
      'GET /v1/items/*': (c: { path: string }) =>
        c.path.endsWith('/actions')
          ? { data: [APPROVE, REJECT] }
          : c.path.endsWith('/events') || c.path.endsWith('/versions')
            ? { data: [] }
            : item({ id: 'itm_5', title: 'Item 5' }),
    })
    renderWithProviders(<CollectionPage id="col_1" />, { mode: 'local' })
    expect(await screen.findByTestId('table-view')).toBeInTheDocument()
    await settle()

    renders.reset()
    go('/collections/col_1', 'item=itm_5')
    await screen.findByRole('dialog')
    await settle()
    // The sheet and the item mount; the table, its 300 rows and the filters don't render. Was 589.
    expectRenders({ TableView: 0, TableItemRow: 0, SelectItem: 0 }, 380)

    renders.reset()
    go('/collections/col_1', '')
    await settle()
    // Only the closing sheet. Was 343.
    expectRenders({ TableView: 0, TableItemRow: 0, SelectItem: 0 }, 80)
  })

  it('activity feed', async () => {
    nav.set('/activity')
    let fresh: StashEvent[] = []

    mockApi({
      'GET /v1/events': (c: { query: URLSearchParams }) =>
        c.query.get('cursor') ? { data: events(50, 50) } : { data: [...fresh, ...events(0, 50)], next_cursor: 'c1' },
      'GET /v1/collections': { data: [collection()] },
    })
    renderWithProviders(<ActivityFeed />, { mode: 'local' })
    const rows = await screen.findAllByTestId('audit-event')

    await settle()
    renders.reset()
    fireEvent.click(within(rows[3]).getByRole('button', { name: 'Data' }))
    await settle()
    // The data card keeps its own open state: only it renders, not the row.
    expectRenders({ AuditEventRow: 0, DisclosureCard: 1 }, 15)

    renders.reset()
    fireEvent.click(screen.getByRole('button', { name: /load older events/i }))
    await waitFor(() => expect(screen.getAllByTestId('audit-event')).toHaveLength(100))
    await settle()
    // Only the 50 new rows (the line under the previously last one is CSS). Was 51.
    expectRenders({ AuditEventRow: 50 }, 1500) // each row has an entity, a collection link and a data card

    fresh = events(-1, 1)
    renders.reset()
    fireEvent.click(screen.getByRole('button', { name: /refresh/i }))
    await waitFor(() => expect(screen.getAllByTestId('audit-event')).toHaveLength(101))
    await settle()
    // Events are shared by id: only the new row renders. Was 51 rows (192 in total).
    expectRenders({ AuditEventRow: 1 }, 60)
  })

  it('app shell', async () => {
    nav.set('/inbox')
    mockApi({
      'GET /v1/collections': {
        data: Array.from({ length: 20 }, (_, i) => collection({ id: `col_${i}`, name: `Collection ${i}` })),
      },
      'GET /v1/workspace/usage': usage(UNLOCKED),
    })
    renderWithProviders(
      <AppShell>
        <div>page</div>
      </AppShell>,
      { mode: 'local' },
    )
    expect(await screen.findByText('Collection 19')).toBeInTheDocument()
    await settle()

    renders.reset()
    go('/activity')
    await settle()
    // The sidebar's own links; none of the 20 collection rows. Was 407 in total.
    expectRenders({ AppSidebar: 1, NavCollectionItem: 0 }, 90)

    renders.reset()
    go('/collections/col_3')
    await settle()
    expectRenders({ NavCollectionItem: 1 }, 90)

    renders.reset()
    go('/collections/col_3', 'item=itm_1')
    await settle()
    // A query string change (e.g. opening an item sheet) doesn't touch the shell.
    expectRenders({}, 0)
  })
})
