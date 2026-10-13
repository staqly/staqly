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

  return decodeURIComponent(pathname.split('/').findLast(Boolean) ?? '')
}
