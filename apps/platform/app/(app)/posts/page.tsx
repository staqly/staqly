import type { Metadata } from 'next'

export const metadata: Metadata = { title: 'Posts' }

// The list is the layout (layout.tsx); /posts itself adds nothing over it.
export default function PostsPage() {
  return null
}
