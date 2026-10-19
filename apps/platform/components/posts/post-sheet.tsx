'use client'

import { useMutation, useQueryClient } from '@tanstack/react-query'
import { Copy, Ellipsis, Send, Trash2 } from 'lucide-react'
import dynamic from 'next/dynamic'
import { useState } from 'react'

import { ConfirmDialog } from '@/components/common/confirm-dialog'
import { ErrorState } from '@/components/common/error-state'
import { Button } from '@/components/ui/button'
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuSeparator,
  DropdownMenuTrigger,
} from '@/components/ui/dropdown-menu'
import { Sheet, SheetContent, SheetTitle } from '@/components/ui/sheet'
import { Skeleton } from '@/components/ui/skeleton'
import { api, type Post } from '@/lib/api'
import { namedToast, toastApiError } from '@/lib/notify'
import { canReschedule, postTitle } from '@/lib/posts'
import { invalidateAfterPostChange, removePostFromLists, syncPostInCache, usePost } from '@/lib/queries'

// The editor (Tiptap) loads with the sheet, not with the page behind it.
const ComposerForm = dynamic(() => import('./composer').then((m) => m.ComposerForm), {
  ssr: false,
  loading: () => <SheetSkeleton />,
})

function SheetSkeleton() {
  return (
    <div className="grid gap-3 p-4">
      <SheetTitle className="sr-only">Post</SheetTitle>
      <Skeleton className="h-6 w-2/3" />
      <Skeleton className="h-40" />
    </div>
  )
}

/** Publish now, Duplicate and Delete, in the header's More menu. */
function PostMenu({ post, onDeleted }: { post: Post; onDeleted: () => void }) {
  const qc = useQueryClient()
  const [deleteOpen, setDeleteOpen] = useState(false)
  const publish = useMutation({
    mutationFn: () => api.publishPost(post.id),
    onSuccess: (p) => {
      syncPostInCache(qc, p)
      invalidateAfterPostChange(qc, p.id)
      namedToast(postTitle(p, 40), 'is being published')
    },
    onError: (e) => toastApiError(e),
  })
  const duplicate = useMutation({
    mutationFn: () => api.duplicatePost(post.id),
    onSuccess: (p) => {
      invalidateAfterPostChange(qc, p.id, { usage: true })
      namedToast(postTitle(p, 40), 'was duplicated as a draft')
    },
    onError: (e) => toastApiError(e),
  })
  const del = useMutation({
    mutationFn: () => api.deletePost(post.id),
    onSuccess: () => {
      removePostFromLists(qc, post.id)
      invalidateAfterPostChange(qc, undefined, { usage: true })
      namedToast(postTitle(post, 40), 'was deleted')
      setDeleteOpen(false)
      onDeleted()
    },
    onError: (e) => toastApiError(e),
  })

  return (
    <>
      <DropdownMenu>
        <DropdownMenuTrigger asChild>
          <Button type="button" size="sm" variant="outline" aria-label="More actions">
            <Ellipsis /> More
          </Button>
        </DropdownMenuTrigger>
        <DropdownMenuContent align="end">
          {canReschedule(post) && (post.targets?.length ?? 0) > 0 && (
            <DropdownMenuItem onSelect={() => publish.mutate()} disabled={publish.isPending}>
              <Send /> Publish now
            </DropdownMenuItem>
          )}
          <DropdownMenuItem onSelect={() => duplicate.mutate()}>
            <Copy /> Duplicate
          </DropdownMenuItem>
          <DropdownMenuSeparator />
          <DropdownMenuItem variant="destructive" onSelect={() => setDeleteOpen(true)}>
            <Trash2 /> Delete
          </DropdownMenuItem>
        </DropdownMenuContent>
      </DropdownMenu>
      <ConfirmDialog
        open={deleteOpen}
        onOpenChange={setDeleteOpen}
        title="Delete this post?"
        description={
          post.status === 'published'
            ? 'It stays where it was published; only staqly forgets it.'
            : 'It will not be published.'
        }
        confirmLabel={del.isPending ? 'Deleting...' : 'Delete'}
        variant="destructive"
        onConfirm={() => del.mutate()}
      />
    </>
  )
}

/**
 * A saved post: the composer itself (channels, editor, channel tabs), with each channel's state, link, error
 * and Retry, and Publish now, Duplicate and Delete under More. Read only once it went out.
 */
export function PostSheet({ id, channel, onClose }: { id: string | null; channel?: string; onClose: () => void }) {
  const post = usePost(id ?? '')
  const p = post.data

  return (
    <Sheet open={!!id} onOpenChange={(o) => !o && onClose()}>
      <SheetContent className="gap-0 data-[side=right]:w-full data-[side=right]:sm:max-w-2xl data-[side=right]:lg:max-w-5xl">
        {post.isLoading || !id ? (
          <SheetSkeleton />
        ) : post.error || !p ? (
          <div className="p-4">
            <SheetTitle className="sr-only">Post</SheetTitle>
            <ErrorState error={post.error ?? new Error('Post not found')} onRetry={() => post.refetch()} />
          </div>
        ) : (
          <ComposerForm
            key={p.id}
            post={p}
            initialTab={channel}
            headerActions={<PostMenu post={p} onDeleted={onClose} />}
            onDone={onClose}
          />
        )}
      </SheetContent>
    </Sheet>
  )
}
