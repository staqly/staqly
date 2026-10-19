import type { Post, PostTarget } from './api'

/** The first line of a post, shortened: the title a post does not have. */
export function postTitle(post: Pick<Post, 'content'>, max = 80): string {
  const line = post.content.trim().split('\n')[0] ?? ''

  if (!line) {
    return 'Untitled post'
  }

  return line.length > max ? `${line.slice(0, max - 3)}...` : line
}

/** The schedule can still be set, moved or cleared: nothing has gone out yet. */
export function canReschedule(post: Pick<Post, 'status'>) {
  return post.status === 'draft' || post.status === 'scheduled' || post.status === 'failed'
}

/** A post whose content is final (the API refuses edits with 409). */
export function isLocked(post: Pick<Post, 'status'>) {
  return post.status === 'published' || post.status === 'publishing'
}

/** Published targets, for the links under a published post. */
export function publishedTargets(post: Pick<Post, 'targets'>): PostTarget[] {
  return (post.targets ?? []).filter((t) => t.status === 'published')
}

/** Counts per target status, for the "2 of 3 published" line. */
export function targetSummary(post: Pick<Post, 'targets'>) {
  const targets = post.targets ?? []
  const n = (s: PostTarget['status']) => targets.filter((t) => t.status === s).length

  return { total: targets.length, published: n('published'), failed: n('failed'), publishing: n('publishing') }
}

/** Characters as the platforms count them (code points, so an emoji is one). */
export function charCount(text: string) {
  return [...text].length
}
