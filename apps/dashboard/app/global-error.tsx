'use client'

import { GeistMono } from 'geist/font/mono'
import { GeistSans } from 'geist/font/sans'

import { ErrorScreen } from '@/components/common/error-screen'

import './globals.css'

// Replaces the root layout when it fails, so it brings its own document.
export default function GlobalError({ error, retry }: { error: Error & { digest?: string }; retry: () => void }) {
  return (
    <html lang="en" className={`${GeistSans.variable} ${GeistMono.variable} h-full antialiased`}>
      <body className="min-h-full">
        <title>Something went wrong · staqly</title>
        <ErrorScreen error={error} retry={retry} />
      </body>
    </html>
  )
}
