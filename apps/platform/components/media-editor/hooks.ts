'use client'

import { useEditorServices } from '@opengpex/editor/core/context'
// Typed access to the staqly commands from a plugin component, and the editor's URL parameters.
import { useMemo } from 'react'

import type { ExportPayload, OpenMediaPayload } from './commands'
import * as P from './protocols'

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
      exportImage: async (p: ExportPayload = {}) => {
        for (let attempt = 0; ; attempt++) {
          try {
            // oxlint-disable-next-line no-await-in-loop
            return await run<Blob>(P.CMD_EXPORT, p)
          } catch (e) {
            if (attempt >= 20 || !/before init/i.test(String((e as Error)?.message))) {
              throw e
            }

            // oxlint-disable-next-line no-await-in-loop
            await new Promise((r) => setTimeout(r, 500))
          }
        }
      },
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
  const back = q.get('return') ?? ''

  return {
    ids: (q.get('ids') ?? '')
      .split(',')
      .map((s) => s.trim())
      .filter(Boolean)
      .slice(0, 10),
    project: q.get('project') ?? '',
    back: back.startsWith('/') && !back.startsWith('//') ? back : '/media',
    fromComposer: q.get('from') === 'composer',
  }
}
