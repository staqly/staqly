import type { Metadata } from 'next'
import { Suspense } from 'react'

import { PostsWorkspace } from '@/components/posts/posts-workspace'

export const metadata: Metadata = { title: 'Posts' }

export default function PostsPage() {
  return (
    <Suspense>
      <PostsWorkspace />
    </Suspense>
  )
}
