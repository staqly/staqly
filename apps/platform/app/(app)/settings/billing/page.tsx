import type { Metadata } from 'next'

import { BillingPage } from '@/components/settings/billing-page'

export const metadata: Metadata = { title: 'Billing' }

export default function Page() {
  return <BillingPage />
}
