import type { Metadata } from 'next'

import { DataPrivacyPage } from '@/components/settings/data-privacy-page'

export const metadata: Metadata = { title: 'Data & privacy' }

export default function Page() {
  return <DataPrivacyPage />
}
