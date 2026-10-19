import type { Metadata } from 'next'
import { Suspense } from 'react'

import { IntegrationsPage } from '@/components/integrations/pages'

export const metadata: Metadata = { title: 'Integrations' }

export default function Page() {
  return (
    <Suspense>
      <IntegrationsPage />
    </Suspense>
  )
}
