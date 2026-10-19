import type { Metadata } from 'next'

import { AiSettings } from '@/components/integrations/ai-settings'

export const metadata: Metadata = { title: 'AI' }

export default function Page() {
  return <AiSettings />
}
