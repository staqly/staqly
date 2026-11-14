import type { Metadata } from 'next'
import { Suspense } from 'react'

import { NoWelcomeLocally } from '@/components/onboarding/welcome-redirect'
import { WelcomeWizard } from '@/components/onboarding/welcome-wizard'
import { AuthGate } from '@/components/shell/auth-gate'

export const metadata: Metadata = { title: 'Welcome' }

export default function Page() {
  return (
    <AuthGate>
      <NoWelcomeLocally>
        <Suspense>
          <WelcomeWizard />
        </Suspense>
      </NoWelcomeLocally>
    </AuthGate>
  )
}
