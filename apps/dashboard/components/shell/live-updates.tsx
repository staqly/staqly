'use client'

import { useQueryClient } from '@tanstack/react-query'
import { useEffect, useRef } from 'react'

import { qk, useRecentEvents } from '@/lib/queries'

/** How often the dashboard looks for changes made elsewhere (agents, curl, other tabs); paused in background tabs. */
export const LIVE_INTERVAL_MS = 3000

/**
 * Keeps every open view current without a reload: one small request for the newest events, and when one
 * appears that this tab hasn't seen, the views it touches refetch (counts, activity, the items it names,
 * webhook deliveries). Paged item lists (inbox, collection) refetch themselves from the same request.
 */
export function LiveUpdates() {
  const qc = useQueryClient()
  const events = useRecentEvents({ refetchInterval: LIVE_INTERVAL_MS })
  const seen = useRef<number | undefined>(undefined)

  useEffect(() => {
    const list = events.data

    if (!list) {
      return
    }

    const previous = seen.current

    seen.current = list[0]?.seq ?? 0

    if (previous === undefined || seen.current === previous) {
      return
    }

    const fresh = list.filter((e) => e.seq > previous)

    void qc.invalidateQueries({ queryKey: qk.collectionsAll })
    void qc.invalidateQueries({ queryKey: ['collection'] })
    void qc.invalidateQueries({ queryKey: qk.eventsAll })
    void qc.invalidateQueries({ queryKey: ['deliveries'] })
    // The inbox step counts.
    void qc.invalidateQueries({ queryKey: ['flow'] })

    for (const id of new Set(fresh.map((e) => e.item_id).filter(Boolean))) {
      void qc.invalidateQueries({ queryKey: ['item', id] })
    }
  }, [events.data, qc])

  return null
}
