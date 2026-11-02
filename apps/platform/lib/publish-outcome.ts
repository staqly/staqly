// Posts this tab sent off with Publish now, by id (with their title), until their outcome arrives (LiveUpdates).
const awaiting = new Map<string, string>()

/** Remember a post this tab just sent off, to say how it went (a module of its own: the shell loads it). */
export function awaitOutcome(postId: string, title: string) {
  awaiting.set(postId, title)
}

/** The title of a post this tab published and waits to hear about, once: undefined for any other post. */
export function takeAwaitedOutcome(postId: string) {
  const title = awaiting.get(postId)

  awaiting.delete(postId)

  return title
}
