import type { Metadata } from 'next'

import { ClaudeCodePage } from '@/components/integrations/pages'

export const metadata: Metadata = { title: 'Claude Code' }

export default function Page() {
  return <ClaudeCodePage />
}
