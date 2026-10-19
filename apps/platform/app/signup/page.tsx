import type { Metadata } from 'next'
import { Suspense } from 'react'

import { SignIn } from '@/components/auth/sign-in'

export const metadata: Metadata = { title: 'Create your account' }

export default function SignupPage() {
  return (
    <Suspense>
      <SignIn signup />
    </Suspense>
  )
}
