'use client'

import { createContext } from 'react'

/**
 * The posts view's query string: the open collection and its filters. Links to a post carry it, so the list
 * (kept mounted by app/(app)/posts/layout.tsx) stays as it was behind the post's sheet, and closing the sheet
 * goes back to it. Rows read it from context: it changes only with the filters, which re-render the list anyway.
 */
export const PostsQueryContext = createContext('')

/** /posts/<id>, with the posts view's query string when there is one. */
export function postHref(id: string, query = '') {
  return query ? `/posts/${id}?${query}` : `/posts/${id}`
}

/** The posts view's query string for post links: always the open collection, never a one-off flag. */
export function postsQuery(params: { toString(): string }, collection: string) {
  const sp = new URLSearchParams(params.toString())

  sp.set('collection', collection)
  sp.delete('compose')
  sp.delete('new')

  return sp.toString()
}
