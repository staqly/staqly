import type { Metadata } from 'next'

import { OpenCodePage } from '@/components/integrations/pages'

export const metadata: Metadata = { title: 'OpenCode' }

export default function Page() {
  return <OpenCodePage />
}
