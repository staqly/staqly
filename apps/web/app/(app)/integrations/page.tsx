import type { Metadata } from 'next'

import { IntegrationsOverview } from '@/components/integrations/pages'

export const metadata: Metadata = { title: 'Integrations' }

export default function Page() {
  return <IntegrationsOverview />
}
