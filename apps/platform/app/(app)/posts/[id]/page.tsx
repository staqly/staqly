import type { Metadata } from 'next'
import { Suspense } from 'react'

import { PostRoute } from '@/components/posts/post-route'

export const metadata: Metadata = { title: 'Post' }

// One prerendered shell for every id; the Go web server maps /posts/* onto it.
export function generateStaticParams() {
  return [{ id: '_' }]
}

export default function Page() {
  return (
    <Suspense>
      <PostRoute />
    </Suspense>
  )
}
