import type { Metadata } from 'next'
import { Suspense } from 'react'

import { SchedulePage } from '@/components/schedule/schedule-page'

export const metadata: Metadata = { title: 'Schedule' }

export default function Page() {
  return (
    <Suspense>
      <SchedulePage />
    </Suspense>
  )
}
