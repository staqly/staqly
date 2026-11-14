import type { Metadata } from 'next'
import { Suspense } from 'react'

import { DesignPage } from '@/components/design/design-page'
import { AuthGate } from '@/components/shell/auth-gate'

export const metadata: Metadata = { title: 'Design' }

export default function Page() {
  return (
    <AuthGate>
      <Suspense>
        <DesignPage />
      </Suspense>
    </AuthGate>
  )
}
