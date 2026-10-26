// The open document (a design, a video or a sound) with undo and redo. Changes of one control in a row (dragging a slider, typing a colour)
// coalesce into one undo step.
import { produce } from 'immer'
import { useCallback, useState } from 'react'

import type { Design } from './doc'

interface History<T = Design> {
  past: T[]
  present: T
  future: T[]
  key: string
  at: number
}

const LIMIT = 100
const COALESCE_MS = 800

export function step<T = Design>(s: History<T>, fn: (d: T) => void | T, coalesce = '', now = Date.now()): History<T> {
  const next = produce(s.present, fn as (d: T) => T) as T

  if (next === s.present) {
    return s
  }

  const merge = coalesce !== '' && coalesce === s.key && now - s.at < COALESCE_MS

  return {
    past: merge ? s.past : [...s.past.slice(-LIMIT + 1), s.present],
    present: next,
    future: [],
    key: coalesce,
    at: now,
  }
}

export function useHistory<T>(initial: T) {
  const [h, setH] = useState<History<T>>({ past: [], present: initial, future: [], key: '', at: 0 })

  const update = useCallback((fn: (d: T) => void | T, coalesce?: string) => setH((s) => step(s, fn, coalesce)), [])
  const undo = useCallback(
    () =>
      setH((s) =>
        s.past.length
          ? { past: s.past.slice(0, -1), present: s.past.at(-1)!, future: [s.present, ...s.future], key: '', at: 0 }
          : s,
      ),
    [],
  )
  const redo = useCallback(
    () =>
      setH((s) =>
        s.future.length
          ? { past: [...s.past, s.present], present: s.future[0]!, future: s.future.slice(1), key: '', at: 0 }
          : s,
      ),
    [],
  )
  /** A new document (opened, or a template applied): undo goes back to what was there before. */
  const replace = useCallback((d: T) => setH((s) => step(s, () => d)), [])

  return {
    doc: h.present,
    update,
    undo,
    redo,
    replace,
    canUndo: h.past.length > 0,
    canRedo: h.future.length > 0,
  }
}

export function useDesignStore(initial: Design) {
  const { doc, ...rest } = useHistory(initial)

  return { design: doc, ...rest }
}

export type DesignStore = ReturnType<typeof useDesignStore>
