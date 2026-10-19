'use client'

import { toast } from 'sonner'

import { useNow } from '@/hooks/use-now'
import { api, type Post } from '@/lib/api'
import { toastApiError } from '@/lib/notify'
import { postTitle } from '@/lib/posts'

/** How long Publish now waits for an Undo. Tests shorten it with window.staqlyUndoMs. */
export function undoDelayMs() {
  const override = typeof window === 'undefined' ? undefined : (window as { staqlyUndoMs?: number }).staqlyUndoMs

  return typeof override === 'number' ? override : 7000
}

/** "Publishing in 7 s", counting down while the toast is up. */
function Countdown({ until }: { until: number }) {
  const now = useNow(true, 250)
  const left = Math.max(0, Math.ceil((until - now) / 1000))

  return <>{left > 0 ? `Publishing in ${left} s` : 'Publishing now'}</>
}

/**
 * The toast after Publish now: a countdown and an Undo that takes the post off the queue before it goes out (it
 * becomes a draft again). Without Undo the queue publishes it when the time is up.
 */
export function undoToast(saved: Post, onUndone: (draft: Post) => void) {
  const delay = undoDelayMs()
  const until = Date.now() + delay

  toast(<Countdown until={until} />, {
    description: postTitle(saved, 60),
    duration: delay,
    action: {
      label: 'Undo',
      onClick: () => {
        api
          .cancelPost(saved.id)
          .then((draft) => {
            onUndone(draft)
            toast.success('Publishing canceled', { description: 'The post is a draft again.' })
          })
          .catch((e) => toastApiError(e))
      },
    },
  })
}
