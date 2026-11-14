import type { Metadata } from 'next'
import { Suspense } from 'react'

import { OAuthCallback } from '@/components/channels/oauth-callback'

export const metadata: Metadata = { title: 'Connecting channel' }

// One prerendered shell for every provider; the Go web server maps /channels/callback/* onto it.
export function generateStaticParams() {
  return [{ provider: '_' }]
}

export default function Page() {
  return (
    <Suspense>
      <OAuthCallback />
    </Suspense>
  )
}
