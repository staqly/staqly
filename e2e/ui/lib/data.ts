// Data a single test owns: a fresh collection with a custom channel and a few
// draft posts, so specs never depend on what another spec published or deleted.
import type { Api, Channel, Post } from './api.js'
import { uniq } from './fixtures.js'

export async function freshCollection(
  api: Api,
  opts: { name?: string; posts?: number; contents?: string[]; withChannel?: boolean } = {},
): Promise<{ id: string; name: string; channel: Channel; posts: Post[] }> {
  const name = opts.name ?? uniq('UI col')
  const col = await api.collection({ name })
  const channel = await api.channel(uniq('UI channel'))
  const contents = opts.contents ?? Array.from({ length: opts.posts ?? 3 }, (_, i) => `${name} post ${i + 1}`)
  const posts: Post[] = []

  for (const content of contents) {
    posts.push(
      await api.createPost(col.id, {
        content,
        source: 'ui-test',
        tags: ['ui-test'],
        ...(opts.withChannel === false ? {} : { targets: [{ channel_id: channel.id }] }),
      }),
    )
  }

  return { id: col.id, name, channel, posts }
}

export async function getPost(api: Api, id: string): Promise<Post> {
  return api.getPost(id)
}

/** The first line of a post's content, as the dashboard titles it. */
export const titleOf = (content: string) => content.split('\n')[0]!.trim()

const pad = (n: number) => String(n).padStart(2, '0')

export const dayKey = (d: Date) => `${d.getFullYear()}-${pad(d.getMonth() + 1)}-${pad(d.getDate())}`

/** `offset` days ahead at `hour`:00 local time: in the future whatever the clock says. */
export function daysAhead(offset: number, hour: number): Date {
  const d = new Date()

  d.setDate(d.getDate() + offset)
  d.setHours(hour, 0, 0, 0)

  return d
}
