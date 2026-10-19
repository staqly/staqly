'use client'

import { useQueryClient } from '@tanstack/react-query'
import { useEffect, useRef, useState } from 'react'

import { isForeignEvent, qk, refreshEventLists, useRecentEvents } from '@/lib/queries'

/** How often the dashboard looks for changes made elsewhere (agents, the scheduler, other tabs); paused in background tabs. */
export const LIVE_INTERVAL_MS = 3000

/**
 * Post events that change a collection's counts (posts per status). Edits of the text, feedback, replies and
 * shared previews leave them as they are.
 */
const COUNT_EVENTS = new Set([
  'post.created',
  'post.updated',
  'post.deleted',
  'post.purged',
  'post.scheduled',
  'post.unscheduled',
  'post.publish_requested',
  'post.published',
  'post.failed',
])

/**
 * True once the page has loaded and the browser is idle. The first poll waits for it: on a slow cold load (a
 * phone) the 3 s poll would otherwise repeat the events request the page made a moment ago, while it still loads.
 */
function useLoaded() {
  const [loaded, setLoaded] = useState(false)

  useEffect(() => {
    let idle: number | undefined
    const ready = () => {
      if (typeof window.requestIdleCallback === 'function') {
        idle = window.requestIdleCallback(() => setLoaded(true), { timeout: 5000 })
      } else {
        setLoaded(true)
      }
    }

    if (document.readyState === 'complete') {
      ready()
    } else {
      window.addEventListener('load', ready, { once: true })
    }

    return () => {
      window.removeEventListener('load', ready)

      if (idle !== undefined) {
        window.cancelIdleCallback(idle)
      }
    }
  }, [])

  return loaded
}

/**
 * Keeps every open view current without a reload: one small request for the newest events, and when one
 * appears that this tab hasn't seen, the views it touches refetch (counts, activity, the posts it names, the
 * channels and schedules, webhook deliveries). Paged post lists refetch themselves from the same request.
 * Changes this tab made itself are already in the cache, so their events refetch nothing but the activity.
 */
export function LiveUpdates() {
  const qc = useQueryClient()
  const loaded = useLoaded()
  const events = useRecentEvents({ refetchInterval: loaded ? LIVE_INTERVAL_MS : undefined })
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
    const others = fresh.filter((e) => isForeignEvent(e))

    if (others.some((e) => e.type.startsWith('collection.') || COUNT_EVENTS.has(e.type))) {
      void qc.invalidateQueries({ queryKey: qk.collectionsAll })
      void qc.invalidateQueries({ queryKey: ['collection'] })
    }

    // The activity feed: only its newest events, not every page it has loaded.
    void refreshEventLists(qc)
    // Any event may have been delivered to a webhook, this tab's own too. Only a deliveries list on screen
    // refetches; the others are just marked stale.
    void qc.invalidateQueries({ queryKey: ['deliveries'] })

    if (others.some((e) => e.type.startsWith('channel.'))) {
      void qc.invalidateQueries({ queryKey: qk.channels })
    }

    if (others.some((e) => e.type.startsWith('schedule.'))) {
      void qc.invalidateQueries({ queryKey: qk.schedulesAll })
    }

    for (const id of new Set(others.map((e) => e.post_id).filter(Boolean))) {
      void qc.invalidateQueries({ queryKey: ['post', id] })
    }
  }, [events.data, qc])

  return null
}
