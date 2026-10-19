'use client'

import { usePathname, useRouter } from 'next/navigation'
import { useEffect, type ReactNode } from 'react'

import { useSession } from '@/lib/queries'

/**
 * Sends someone who hasn't seen the welcome yet to /welcome, once they land on the dashboard's home (the
 * calendar); the calendar doesn't render meanwhile, so it never flashes. Deep links (a post, Settings, an
 * invitation, a preview) open as asked.
 */
export function WelcomeGate({ children }: { children: ReactNode }) {
  const session = useSession()
  const pathname = usePathname()
  const router = useRouter()
  const pending = session.data?.onboarded === false && pathname === '/schedule'

  useEffect(() => {
    if (pending) {
      router.replace('/welcome')
    }
  }, [pending, router])

  return pending ? null : children
}
