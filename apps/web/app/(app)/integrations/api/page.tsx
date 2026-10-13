import type { Metadata } from 'next'

import { ApiDocsPage } from '@/components/integrations/pages'

export const metadata: Metadata = { title: 'REST API' }

export default function Page() {
  return <ApiDocsPage />
}
