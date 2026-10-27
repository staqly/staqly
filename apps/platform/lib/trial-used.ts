import { useSyncExternalStore } from 'react'

/**
 * The free trial reused what an earlier unpaid trial used, and the shell explains it (TrialUsedDialog):
 * `paused` (accounts: the trial waits for review, nothing charged) or `card` (the card: no free trial, charged).
 * A refused connect or publish (402 trial_already_used) opens `paused`; the billing state opens either.
 */
export const TRIAL_USED = 'trial_already_used'

export type TrialUsedKind = 'paused' | 'card'

let current: TrialUsedKind | null = null
const listeners = new Set<() => void>()

function emit() {
  for (const l of listeners) {
    l()
  }
}

/** Opens the explanation of a paused trial (from a refused connect or publish, or the OAuth callback). */
export function openTrialUsed() {
  current = 'paused'
  emit()
}

export function closeTrialUsed() {
  current = null
  emit()
}

function subscribe(l: () => void) {
  listeners.add(l)

  return () => {
    listeners.delete(l)
  }
}

const get = () => current
const getServer = () => null

/** What to explain now, null when nothing asked for it. */
export function useTrialUsed(): TrialUsedKind | null {
  return useSyncExternalStore(subscribe, get, getServer)
}
