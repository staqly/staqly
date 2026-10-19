'use client'

import { useEffect } from 'react'
import { toast } from 'sonner'

import type { XPosts } from './api'
import { useMode, useXPosts } from './queries'

// The X posts allowance without its UI: the app shell warns on every page (useXPostsWarnings), so this module
// stays light; the billing section and the Buy more dialog live in components/billing/x-posts.tsx.

/** Nov 30: a short day for "expire Nov 30" and "wait until Nov 1". */
export function shortDay(iso?: string | null) {
  return iso ? new Date(iso).toLocaleDateString('en-US', { month: 'short', day: 'numeric', timeZone: 'UTC' }) : ''
}

/** "37 of 250 included · 200 extra left (expire Nov 30)". */
export function xPostsLine(x: XPosts) {
  if (x.unlimited) {
    return `${x.used.toLocaleString('en-US')} this month, no limit`
  }

  const used = Math.min(x.included - x.included_left, x.included)
  let line = `${used.toLocaleString('en-US')} of ${x.included.toLocaleString('en-US')} included`

  if (x.credits_left > 0) {
    line += ` · ${x.credits_left.toLocaleString('en-US')} extra left (expire ${shortDay(x.credits_expire_at)})`
  }

  return line
}

/** How full the allowance is: 0 to 1 (included and bought posts together). */
export function xPostsShare(x: XPosts) {
  if (x.unlimited) {
    return 0
  }

  const total = x.used + x.left

  return total > 0 ? Math.min(x.used / total, 1) : 1
}

/** The warning at 80 % and 100 %, or nothing. */
export function xPostsWarning(x: XPosts) {
  if (x.unlimited) {
    return ''
  }

  if (x.left <= 0) {
    return `Your X posts for this period are used up. Buy more or wait until ${shortDay(x.period_end)}.`
  }

  if (xPostsShare(x) >= 0.8) {
    return `Only ${x.left.toLocaleString('en-US')} X posts left this period.`
  }

  return ''
}

/** App-wide: a toast once a period when the X posts reach 80 % and again when they run out. */
export function useXPostsWarnings() {
  const { isLocal } = useMode()
  const x = useXPosts({ enabled: !isLocal })

  useEffect(() => {
    if (!x.data || x.data.unlimited) {
      return
    }

    const level = x.data.left <= 0 ? '100' : xPostsShare(x.data) >= 0.8 ? '80' : ''

    if (!level) {
      return
    }

    const key = `staqly.x-posts-warned.${x.data.period}.${level}`

    try {
      if (sessionStorage.getItem(key)) {
        return
      }

      sessionStorage.setItem(key, '1')
    } catch {
      // No storage (private window): warn anyway, once per page load.
    }

    const message = xPostsWarning(x.data)

    if (level === '100') {
      toast.error(message, {
        action: { label: 'Buy more', onClick: () => window.location.assign('/settings/plan') },
      })
    } else {
      toast.warning(message)
    }
  }, [x.data])
}
