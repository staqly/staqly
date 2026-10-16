import type { Metadata } from 'next'

import { WatchersPage } from '@/components/watchers/watchers-page'

export const metadata: Metadata = { title: 'Watchers' }

export default function Page() {
  return <WatchersPage />
}
