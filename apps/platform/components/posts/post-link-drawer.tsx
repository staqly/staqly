'use client'

import { usePathname } from 'next/navigation'
import { useEffect, useState } from 'react'

import { PostSheet } from '@/components/posts/post-sheet-lazy'

const POST_PATH = /^\/posts\/([^/?#]+)$/

/**
 * Every link to /posts/<id> outside the posts view opens the post's drawer over the current page instead of
 * leaving it (the media drawer's Used in, alerts, notifications, the activity feed, analytics). One listener in
 * the capture phase, before Next's Link navigates, so new links follow the rule without wiring. A plain click
 * only: Cmd/Ctrl/Shift/Alt-click, middle-click and target=_blank still follow the link. The posts view keeps
 * its own route-based sheet (the list stays mounted behind it).
 */
export function PostLinkDrawer() {
  const pathname = usePathname()
  const [opened, setOpened] = useState<string | null>(null)
  // The drawer's code loads on the first post opened, not with every page; it stays mounted to animate out.
  const [used, setUsed] = useState(false)
  const onPosts = pathname === '/posts' || pathname.startsWith('/posts/')

  useEffect(() => {
    if (onPosts) {
      return
    }

    const onClick = (e: MouseEvent) => {
      if (e.defaultPrevented || e.button !== 0 || e.metaKey || e.ctrlKey || e.shiftKey || e.altKey) {
        return
      }

      const a = (e.target as Element | null)?.closest?.('a[href]')

      if (!(a instanceof HTMLAnchorElement) || (a.target && a.target !== '_self') || a.origin !== location.origin) {
        return
      }

      const m = POST_PATH.exec(a.pathname)

      if (!m || a.search) {
        return
      }

      e.preventDefault()
      setUsed(true)
      setOpened(decodeURIComponent(m[1]))
    }

    document.addEventListener('click', onClick, true)

    return () => document.removeEventListener('click', onClick, true)
  }, [onPosts])

  return used && <PostSheet id={onPosts ? null : opened} onClose={() => setOpened(null)} />
}
