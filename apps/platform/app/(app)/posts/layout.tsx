import { Suspense } from 'react'

import { PostsWorkspace } from '@/components/posts/posts-workspace'

/**
 * The posts view is the layout of /posts and /posts/<id>: opening or closing a post's sheet changes only the page
 * (the sheet), so the list stays mounted with its scroll position, loaded pages and selection.
 */
export default function PostsLayout({ children }: { children: React.ReactNode }) {
  return (
    <>
      <Suspense>
        <PostsWorkspace />
      </Suspense>
      {children}
    </>
  )
}
