import type { Metadata } from 'next'
import { Suspense } from 'react'

import { PaymentFailedPage } from '@/components/plans/payment-failed-page'
import { AuthGate } from '@/components/shell/auth-gate'

export const metadata: Metadata = { title: 'Payment' }

export default function Page() {
  return (
    <AuthGate>
      <Suspense>
        <PaymentFailedPage />
      </Suspense>
    </AuthGate>
  )
}
