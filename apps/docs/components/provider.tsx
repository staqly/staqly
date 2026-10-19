'use client'

import { RootProvider } from 'fumadocs-ui/provider/next'
import { type ReactNode } from 'react'

import SearchDialog from '@/components/search'

export function Provider({ children }: { children: ReactNode }) {
  // The docs always use the dark theme (no light mode, no system preference).
  return (
    <RootProvider search={{ SearchDialog }} theme={{ forcedTheme: 'dark', defaultTheme: 'dark', enableSystem: false }}>
      {children}
    </RootProvider>
  )
}
