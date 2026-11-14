import type { Metadata } from 'next'
import { Suspense } from 'react'

import { AcceptInvitation } from '@/components/auth/accept-invitation'

export const metadata: Metadata = { title: 'Invitation', referrer: 'no-referrer' }

export default function InvitePage() {
  return (
    <Suspense>
      <AcceptInvitation />
    </Suspense>
  )
}
