'use client'

import dynamic from 'next/dynamic'

import { ErrorState } from '@/components/common/error-state'
import { Sheet, SheetContent, SheetTitle } from '@/components/ui/sheet'
import { Skeleton } from '@/components/ui/skeleton'
import { usePost } from '@/lib/queries'

import { PostActions } from './post-actions'

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

/**
 * A saved post: the composer itself (channels, editor, channel tabs), with each channel's state, link, error
 * and Retry; the header's icon row opens it on the platform, shows its numbers, shares a preview, duplicates and
 * deletes it. Read only once it went out.
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
            headerActions={<PostActions post={p} onDeleted={onClose} />}
            onDone={onClose}
          />
        )}
      </SheetContent>
    </Sheet>
  )
}
