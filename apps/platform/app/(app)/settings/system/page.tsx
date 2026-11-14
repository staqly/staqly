import type { Metadata } from 'next'

import { SystemPage } from '@/components/settings/system-page'

export const metadata: Metadata = { title: 'System' }

export default function Page() {
  return <SystemPage />
}
