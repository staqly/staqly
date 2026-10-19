import type { Metadata } from 'next'
import { Suspense } from 'react'

import { ChannelsPage } from '@/components/channels/channels-page'

export const metadata: Metadata = { title: 'Channels' }

export default function Page() {
  return (
    <Suspense>
      <ChannelsPage />
    </Suspense>
  )
}
