'use client'

import type { EditorPlugin } from '@opengpex/editor/core/types'

import { STAQLY_COMMANDS } from './commands'
import { BRIDGE_ID, PLUGIN_AUTHOR } from './protocols'

/**
 * staqly's plugin of the media editor: the commands the editor bar and panels use to open files of the media
 * library, add layers and versions, export and pack projects. They run inside OpenGPEX's command bus (history,
 * undo). Registered in lib/opengpex/core/plugin/registry.ts.
 */
export const plugin: EditorPlugin = {
  manifest: {
    id: BRIDGE_ID,
    displayName: 'staqly',
    version: '1.0.0',
    description: 'Opens and saves files of the staqly media library.',
    category: 'overlays',
    author: PLUGIN_AUTHOR,
    requirements: { coreVersion: '>=2.0.0', auth: 'none' },
  },
  slot: 'HIDDEN',
  component: () => null,
  commands: Object.values(STAQLY_COMMANDS),
}
