'use client'

import { Loader2 } from 'lucide-react'
import dynamic from 'next/dynamic'
import { useSearchParams } from 'next/navigation'

// The video and sound editor (its timeline, WebCodecs export) loads only here. Link to it with a plain <a>, a full
// page load, like the design editor.
const TimelineEditor = dynamic(() => import('./editor'), {
  ssr: false,
  loading: () => (
    <div className="fixed inset-0 flex items-center justify-center bg-background">
      <Loader2 className="animate-spin text-muted-foreground" aria-label="Loading the editor" />
    </div>
  ),
})

export function TimelinePage({ kind }: { kind: 'video' | 'audio' }) {
  return <TimelineEditor kind={kind} search={useSearchParams().toString()} />
}
