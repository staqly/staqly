'use client'

import { type InfiniteData, type QueryClient, useQuery, useQueryClient } from '@tanstack/react-query'
import { lazy, Suspense, useEffect, useState } from 'react'

import { ErrorState } from '@/components/common/error-state'
import { Sheet, SheetContent, SheetTitle } from '@/components/ui/sheet'
import { Skeleton } from '@/components/ui/skeleton'
import { api, type Post, type PostPage } from '@/lib/api'
import { qk, useChannelGroups, useChannels, useProviders, useTags } from '@/lib/queries'

import { PostActions } from './post-actions'

const loadComposer = () => import('./composer').then((m) => m.ComposerForm)

// The editor (Tiptap) loads with the sheet, not with the page behind it.
const ComposerForm = lazy(() => loadComposer().then((form) => ({ default: form })))

function SheetSkeleton() {
  return (
    <div className="grid gap-3 p-4">
      <SheetTitle className="sr-only">Post</SheetTitle>
      <Skeleton className="h-6 w-2/3" />
      <Skeleton className="h-40" />
    </div>
  )
}

/** The post's text while the editor's code still loads (on a slow phone it is the larger part of opening it). */
function SheetText({ post }: { post: Post }) {
  return (
    <div className="grid gap-3 p-4">
      <SheetTitle className="sr-only">Post</SheetTitle>
      <Skeleton className="h-6 w-2/3" />
      <p className="min-h-40 rounded-md border px-3 py-2 text-sm whitespace-pre-wrap">{post.content}</p>
    </div>
  )
}

/** The post as a loaded list holds it (the posts table, the calendar), to show while the post itself loads. */
function postFromLists(qc: QueryClient, id: string): Post | undefined {
  for (const [, data] of qc.getQueriesData<InfiniteData<PostPage>>({ queryKey: qk.postsAll })) {
    for (const page of data?.pages ?? []) {
      const hit = page.data.find((p) => p.id === id)

      if (hit) {
        return hit
      }
    }
  }

  return undefined
}

/**
 * What the composer needs besides the post, asked for together with it: its code and its queries. Otherwise
 * each waits for the one before (the post, then the composer's code, then the channels and providers).
 */
function ComposerPreload() {
  useChannels()
  useProviders()
  useTags()
  useChannelGroups()

  useEffect(() => {
    void loadComposer()
  }, [])

  return null
}

/**
 * A saved post: the composer itself (channels, editor, channel tabs), with each channel's state, link, error
 * and Retry; the header's icon row opens it on the platform, shows its numbers, shares a preview, duplicates and
 * deletes it. Read only once it went out.
 */
export function PostSheet({ id, channel, onClose }: { id: string | null; channel?: string; onClose: () => void }) {
  const qc = useQueryClient()
  // A post from a loaded list shows at once; the post itself still loads (and replaces it).
  const post = useQuery({
    queryKey: qk.post(id ?? ''),
    queryFn: () => api.post(id!),
    enabled: !!id,
    placeholderData: () => (id ? postFromLists(qc, id) : undefined),
  })
  const p = post.data
  // The form's fields are its initial state. When the list's copy turns out older than the post, the form starts
  // over from the post; later versions (a save, a retry) keep what is typed.
  const [shown, setShown] = useState<{ id: string; version: number; checked: boolean; round: number } | null>(null)

  if (p && shown?.id !== p.id) {
    setShown({ id: p.id, version: p.version, checked: !post.isPlaceholderData, round: 0 })
  } else if (p && shown && !shown.checked && !post.isPlaceholderData) {
    setShown({ ...shown, checked: true, round: p.version === shown.version ? shown.round : shown.round + 1 })
  }

  return (
    <Sheet open={!!id} onOpenChange={(o) => !o && onClose()}>
      {id && <ComposerPreload />}
      <SheetContent className="gap-0 data-[side=right]:w-full data-[side=right]:sm:max-w-2xl data-[side=right]:lg:max-w-5xl">
        {post.isLoading || !id ? (
          <SheetSkeleton />
        ) : post.error || !p ? (
          <div className="p-4">
            <SheetTitle className="sr-only">Post</SheetTitle>
            <ErrorState error={post.error ?? new Error('Post not found')} onRetry={() => post.refetch()} />
          </div>
        ) : (
          <Suspense fallback={<SheetText post={p} />}>
            <ComposerForm
              key={`${p.id}:${shown?.id === p.id ? shown.round : 0}`}
              post={p}
              initialTab={channel}
              headerActions={<PostActions post={p} onDeleted={onClose} />}
              onDone={onClose}
            />
          </Suspense>
        )}
      </SheetContent>
    </Sheet>
  )
}
