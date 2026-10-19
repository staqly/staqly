'use client'

import { useSyncExternalStore } from 'react'

/**
 * The most recently created API key, kept only in memory for this browser tab
 * so the integration snippets can be pre-filled. Never persisted, and never
 * holds the recovery key. Cleared on sign-out, sign-in, setup, device redeem
 * and when that key is revoked or rotated.
 */
export interface RememberedKey {
  id?: string
  name: string
  key: string
}

let current: RememberedKey | null = null
const listeners = new Set<() => void>()

export function rememberApiKey(value: RememberedKey | null) {
  current = value
  listeners.forEach((l) => l())
}

/** Forgets the remembered key if it is `id` (or if its id is unknown). */
export function forgetApiKey(id: string) {
  if (current && (!current.id || current.id === id)) {
    rememberApiKey(null)
  }
}

/** Current value, for non-React code and tests. */
export function rememberedApiKey(): RememberedKey | null {
  return current
}

function subscribe(l: () => void) {
  listeners.add(l)

  return () => listeners.delete(l)
}

export function useRememberedApiKey(): RememberedKey | null {
  return useSyncExternalStore(
    subscribe,
    () => current,
    () => null,
  )
}
