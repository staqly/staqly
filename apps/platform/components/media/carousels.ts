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

// "Launch (2 of 4).png": the names the design editor saves a carousel's pages under.
const PAGE_NAME = /^(.+) \((\d+) of (\d+)\)\.[a-z0-9]+$/i

/**
 * The list as the library shows it: a carousel's later pages left out where its first page is in the list too,
 * and every carousel's pages by its first page's id. Carousels are known from their designs (page_ids), and,
 * for pages saved before designs kept them, by their names when every page is in the list.
 */
export function stackCarousels<T extends MediaLibraryItem>(items: T[], c: ReturnType<typeof useCarousels>) {
  const pages = new Map(c.pages)
  const byName = new Map<string, Map<number, string>>()

  for (const m of items) {
    const hit = PAGE_NAME.exec(m.original_name)

    if (hit && !c.rest.has(m.id) && !pages.has(m.id)) {
      const key = `${hit[1]}|${hit[3]}`
      const group = byName.get(key) ?? new Map<number, string>()

      group.set(Number(hit[2]), m.id)
      byName.set(key, group)
    }
  }

  for (const [key, group] of byName) {
    const n = Number(key.split('|').pop())

    if (n > 1 && group.size === n) {
      const ids = Array.from({ length: n }, (_, i) => group.get(i + 1)!)

      if (ids.every(Boolean)) {
        pages.set(ids[0]!, ids)
      }
    }
  }

  const shown = new Set(items.map((m) => m.id))
  const firstOf = new Map<string, string>()

  for (const [first, ids] of pages) {
    ids.slice(1).forEach((id) => firstOf.set(id, first))
  }

  return {
    items: items.filter((m) => !(firstOf.has(m.id) && shown.has(firstOf.get(m.id)!))),
    pages,
  }
}
