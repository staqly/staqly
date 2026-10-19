import type { Metadata } from 'next'

import { UnsubscribePage } from '@/components/notifications/unsubscribe-page'

// The token is in the fragment (never sent with the page request); no-referrer keeps the address private.
export const metadata: Metadata = { title: 'Unsubscribe', referrer: 'no-referrer', robots: { index: false } }

export default function Page() {
  return <UnsubscribePage />
}
