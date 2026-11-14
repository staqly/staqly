import type { Metadata } from 'next'
import { Suspense } from 'react'

import { MembersPage } from '@/components/settings/members-page'

export const metadata: Metadata = { title: 'Members' }

export default function Page() {
  return (
    <Suspense>
      <MembersPage />
    </Suspense>
  )
}
