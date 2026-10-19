import type { Metadata } from 'next'

import { IntegrationsRedirect } from '@/components/integrations/pages'

export const metadata: Metadata = { title: 'Agents' }

// Agents moved into Integrations.
export default function Page() {
  return <IntegrationsRedirect view="agents" />
}
