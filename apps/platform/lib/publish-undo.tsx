'use client'

import { Loader2Icon } from 'lucide-react'
import { useSyncExternalStore } from 'react'
import { toast } from 'sonner'

import { useNow } from '@/hooks/use-now'
import { api, type Post } from '@/lib/api'
import { toastApiError } from '@/lib/notify'
import { postTitle } from '@/lib/posts'
import {
  awaitOutcome,
  forgetOutcome,
  isAwaited,
  progressOf,
  publishToastId,
  subscribeProgress,
} from '@/lib/publish-outcome'

/** How Publish now waits for an Undo. Tests shorten it with window.staqlyUndoMs. */
export function undoDelayMs() {
  const override = typeof window === 'undefined' ? undefined : (window as { staqlyUndoMs?: number }).staqlyUndoMs

  return typeof override === 'number' ? override : 7000
}

/** How long the toast follows the publishing; afterwards it says it goes on and closes (the post shows how it went). */
const FOLLOW_MS = 3 * 60_000

/** "Publishing in 7 s", counting down while the toast is up. */
function Countdown({ until }: { until: number }) {
  const now = useNow(true, 250)
  const left = Math.max(0, Math.ceil((until - now) / 1000))

  return <>{left > 0 ? `Publishing in ${left} s` : 'Publishing now'}</>
}

/** "Publishing: 1 of 3 channels" as the channels report back. */
function Progress({ postId }: { postId: string }) {
  const p = useSyncExternalStore(
    subscribeProgress,
    () => progressOf(postId),
    () => undefined,
  )

  if (!p || p.total < 2) {
    return <>Publishing now</>
  }

  return (
    <>
      Publishing: {p.done} of {p.total} channels{p.failed > 0 && ` (${p.failed} failed)`}
    </>
  )
}

/**
 * The toast after Publish now: a countdown and an Undo that takes the post off the queue before it goes out (it
 * becomes a draft again). Without Undo the queue publishes it when the time is up; the toast then stays, follows the
 * channels as they report back and turns into Published or Couldn't publish (LiveUpdates, same toast id).
 */
export function undoToast(saved: Post, onUndone: (draft: Post) => void) {
  const delay = undoDelayMs()
  const until = Date.now() + delay
  const id = publishToastId(saved.id)
  const description = postTitle(saved, 60)

  awaitOutcome(saved.id, description, false, Math.max(1, saved.targets?.length ?? 0))
  toast.info(<Countdown until={until} />, {
    id,
    description,
    duration: Infinity,
    action: {
      label: 'Undo',
      onClick: () => {
        api
          .cancelPost(saved.id)
          .then((draft) => {
            forgetOutcome(saved.id)
            onUndone(draft)
            toast.success('Publishing canceled', { id, description: 'The post is a draft again.', duration: 4000 })
          })
          .catch((e) => toastApiError(e))
      },
    },
  })
  setTimeout(() => {
    if (isAwaited(saved.id)) {
      // Too late to undo: the post is going out. The same info toast, a spinner in place of its icon.
      toast.info(<Progress postId={saved.id} />, {
        id,
        description,
        action: undefined,
        icon: <Loader2Icon aria-hidden className="size-4 animate-spin" />,
      })
    }
  }, delay)
  setTimeout(() => {
    if (isAwaited(saved.id)) {
      toast.info('Still publishing', { id, description: `${description}: open it to follow it.`, duration: 6000 })
    }
  }, delay + FOLLOW_MS)
}
