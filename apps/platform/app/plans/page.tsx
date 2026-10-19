import type { Metadata } from 'next'

import { PlansPage } from '@/components/plans/plans-page'
import { AuthGate } from '@/components/shell/auth-gate'

export const metadata: Metadata = { title: 'Plans' }

export default function Page() {
  return (
    <AuthGate>
      <PlansPage />
    </AuthGate>
  )
}
