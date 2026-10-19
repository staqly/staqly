import type { Metadata } from 'next'

import { BackupsPage } from '@/components/settings/backup-card'

export const metadata: Metadata = { title: 'Backups' }

export default function Page() {
  return <BackupsPage />
}
