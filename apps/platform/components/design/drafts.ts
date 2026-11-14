// Unsaved work survives a closed tab or a crash: an editor keeps a draft in this browser (per document) and offers
// it back when the same document opens again. ponytail: this browser only; a server-side draft if people ask.
import { toast } from 'sonner'

import type { SessionInfo } from '@/lib/api'
import { formatDateTime } from '@/lib/format'

/** Whose drafts these are: the signed-in person and the workspace ('' until the session is known: no drafts then). */
export const draftScope = (s?: Pick<SessionInfo, 'user' | 'workspace'>) =>
  s ? `${s.user?.id ?? 'local'}:${s.workspace.id}` : ''

/**
 * The key of a document's draft: whose it is, then its project, the files it opened with, or a new one. '' without
 * a scope: nothing is kept or offered.
 */
export const draftKey = (prefix: string, scope: string, p: { project: string; ids: string[] }) =>
  scope ? `${prefix}${scope}:${p.project || p.ids.join(',') || 'new'}` : ''

// Drafts kept before 2026-11-12 named no person or workspace (no ":" after the prefix), so nobody can tell whose
// they are: they go.
function dropUnscopedDrafts() {
  try {
    for (const k of Object.keys(localStorage)) {
      if (/^staqly-(design|video|audio)-draft:[^:]*$/.test(k)) {
        localStorage.removeItem(k)
      }
    }
  } catch {
    // Blocked storage: nothing to drop.
  }
}

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
  if (!key) {
    return
  }

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
  dropUnscopedDrafts()

  const d = key ? readDraft(key, parse) : null

  if (!d) {
    return
  }

  toast.info(`Unsaved changes from ${formatDateTime(new Date(d.at).toJSON())}`, {
    description: 'The editor kept them when it was closed.',
    duration: 30_000,
    action: { label: 'Restore', onClick: () => restore(d.doc) },
    cancel: { label: 'Discard', onClick: () => dropDraft(key) },
  })
}
