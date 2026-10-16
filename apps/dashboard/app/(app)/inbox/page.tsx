import type { Metadata } from 'next'
import { Suspense } from 'react'

import { Inbox } from '@/components/inbox/inbox'

export const metadata: Metadata = { title: 'Inbox' }

export default function InboxPage() {
  return (
    <Suspense>
      <Inbox />
    </Suspense>
  )
}
