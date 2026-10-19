import { useSyncExternalStore } from 'react'

// setTimeout holds a signed 32-bit delay; a longer one fires at once.
const MAX_DELAY = 2_147_483_647

/**
 * Whether `date` has passed. One timer until the date flips it, instead of a clock ticking every second
 * (re-armed when the date changes).
 */
export function useIsPast(date: Date | null): boolean {
  const at = date ? date.getTime() : null
  // The React Compiler keeps `subscribe` stable per date, so the timer is re-armed only when the date changes.
  const subscribe = (onChange: () => void) => {
    if (at === null || Number.isNaN(at)) {
      return () => {}
    }

    let t: ReturnType<typeof setTimeout> | undefined
    const arm = () => {
      const left = at - Date.now()

      if (left <= 0) {
        onChange()

        return
      }

      t = setTimeout(arm, Math.min(left, MAX_DELAY))
    }

    arm()

    return () => clearTimeout(t)
  }

  return useSyncExternalStore(
    subscribe,
    () => at !== null && at <= Date.now(),
    () => false,
  )
}
