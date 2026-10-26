'use client'

import { useQuery } from '@tanstack/react-query'
import { useMemo } from 'react'

import { api, type MediaLibraryItem } from '@/lib/api'

const NONE = { pages: new Map<string, string[]>(), rest: new Set<string>() }

/**
 * The design editor's carousels: a design saved with several pages is one file per page. The library shows them
 * as one tile (the first page, "4 pages"), and picking it for a post takes every page in order.
 */
export function useCarousels() {
  const projects = useQuery({
    queryKey: ['media-projects'],
    queryFn: () => api.mediaProjects(),
    staleTime: 30_000,
  })
  const data = projects.data?.data

  return useMemo(() => {
    if (!data) {
      return NONE
    }

    const pages = new Map<string, string[]>()
    const rest = new Set<string>()

    for (const p of data) {
      if (p.page_ids.length > 1 && !pages.has(p.page_ids[0]!)) {
        pages.set(p.page_ids[0]!, p.page_ids)
        p.page_ids.slice(1).forEach((id) => rest.add(id))
      }
    }

    return { pages, rest }
  }, [data])
}

/** The list with a carousel's later pages left out, where its first page is in the list too. */
export function stackCarousels<T extends MediaLibraryItem>(items: T[], c: ReturnType<typeof useCarousels>) {
  if (!c.rest.size) {
    return items
  }

  const shown = new Set(items.map((m) => m.id))
  const firstOf = new Map<string, string>()

  for (const [first, ids] of c.pages) {
    ids.slice(1).forEach((id) => firstOf.set(id, first))
  }

  return items.filter((m) => !(firstOf.has(m.id) && shown.has(firstOf.get(m.id)!)))
}
