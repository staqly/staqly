import * as React from 'react'

const MOBILE_BREAKPOINT = 768

/**
 * One MediaQueryList per query, made on first use and shared: `matchMedia` builds a new one per call, and these
 * hooks run in every calendar cell.
 */
const lists = new Map<string, MediaQueryList>()
let source: typeof window.matchMedia | undefined

function mediaQuery(query: string) {
  // A replaced matchMedia (tests stub it) starts a new cache.
  if (source !== window.matchMedia) {
    source = window.matchMedia
    lists.clear()
  }

  let mql = lists.get(query)

  if (!mql) {
    mql = window.matchMedia(query)
    lists.set(query, mql)
  }

  return mql
}

const query = (breakpoint: number) => `(max-width: ${breakpoint - 1}px)`

/**
 * True below `breakpoint` px (phones by default). Pass 1024 for layouts that only fit on desktop, such as wide
 * tables that become cards on tablets too.
 */
export function useIsMobile(breakpoint = MOBILE_BREAKPOINT) {
  // The React Compiler keeps `subscribe` stable per breakpoint, so the store is not re-subscribed on every render.
  const subscribe = (onChange: () => void) => {
    const mql = mediaQuery(query(breakpoint))

    mql.addEventListener('change', onChange)

    return () => mql.removeEventListener('change', onChange)
  }

  return React.useSyncExternalStore(
    subscribe,
    () => mediaQuery(query(breakpoint)).matches,
    () => false,
  )
}

const COARSE = '(pointer: coarse)'

function subscribeCoarse(onChange: () => void) {
  const mql = mediaQuery(COARSE)

  mql.addEventListener('change', onChange)

  return () => mql.removeEventListener('change', onChange)
}

/** True on touch screens (a coarse pointer): controls there need 40 px targets. */
export function useIsCoarsePointer() {
  return React.useSyncExternalStore(
    subscribeCoarse,
    () => mediaQuery(COARSE).matches,
    () => false,
  )
}
