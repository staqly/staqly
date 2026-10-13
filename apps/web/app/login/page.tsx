import type { Metadata } from 'next'
import { Suspense } from 'react'

import { RecoverForm } from '@/components/auth/recover-form'

export const metadata: Metadata = { title: 'Recover access' }

export default function LoginPage() {
  return (
    <Suspense>
      <RecoverForm />
    </Suspense>
  )
}
