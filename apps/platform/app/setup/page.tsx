import type { Metadata } from 'next'

import { SetupRedirect } from '@/components/auth/setup-redirect'

export const metadata: Metadata = { title: 'Create your account' }

export default function SetupPage() {
  return <SetupRedirect />
}
