// The design editor opened inside the dashboard (a drawer over a post) instead of on its own page: what it is
// told, and where the pictures it saves go. Pure, so the post's code loads none of the editor.
import type { Media } from '@/lib/api'

/** A page the editor saved: its picture, and the file it replaced (a page saved before replaces its own file). */
export interface SavedFile {
  before?: string
  after: Media
}

export interface Embedded {
  /** Saved: every page's picture, in order. The drawer closes after. */
  onSaved: (files: SavedFile[]) => void
  /** Closed without saving (after asking, when there were unsaved changes). */
  onClose: () => void
  /** Opens another saved design in its place (Your designs, under Templates). */
  onOpen: (project: string) => void
  /** The editor hands the drawer its answer to Escape: it asks first when there are unsaved changes. */
  onEscape: (fn: (() => void) | null) => void
}

/**
 * The post's pictures after a save: a file the editor replaced is swapped where it is, the picture being edited
 * (`editing`) becomes the new pictures in its place, and a new design's pictures come last.
 */
export function placeSaved(media: Media[], saved: SavedFile[], editing?: string): Media[] {
  const swaps = new Map(saved.flatMap((s) => (s.before ? [[s.before, s.after] as const] : [])))
  const has = new Set(media.map((m) => m.id))
  let fresh = saved.filter((s) => !s.before || !has.has(s.before)).map((s) => s.after)
  const out = media.flatMap((m) => {
    const swap = swaps.get(m.id)

    if (swap) {
      return [swap]
    }

    if (m.id === editing && fresh.length) {
      const placed = fresh

      fresh = []

      return placed
    }

    return [m]
  })

  return editing && media.some((m) => m.id === editing) ? out : [...out, ...fresh]
}
