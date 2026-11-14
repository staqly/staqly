import type { Metadata } from 'next'
import { Suspense } from 'react'

import { AuthGate } from '@/components/shell/auth-gate'
import { TimelinePage } from '@/components/timeline/timeline-page'

export const metadata: Metadata = { title: 'Sound' }

export default function Page() {
  return (
    <AuthGate>
      <Suspense>
        <TimelinePage kind="audio" />
      </Suspense>
    </AuthGate>
  )
}
