// The publishing flow as the tests drive it: wait for a post or its targets
// to reach a state, hand a post to "your own automation" and report back.
import type { Api, Post } from './clients.js'
import { waitFor } from './wait.js'

/** Waits until the post has the given status. */
export function waitForPost(agent: Api, id: string, status: Post['status'], timeout = 30_000): Promise<Post> {
  return waitFor(
    async () => {
      const p = await agent.getPost(id)

      return p.status === status ? p : undefined
    },
    { message: `post ${id} did not become ${status}`, timeout, interval: 500 },
  )
}

/**
 * Waits until every target of the post has been handed to the automation:
 * publishing, with at least `attempts` tries (post.publish_requested was emitted).
 */
export function waitForRequested(agent: Api, id: string, attempts = 1, timeout = 30_000): Promise<Post> {
  return waitFor(
    async () => {
      const p = await agent.getPost(id)

      return p.targets.every((t) => t.status === 'published' || (t.status === 'publishing' && t.attempts >= attempts))
        ? p
        : undefined
    },
    { message: `targets of ${id} were not handed to the automation`, timeout, interval: 500 },
  )
}

/** Publish now and wait until the automation was asked for every target. */
export async function publishNow(agent: Api, id: string): Promise<Post> {
  await agent.publish(id)

  return waitForRequested(agent, id)
}

/** Publish a post on custom channels and report every target as published, so the post ends published. */
export async function publishThrough(agent: Api, id: string, url = 'https://example.com/posts/1'): Promise<Post> {
  const asked = await publishNow(agent, id)
  let post = asked

  for (const t of asked.targets.filter((x) => x.status === 'publishing')) {
    post = await agent.result(id, t.channel_id, { status: 'published', url })
  }

  return post
}
