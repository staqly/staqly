'use client'

import { RootProvider } from 'fumadocs-ui/provider/next'
import dynamic from 'next/dynamic'
import { type ReactNode } from 'react'

// The search dialog and its Orama client (~25 KB gzipped) load after the page has hydrated, not in its first bundle.
// It mounts right away (not on first open): the search button opens it through a handle on the mounted dialog.
const SearchDialog = dynamic(() => import('@/components/search'), { ssr: false })

export function Provider({ children }: { children: ReactNode }) {
  // The docs always use the dark theme (no light mode, no system preference).
  return (
    <RootProvider search={{ SearchDialog }} theme={{ forcedTheme: 'dark', defaultTheme: 'dark', enableSystem: false }}>
      {children}
    </RootProvider>
  )
}
