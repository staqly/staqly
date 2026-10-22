'use client'

import { useEditorServices } from '@opengpex/editor/core/context'
// Typed access to the staqly commands from a plugin component, and the editor's URL parameters.
import { useMemo } from 'react'

import { safeNextPath } from '@/lib/safe'

import type { ExportPayload, OpenMediaPayload } from './commands'
import * as P from './protocols'

/** Runs `fn` again while the editor answers "before init", up to 20 times, half a second apart. */
async function retrying<R>(fn: () => Promise<R>): Promise<R> {
  for (let attempt = 0; ; attempt++) {
    try {
      return await fn()
    } catch (e) {
      if (attempt >= 20 || !/before init/i.test(String((e as Error)?.message))) {
        throw e
      }

      await new Promise((r) => setTimeout(r, 500))
    }
  }
}

/** The staqly commands (registered by the bridge plugin), callable from anywhere inside the editor. */
export function useStaqlyCommands() {
  const { actions } = useEditorServices()

  return useMemo(() => {
    const run = <R>(id: string, payload?: unknown) => {
      const out = actions.executeCommand<unknown, Promise<R> | undefined>(`${P.BRIDGE_UID}.${id}`, payload)

      return out ?? Promise.reject(new Error('The editor is still starting'))
    }

    return {
      openMedia: (p: OpenMediaPayload) => run<void>(P.CMD_OPEN_MEDIA, p),
      addImageLayer: (p: { blob: Blob; name: string }) => run<void>(P.CMD_ADD_IMAGE_LAYER, p),
      addVersion: (p: { blob: Blob; name: string }) => run<string>(P.CMD_ADD_VERSION, p),
      // The renderer starts a moment after a picture opens. A command sees the editor as it was when it
      // started, so each try is a new command (up to 10 s).
      exportImage: (p: ExportPayload = {}) => retrying(() => run<Blob>(P.CMD_EXPORT, p)),
      pack: () => run<Blob>(P.CMD_PACK),
      unpack: (file: Blob) => run<unknown>(P.CMD_UNPACK, file),
    }
  }, [actions])
}

export interface EditorParams {
  ids: string[]
  project: string
  /** Where Back and "Use in a post" go: a dashboard path only (never another site). */
  back: string
  /** Opened from a post's attachment (a new tab): saving updates that post. */
  fromComposer: boolean
}

export function readParams(search: string): EditorParams {
  const q = new URLSearchParams(search)

  return {
    ids: (q.get('ids') ?? '')
      .split(',')
      .map((s) => s.trim())
      .filter(Boolean)
      .slice(0, 10),
    project: q.get('project') ?? '',
    back: safeNextPath(q.get('return'), 'http://localhost', '/media'),
    fromComposer: q.get('from') === 'composer',
  }
}
