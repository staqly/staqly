import type { Metadata } from 'next'
import { Suspense } from 'react'

import { AuthGate } from '@/components/shell/auth-gate'
import { TimelinePage } from '@/components/timeline/timeline-page'

export const metadata: Metadata = { title: 'Video' }

export default function Page() {
  return (
    <AuthGate>
      <Suspense>
        <TimelinePage kind="video" />
      </Suspense>
    </AuthGate>
  )
}
