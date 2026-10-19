import type { PostComment } from './api'
import { TONES, type Tone } from './tones'

/**
 * Comments on a passage of a previewed post: where a selection sits in a text (offsets in UTF-16 code units,
 * what JavaScript strings count and what the API stores), how the text splits into highlighted pieces, and the
 * threads (a first comment and its replies).
 */

export const QUOTE_MAX = 500

export interface SelectionAnchor {
  quote: string
  start: number
  end: number
}

/**
 * The selected words inside root as a passage: offsets in root's text, surrounding spaces left out. Null when the
 * selection is empty or reaches outside root.
 */
export function anchorFromRange(root: Node, range: Range): SelectionAnchor | null {
  if (range.collapsed || !root.contains(range.startContainer) || !root.contains(range.endContainer)) {
    return null
  }

  const before = document.createRange()

  before.selectNodeContents(root)
  before.setEnd(range.startContainer, range.startOffset)
  const raw = range.toString()
  const lead = raw.length - raw.trimStart().length
  const quote = raw.trim().slice(0, QUOTE_MAX)

  if (!quote) {
    return null
  }

  const start = before.toString().length + lead

  return { quote, start, end: start + quote.length }
}

export interface Mark {
  id: string
  start: number
  end: number
}

export interface Segment {
  text: string
  start: number
  /** The threads marking this piece (several when passages overlap). */
  ids: string[]
}

/** text cut at every mark's edges; each piece lists the marks covering it. Out-of-range marks are clamped. */
export function segments(text: string, marks: Mark[]): Segment[] {
  const valid = marks
    .map((m) => ({
      ...m,
      start: Math.max(0, Math.min(m.start, text.length)),
      end: Math.max(0, Math.min(m.end, text.length)),
    }))
    .filter((m) => m.end > m.start)
  const cuts = [...new Set([0, text.length, ...valid.flatMap((m) => [m.start, m.end])])].toSorted((a, b) => a - b)
  const out: Segment[] = []

  for (let i = 0; i < cuts.length - 1; i++) {
    const [a, b] = [cuts[i]!, cuts[i + 1]!]

    out.push({
      text: text.slice(a, b),
      start: a,
      ids: valid.filter((m) => m.start <= a && m.end >= b).map((m) => m.id),
    })
  }

  return out.length ? out : [{ text, start: 0, ids: [] }]
}

export interface Thread {
  root: PostComment
  replies: PostComment[]
}

/** Comments as threads, oldest first; a reply whose first comment is gone is shown as its own thread. */
export function threadsOf(comments: PostComment[]): Thread[] {
  const roots = new Map<string, Thread>()

  for (const c of comments) {
    if (!c.parent_id) {
      roots.set(c.id, { root: c, replies: [] })
    }
  }

  for (const c of comments) {
    if (c.parent_id) {
      const t = roots.get(c.parent_id)

      if (t) {
        t.replies.push(c)
      } else {
        roots.set(c.id, { root: c, replies: [] })
      }
    }
  }

  return [...roots.values()].toSorted((a, b) => a.root.created_at.localeCompare(b.root.created_at))
}

/** The marks of open threads on one text (a channel's version and part). */
export function marksFor(threads: Thread[], channelId: string, part: number): Mark[] {
  return threads
    .filter(
      (t) =>
        !t.root.resolved_at &&
        t.root.quote &&
        t.root.anchor_start >= 0 &&
        t.root.channel_id === channelId &&
        t.root.part === part,
    )
    .map((t) => ({ id: t.root.id, start: t.root.anchor_start, end: t.root.anchor_end }))
}

export function initials(name: string) {
  const words = name.trim().split(/\s+/).filter(Boolean)

  return ((words[0]?.[0] ?? '?') + (words.length > 1 ? (words.at(-1)?.[0] ?? '') : '')).toUpperCase()
}

const AUTHOR_TONES: Tone[] = ['info', 'success', 'warning', 'accent', 'attention', 'danger', 'neutral']

/** The same colour for the same name, on every device. */
export function authorChip(name: string) {
  let h = 0

  for (const ch of name.trim().toLowerCase()) {
    h = (h * 31 + ch.codePointAt(0)!) | 0
  }

  return TONES[AUTHOR_TONES[Math.abs(h) % AUTHOR_TONES.length]!].chip
}
