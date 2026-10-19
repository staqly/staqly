import type { Metadata } from 'next'

import { BillingRedirect } from '@/components/settings/billing-redirect'

export const metadata: Metadata = { title: 'Plan & usage' }

/** Billing moved into Plan & usage; this route only forwards old links there. */
export default function Page() {
  return <BillingRedirect />
}
