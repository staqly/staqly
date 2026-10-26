// Unsaved work survives a closed tab or a crash: an editor keeps a draft in this browser (per document) and offers
// it back when the same document opens again. ponytail: this browser only; a server-side draft if people ask.
import { toast } from 'sonner'

/** The key of a document's draft: its project, the files it opened with, or a new one. */
export const draftKey = (prefix: string, p: { project: string; ids: string[] }) =>
  `${prefix}${p.project || p.ids.join(',') || 'new'}`

function readDraft<T>(key: string, parse: (text: string) => T | null): { at: number; doc: T } | null {
  try {
    const v = JSON.parse(localStorage.getItem(key) ?? 'null') as { at: number; design?: unknown; doc?: unknown } | null
    // `design`: drafts kept before the video editor shared this.
    const doc = v && parse(JSON.stringify(v.doc ?? v.design))

    return v && doc ? { at: v.at, doc } : null
  } catch {
    return null
  }
}

export function writeDraft(key: string, doc: unknown) {
  try {
    localStorage.setItem(key, JSON.stringify({ at: Date.now(), doc }))
  } catch {
    // Full or blocked storage: no draft, the editor works on.
  }
}

export function dropDraft(key: string) {
  try {
    localStorage.removeItem(key)
  } catch {
    // Nothing to drop.
  }
}

/** Offers the document's draft back, if there is one: Restore puts it in the editor (one undo away). */
export function offerDraft<T>(key: string, parse: (text: string) => T | null, restore: (d: T) => void) {
  const d = readDraft(key, parse)

  if (!d) {
    return
  }

  toast(`Unsaved changes from ${new Date(d.at).toLocaleString()}`, {
    description: 'The editor kept them when it was closed.',
    duration: 30_000,
    action: { label: 'Restore', onClick: () => restore(d.doc) },
    cancel: { label: 'Discard', onClick: () => dropDraft(key) },
  })
}
