'use client'

import dynamic from 'next/dynamic'

import { EditorLoading, WebGpuGate } from './webgpu-gate'

// The editor and everything it brings (codecs, workers, its styles) load only here, never with other pages.
const EditorShell = dynamic(() => import('./editor-shell'), { ssr: false, loading: () => <EditorLoading /> })

export function EditorPage() {
  return (
    <div className="sq-editor fixed inset-0 overflow-hidden bg-background">
      <WebGpuGate>
        <EditorShell />
      </WebGpuGate>
    </div>
  )
}
