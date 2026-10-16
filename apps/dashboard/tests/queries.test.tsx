import { QueryClientProvider } from '@tanstack/react-query'
import { act, renderHook, waitFor } from '@testing-library/react'
import type { ReactNode } from 'react'
import { describe, expect, it } from 'vitest'

import type { ItemQuery } from '@/lib/api'
import { actionsFor, invalidateAfterItemChange, qk, useItemActions, useItemsInfinite } from '@/lib/queries'

import { APPROVE, REGENERATE, collection, item } from './fixtures'
import { makeClient, mockApi } from './utils'

describe('item actions', () => {
  it('derives them from the collection and the status, like the API', () => {
    const col = collection({ actions: [REGENERATE] })

    expect(actionsFor(col, 'pending').map((a) => a.name)).toEqual(['approve', 'reject', 'regenerate'])
    expect(actionsFor(col, 'approved').map((a) => a.name)).toEqual([])
    expect(actionsFor(col, 'failed').map((a) => a.name)).toEqual(['regenerate'])
    const archive = {
      name: 'archive',
      label: 'Archive',
      event: 'item.archived',
      requires_feedback: false,
      set_status: 'archived' as const,
    }

    expect(actionsFor(collection({ actions: [archive] }), 'archived')).toEqual([])
  })

  it('needs no request when the collection is cached', async () => {
    const api = mockApi({ 'GET /v1/collections': { data: [collection({ actions: [REGENERATE] })] } })
    const client = makeClient()
    const wrapper = ({ children }: { children: ReactNode }) => (
      <QueryClientProvider client={client}>{children}</QueryClientProvider>
    )
    const { result, rerender } = renderHook(({ current }) => useItemActions(current), {
      wrapper,
      initialProps: { current: item() },
    })

    await waitFor(() => expect(result.current.data?.map((a) => a.name)).toEqual(['approve', 'reject', 'regenerate']))
    rerender({ current: item({ id: 'itm_2', status: 'approved' }) })
    expect(result.current.data).toEqual([])
    expect(api.calls.filter((c) => c.path.endsWith('/actions'))).toHaveLength(0)
  })

  it('asks the API for items outside the cached collections', async () => {
    const api = mockApi({
      'GET /v1/collections': { data: [] },
      'GET /v1/items/itm_1/actions': { data: [APPROVE] },
    })
    const client = makeClient()
    const wrapper = ({ children }: { children: ReactNode }) => (
      <QueryClientProvider client={client}>{children}</QueryClientProvider>
    )
    const { result } = renderHook(() => useItemActions(item()), { wrapper })

    await waitFor(() => expect(result.current.data).toEqual([APPROVE]))
    expect(api.find('GET', '/v1/items/itm_1/actions')).toHaveLength(1)
  })
})

describe('paged item lists', () => {
  it('come back with one page after the user left them', async () => {
    const api = mockApi({
      'GET /v1/items': (call: { query: URLSearchParams }) => {
        const cursor = call.query.get('cursor')

        return { data: [item({ id: `itm_${cursor || 'first'}` })], next_cursor: cursor ? '' : 'p2' }
      },
    })
    const client = makeClient()
    const wrapper = ({ children }: { children: ReactNode }) => (
      <QueryClientProvider client={client}>{children}</QueryClientProvider>
    )
    const all: ItemQuery = { collection_id: 'col_1' }
    const searched: ItemQuery = { collection_id: 'col_1', q: 'bul' }
    const { result, rerender } = renderHook(({ q }) => useItemsInfinite(q), { wrapper, initialProps: { q: all } })

    await waitFor(() => expect(result.current.data?.pages).toHaveLength(1))
    await act(() => result.current.fetchNextPage())
    await waitFor(() => expect(result.current.data?.pages).toHaveLength(2))

    rerender({ q: searched })
    await waitFor(() => expect(result.current.data?.pages).toHaveLength(1))
    await waitFor(() => expect(client.getQueryData<{ pages: unknown[] }>(qk.items(all))?.pages).toHaveLength(1))

    void client.invalidateQueries({ queryKey: qk.items(all), refetchType: 'none' })
    const before = api.calls.length

    rerender({ q: all })
    await waitFor(() => expect(result.current.isFetching).toBe(false))
    expect(api.calls.length - before).toBe(1) // not one request per page it once had
  })
})

describe('after an item changes', () => {
  it('drops item lists not on screen, so another inbox tab loads fresh', () => {
    mockApi({})
    const qc = makeClient()
    const rejected = qk.items({ status: 'rejected' })

    qc.setQueryData(rejected, { pages: [{ data: [] }], pageParams: [undefined] })
    invalidateAfterItemChange(qc, 'itm_1')
    expect(qc.getQueryData(rejected)).toBeUndefined()
  })
})
