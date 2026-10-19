'use client'

import { useRouter } from 'next/navigation'

import { useRouteId } from '@/hooks/use-route-id'

import { PostSheet } from './post-sheet'
import { PostsWorkspace } from './posts-workspace'

/** /posts/<id>: the posts view with that post's sheet open; closing it goes back to the list. */
export function PostRoute() {
  const id = useRouteId()
  const router = useRouter()

  return (
    <>
      <PostsWorkspace />
      {id && <PostSheet id={id} onClose={() => router.push('/posts')} />}
    </>
  )
}
