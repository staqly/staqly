'use client'

import { Loader2 } from 'lucide-react'
import dynamic from 'next/dynamic'

// The design editor (Konva, its fonts) loads only here, never with other pages.
const DesignEditor = dynamic(() => import('./editor'), {
  ssr: false,
  loading: () => (
    <div className="fixed inset-0 flex items-center justify-center bg-background">
      <Loader2 className="animate-spin text-muted-foreground" aria-label="Loading the editor" />
    </div>
  ),
})

export function DesignPage() {
  return <DesignEditor />
}
