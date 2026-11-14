'use client'

import { useRouter, useSearchParams } from 'next/navigation'

import { useRouteId } from '@/hooks/use-route-id'
import { usePost } from '@/lib/queries'

import { PostSheet } from './post-sheet'

/**
 * /posts/<id>: that post's sheet over the posts view (the posts layout, which stays mounted). Closing goes back
 * to the view as it was: the query string the link carried, or the post's own collection for a bookmark.
 */
export function PostRoute() {
  const id = useRouteId()
  const router = useRouter()
  const params = useSearchParams()
  const collection = usePost(id ?? '').data?.collection_id
  const query = params.has('collection') ? params.toString() : collection ? `collection=${collection}` : ''

  return id ? (
    <PostSheet id={id} onClose={() => router.push(query ? `/posts?${query}` : '/posts', { scroll: false })} />
  ) : null
}
