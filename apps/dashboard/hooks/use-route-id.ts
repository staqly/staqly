'use client'

import { usePathname } from 'next/navigation'
import { useSyncExternalStore } from 'react'

const subscribe = () => () => {}

/**
 * The [id] segment of the current URL. The static export renders each [id]
 * page once (as id "_") and the Go web server serves that file for every id,
 * so the real id only exists in the browser: null while prerendering and
 * hydrating, which keeps hydration consistent with the prerendered HTML.
 */
export function useRouteId(): string | null {
  const pathname = usePathname()
  const hydrated = useSyncExternalStore(
    subscribe,
    () => true,
    () => false,
  )

  if (!hydrated) {
    return null
  }

  return decodeSegment(pathname.split('/').findLast(Boolean) ?? '')
}

/** A malformed escape (a stray "%") would throw a URIError and take the page down; keep the raw segment. */
function decodeSegment(segment: string) {
  try {
    return decodeURIComponent(segment)
  } catch {
    return segment
  }
}
