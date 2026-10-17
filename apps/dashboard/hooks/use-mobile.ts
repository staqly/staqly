import * as React from 'react'

const MOBILE_BREAKPOINT = 768

const query = (breakpoint: number) => `(max-width: ${breakpoint - 1}px)`

/**
 * True below `breakpoint` px (phones by default). Pass 1024 for layouts that only fit on desktop, such as wide
 * tables that become cards on tablets too.
 */
export function useIsMobile(breakpoint = MOBILE_BREAKPOINT) {
  // The React Compiler keeps `subscribe` stable per breakpoint, so the store is not re-subscribed on every render.
  const subscribe = (onChange: () => void) => {
    const mql = window.matchMedia(query(breakpoint))

    mql.addEventListener('change', onChange)

    return () => mql.removeEventListener('change', onChange)
  }

  return React.useSyncExternalStore(
    subscribe,
    () => window.matchMedia(query(breakpoint)).matches,
    () => false,
  )
}
