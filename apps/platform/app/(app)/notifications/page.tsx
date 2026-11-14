import type { Metadata } from 'next'

import { NotificationsPage } from '@/components/notifications/notifications-page'

export const metadata: Metadata = { title: 'Notifications' }

export default function Page() {
  return <NotificationsPage />
}
