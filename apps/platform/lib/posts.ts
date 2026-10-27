import { findLinks } from '@/lib/links'

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

/**
 * A post that went out, in full or on any of its channels, or is going out right now: it is history, and the
 * API refuses to delete it (409). Only purge, retention and deleting its collection or the workspace remove it.
 */
export function wentOut(post: Pick<Post, 'status' | 'targets'>) {
  return isLocked(post) || (post.targets ?? []).some((t) => t.status === 'published')
}

/** Published targets, for the links under a published post. */
export function publishedTargets(post: Pick<Post, 'targets'>): PostTarget[] {
  return (post.targets ?? []).filter((t) => t.status === 'published')
}

/** Characters as the platforms count them (code points, so an emoji is one). */
export function charCount(text: string) {
  return [...text].length
}

/**
 * Text length the way X counts it (twitter-text v3), as the API checks it: Latin and most punctuation 1,
 * everything else (CJK, emoji) 2, an emoji sequence (a family, a skin tone, a flag, a keycap) 2 in all, every
 * link 23 whatever its length.
 */
export function xLength(text: string) {
  let n = 0

  for (const u of findLinks(text)) {
    n += 23 - [...u].length // its characters are counted below: a link is 23 in all
  }

  let joined = false
  let flag = false

  for (const ch of text) {
    const r = ch.codePointAt(0) ?? 0

    if (r === 0x200d) {
      joined = true // zero-width joiner: the next character belongs to the same emoji

      continue
    }

    if (joined || r === 0xfe0f || (r >= 0x1f3fb && r <= 0x1f3ff) || r === 0x20e3 || (r >= 0xe0020 && r <= 0xe007f)) {
      joined = false // part of the emoji before it

      continue
    }

    if (r >= 0x1f1e6 && r <= 0x1f1ff) {
      // Regional indicators: a pair is one flag.
      flag = !flag
      n += flag ? 2 : 0

      continue
    }

    flag = false
    n +=
      r <= 0x10ff || (r >= 0x2000 && r <= 0x200d) || (r >= 0x2010 && r <= 0x201f) || (r >= 0x2032 && r <= 0x2037)
        ? 1
        : 2
  }

  return n
}

/**
 * A delivery error as the server words it, "LinkedIn refused it (the platform's own text); staqly tries again",
 * split into the sentence a person reads and the platform's text (the first parenthesised part) for the details.
 */
export function splitDeliveryError(error: string): { summary: string; detail: string } {
  const open = error.indexOf(' (')

  if (open < 0) {
    return { summary: error, detail: '' }
  }

  let depth = 0

  for (let i = open + 1; i < error.length; i++) {
    if (error[i] === '(') {
      depth++
    } else if (error[i] === ')' && --depth === 0) {
      return { summary: error.slice(0, open) + error.slice(i + 1), detail: error.slice(open + 2, i) }
    }
  }

  return { summary: error, detail: '' }
}
