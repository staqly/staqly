'use client'

import { useQueryClient } from '@tanstack/react-query'
import { useEffect, useRef } from 'react'

import { qk, refreshEventLists, useRecentEvents } from '@/lib/queries'

/** How often the dashboard looks for changes made elsewhere (agents, the scheduler, other tabs); paused in background tabs. */
export const LIVE_INTERVAL_MS = 3000

/**
 * Keeps every open view current without a reload: one small request for the newest events, and when one
 * appears that this tab hasn't seen, the views it touches refetch (counts, activity, the posts it names, the
 * channels and schedules, webhook deliveries). Paged post lists refetch themselves from the same request.
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
    // The activity feed: only its newest events, not every page it has loaded.
    void refreshEventLists(qc)
    void qc.invalidateQueries({ queryKey: ['deliveries'] })

    if (fresh.some((e) => e.type.startsWith('channel.'))) {
      void qc.invalidateQueries({ queryKey: qk.channels })
    }

    if (fresh.some((e) => e.type.startsWith('schedule.'))) {
      void qc.invalidateQueries({ queryKey: qk.schedulesAll })
    }

    for (const id of new Set(fresh.map((e) => e.post_id).filter(Boolean))) {
      void qc.invalidateQueries({ queryKey: ['post', id] })
    }
  }, [events.data, qc])

  return null
}
