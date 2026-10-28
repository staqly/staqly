'use client'

import { useSyncExternalStore } from 'react'

import type { PostStatus } from '@/lib/api'

/**
 * "Show past posts": one switch for the post lists, remembered in this browser (the calendar always shows them).
 * Past posts (the server's `hide_past`) went out: published; failed ones stay in view. Off by default; storage
 * that is blocked or throws (private windows, previews) keeps the choice in memory for this page instead.
 */
const KEY = 'staqly.posts.showPast'

const listeners = new Set<() => void>()
let fallback = false

function read(): boolean {
  try {
    return localStorage.getItem(KEY) === '1'
  } catch {
    return fallback
  }
}

function write(on: boolean) {
  try {
    if (on) {
      localStorage.setItem(KEY, '1')
    } else {
      localStorage.removeItem(KEY)
    }
  } catch {
    // Storage blocked: the in-memory value below still works until the page reloads.
  }
}

function subscribe(onChange: () => void) {
  listeners.add(onChange)
  // Another tab flipped it.
  const onStorage = (e: StorageEvent) => {
    if (e.key === KEY) {
      onChange()
    }
  }

  window.addEventListener('storage', onStorage)

  return () => {
    listeners.delete(onChange)
    window.removeEventListener('storage', onStorage)
  }
}

export function setShowPast(on: boolean) {
  fallback = on
  write(on)

  for (const l of listeners) {
    l()
  }
}

export function useShowPast(): boolean {
  return useSyncExternalStore(subscribe, read, () => false)
}

/**
 * The `hide_past` value for a post list: on unless the switch shows them or the status filter asks for posts that
 * are past by nature (published, failed), which would otherwise always come back empty.
 */
export function hidePastParam(showPast: boolean, status?: PostStatus | ''): true | undefined {
  return showPast || status === 'published' || status === 'failed' ? undefined : true
}
