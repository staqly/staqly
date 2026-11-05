// Posts this tab sent off (Publish now) or is taking down (Unpublish), by id, until the live events say how it went
// (LiveUpdates). A module of its own: the shell loads it on every page.
const awaiting = new Map<string, { title: string; unpublish: boolean; left: number; failed: number }>()

/** The toast's id for a post: the countdown, the progress and how it went replace each other in one toast. */
export const publishToastId = (postId: string) => `publish:${postId}`

/** How far a post this tab publishes got: channels done (failed among them) of all. A new object on every change. */
export type PublishProgress = { done: number; failed: number; total: number }

const progress = new Map<string, PublishProgress>()
const listeners = new Set<() => void>()
const changed = () => {
  for (const l of listeners) {
    l()
  }
}

/** Remember a post this tab just published (to `channels` channels) or started taking down from `channels`. */
export function awaitOutcome(postId: string, title: string, unpublish = false, channels = 1) {
  if (channels < 1) {
    return
  }

  // A post publishes with one post.published (or failed) at the end; it is taken down a channel at a time.
  awaiting.set(postId, { title, unpublish, left: unpublish ? channels : 1, failed: 0 })

  if (!unpublish) {
    progress.set(postId, { done: 0, failed: 0, total: channels })
    changed()
  }
}

/** Forget a post (its publishing was undone). */
export function forgetOutcome(postId: string) {
  awaiting.delete(postId)
  progress.delete(postId)
}

/** True while this tab still waits for how the post went. */
export function isAwaited(postId: string) {
  return awaiting.has(postId)
}

/** For useSyncExternalStore: the publishing progress changed. */
export function subscribeProgress(listener: () => void) {
  listeners.add(listener)

  return () => {
    listeners.delete(listener)
  }
}

export function progressOf(postId: string) {
  return progress.get(postId)
}

/**
 * How an event ends what this tab waits for: the post's title and whether it went well, once every channel being
 * taken down settled; undefined for an event of any other post, or while channels are still being taken down.
 */
export function outcomeOf(e: { type: string; post_id?: string | null }) {
  const w = e.post_id ? awaiting.get(e.post_id) : undefined
  const publish = e.type === 'post.published' || e.type === 'post.failed'
  const p = e.post_id ? progress.get(e.post_id) : undefined

  if (p && (e.type === 'post.target_published' || e.type === 'post.target_failed')) {
    progress.set(e.post_id!, {
      ...p,
      done: Math.min(p.total, p.done + 1),
      failed: p.failed + (e.type === 'post.target_failed' ? 1 : 0),
    })
    changed()

    return undefined
  }

  const removal = e.type === 'post.target_removed' || e.type === 'post.target_remove_failed'

  if (!w || !e.post_id || (w.unpublish ? !removal : !publish)) {
    return undefined
  }

  w.left--

  if (e.type === 'post.failed' || e.type === 'post.target_remove_failed') {
    w.failed++
  }

  if (w.left > 0) {
    return undefined
  }

  awaiting.delete(e.post_id)
  progress.delete(e.post_id)

  return { title: w.title, unpublish: w.unpublish, ok: w.failed === 0 }
}
