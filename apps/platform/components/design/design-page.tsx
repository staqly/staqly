'use client'

import { Loader2 } from 'lucide-react'
import dynamic from 'next/dynamic'
import { useSearchParams } from 'next/navigation'

// The design editor (Konva, its fonts) loads only here, never with other pages. Link to it with a plain <a>, not
// next/link: its CSP (the CDN, wasm and workers background removal needs) comes with a full page load only.
const DesignEditor = dynamic(() => import('./editor'), {
  ssr: false,
  loading: () => (
    <div className="fixed inset-0 flex items-center justify-center bg-background">
      <Loader2 className="animate-spin text-muted-foreground" aria-label="Loading the editor" />
    </div>
  ),
})

export function DesignPage() {
  // The router's query, not window.location: on a client-side navigation the page renders before the address
  // bar changes, and the editor opened the previous page's query (an empty design).
  return <DesignEditor search={useSearchParams().toString()} />
}
