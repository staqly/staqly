'use client'

import { usePathname, useRouter } from 'next/navigation'
import { useEffect, type ReactNode } from 'react'

import { useRole } from '@/lib/queries'

export const GUEST_SETTINGS = '/settings/plan'

/**
 * A guest link gives time-limited access to someone's account: of Settings it sees Plan & usage only (read-only),
 * never the account or the workspace settings. Other sections send it there and don't render meanwhile.
 */
export function GuestSettingsGate({ children }: { children: ReactNode }) {
  const { guest } = useRole()
  const pathname = usePathname()
  const router = useRouter()
  const away = guest && pathname !== GUEST_SETTINGS

  useEffect(() => {
    if (away) {
      router.replace(GUEST_SETTINGS)
    }
  }, [away, router])

  return away ? null : children
}
