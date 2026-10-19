'use client'

import { useEffect } from 'react'

/**
 * Pages used to live under /docs/...; GitHub Pages serves 404.html for those
 * old links, so send the browser to the same page at the root.
 */
export function LegacyRedirect() {
  useEffect(() => {
    const { pathname, search, hash } = window.location

    if (pathname === '/docs' || pathname.startsWith('/docs/')) {
      window.location.replace(`${pathname.slice('/docs'.length) || '/'}${search}${hash}`)
    }
  }, [])

  return null
}
