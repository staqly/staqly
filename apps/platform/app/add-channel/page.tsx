import type { Metadata } from 'next'

import { AddChannelPage } from '@/components/connect/add-channel-page'

// The token is in the fragment; no-referrer keeps the page's address out of requests to other sites too.
export const metadata: Metadata = { title: 'Connect a channel', referrer: 'no-referrer', robots: { index: false } }

export default function Page() {
  return <AddChannelPage />
}
