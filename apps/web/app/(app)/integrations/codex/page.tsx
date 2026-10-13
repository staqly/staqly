import type { Metadata } from 'next'

import { CodexPage } from '@/components/integrations/pages'

export const metadata: Metadata = { title: 'Codex' }

export default function Page() {
  return <CodexPage />
}
