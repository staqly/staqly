'use client'

import { ArrowLeft, MonitorX } from 'lucide-react'
import { useEffect, useState, type ReactNode } from 'react'

import { Button } from '@/components/ui/button'
import { Empty, EmptyContent, EmptyDescription, EmptyHeader, EmptyMedia, EmptyTitle } from '@/components/ui/empty'
import { Spinner } from '@/components/ui/spinner'

type Gpu = { requestAdapter: () => Promise<unknown> }

/** WebGPU with an adapter (the editor draws with it). */
export async function hasWebGpu(): Promise<boolean> {
  const gpu = (navigator as Navigator & { gpu?: Gpu }).gpu

  if (!gpu) {
    return false
  }

  try {
    return !!(await gpu.requestAdapter())
  } catch {
    return false
  }
}

export function EditorLoading() {
  return (
    <div role="status" className="flex h-full items-center justify-center gap-2 text-sm text-muted-foreground">
      <Spinner /> Opening the editor...
    </div>
  )
}

/**
 * The editor needs WebGPU and a cross-origin isolated page (its codecs share memory between threads). The
 * server sends the isolation headers on /media/edit; a page reached by in-app navigation has none, so it
 * reloads once. Without WebGPU (older Safari, Firefox on some systems) it says so instead of failing mid-start.
 */
export function WebGpuGate({ children }: { children: ReactNode }) {
  const [ok, setOk] = useState<boolean | null>(null)

  useEffect(() => {
    if (!window.crossOriginIsolated && !sessionStorage.getItem('editor-reloaded')) {
      sessionStorage.setItem('editor-reloaded', '1')
      window.location.reload()

      return
    }

    sessionStorage.removeItem('editor-reloaded')
    void hasWebGpu().then(setOk)
  }, [])

  if (ok === null) {
    return <EditorLoading />
  }

  if (ok) {
    return children
  }

  return (
    <Empty className="h-full">
      <EmptyHeader>
        <EmptyMedia variant="icon">
          <MonitorX />
        </EmptyMedia>
        <EmptyTitle>This browser cannot run the editor</EmptyTitle>
        <EmptyDescription>
          The editor needs WebGPU: a current Chrome or Edge, or Safari 26 or later, has it. Your files are unchanged.
        </EmptyDescription>
      </EmptyHeader>
      <EmptyContent>
        <Button variant="outline" asChild>
          {/* oxlint-disable-next-line nextjs/no-html-link-for-pages -- a full page load leaves the isolated editor page */}
          <a href="/media">
            <ArrowLeft aria-hidden /> Back to Media
          </a>
        </Button>
      </EmptyContent>
    </Empty>
  )
}
