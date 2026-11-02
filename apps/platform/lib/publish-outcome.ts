// Posts this tab sent off (Publish now) or is taking down (Unpublish), by id, until the live events say how it went
// (LiveUpdates). A module of its own: the shell loads it on every page.
const awaiting = new Map<string, { title: string; unpublish: boolean; left: number; failed: number }>()

/** Remember a post this tab just published (or started taking down from `channels` channels). */
export function awaitOutcome(postId: string, title: string, unpublish = false, channels = 1) {
  if (channels < 1) {
    return
  }

  awaiting.set(postId, { title, unpublish, left: channels, failed: 0 })
}

/** Forget a post (its publishing was undone). */
export function forgetOutcome(postId: string) {
  awaiting.delete(postId)
}

/**
 * How an event ends what this tab waits for: the post's title and whether it went well, once every channel being
 * taken down settled; undefined for an event of any other post, or while channels are still being taken down.
 */
export function outcomeOf(e: { type: string; post_id?: string | null }) {
  const w = e.post_id ? awaiting.get(e.post_id) : undefined
  const publish = e.type === 'post.published' || e.type === 'post.failed'
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

  return { title: w.title, unpublish: w.unpublish, ok: w.failed === 0 }
}
