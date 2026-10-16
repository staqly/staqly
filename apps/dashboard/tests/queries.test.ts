import { QueryClient, type InfiniteData } from '@tanstack/react-query'
import { describe, expect, it } from 'vitest'

import type { ItemPage } from '@/lib/api'
import { qk, refetchItemListsFromStart, shareItemPages } from '@/lib/queries'

import { item } from './fixtures'

const a = item({ id: 'itm_a', title: 'A' })
const b = item({ id: 'itm_b', title: 'B' })
const c = item({ id: 'itm_c', title: 'C' })

function data(...pages: ItemPage[]): InfiniteData<ItemPage> {
  return { pages, pageParams: pages.map((_, i) => (i ? `cur${i}` : '')) }
}

describe('shareItemPages', () => {
  it('returns the previous data when nothing changed', () => {
    const prev = data({ data: [a, b], next_cursor: 'cur1' }, { data: [c] })
    const next = structuredClone(prev)

    expect(shareItemPages(prev, next)).toBe(prev)
  })

  it('keeps item identity when items shift to another page or index', () => {
    const prev = data({ data: [a, b], next_cursor: 'cur1' }, { data: [c] })
    // `a` was approved elsewhere: the refetch moves b and c up.
    const next = structuredClone(data({ data: [b, c] }))
    const out = shareItemPages(prev, next) as InfiniteData<ItemPage>

    expect(out).not.toBe(prev)
    expect(out.pages[0].data[0]).toBe(b)
    expect(out.pages[0].data[1]).toBe(c)
  })

  it('replaces only items whose content changed', () => {
    const prev = data({ data: [a, b] })
    const next = structuredClone(prev)

    next.pages[0].data[1] = { ...next.pages[0].data[1], title: 'B2' }

    const out = shareItemPages(prev, next) as InfiniteData<ItemPage>

    expect(out.pages[0].data[0]).toBe(a)
    expect(out.pages[0].data[1]).not.toBe(b)
    expect(out.pages[0].data[1].title).toBe('B2')
  })

  it('handles a newly appended page', () => {
    const prev = data({ data: [a], next_cursor: 'cur1' })
    const next = data({ data: [a], next_cursor: 'cur1' }, { data: [b] })
    const out = shareItemPages(prev, next) as InfiniteData<ItemPage>

    expect(out.pages[0]).toBe(prev.pages[0])
    expect(out.pages[1].data[0]).toBe(b)
  })
})

describe('refetchItemListsFromStart', () => {
  it('keeps only the first page of every item list, so the refetch is one request per list', async () => {
    const qc = new QueryClient()
    const key = qk.items({ collection_id: 'col_1', limit: 48 })

    qc.setQueryData(key, data({ data: [a], next_cursor: 'cur1' }, { data: [b], next_cursor: 'cur2' }, { data: [c] }))
    refetchItemListsFromStart(qc, 'itm_new')

    const out = qc.getQueryData<InfiniteData<ItemPage>>(key)!

    expect(out.pages).toHaveLength(1)
    expect(out.pageParams).toEqual([''])
    expect(qc.getQueryState(key)?.isInvalidated).toBe(true)
  })
})
