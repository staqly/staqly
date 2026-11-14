'use client'

import { usePathname, useRouter } from 'next/navigation'
import { useEffect, type ReactNode } from 'react'

import { useBilling, useRole, useSession, useUsage } from '@/lib/queries'

/** A local install has no welcome: /welcome opens the calendar there, whose empty state guides instead. */
export function NoWelcomeLocally({ children }: { children: ReactNode }) {
  const session = useSession()
  const router = useRouter()
  const local = session.data?.mode === 'local'

  useEffect(() => {
    if (local) {
      router.replace('/schedule')
    }
  }, [local, router])

  return local ? null : children
}

/**
 * Sends someone who hasn't seen the welcome yet to /welcome, once they land on the dashboard's home (the
 * calendar); the calendar doesn't render meanwhile, so it never flashes. Deep links (a post, Settings, an
 * invitation, a preview) open as asked.
 */
export function WelcomeGate({ children }: { children: ReactNode }) {
  const session = useSession()
  const pathname = usePathname()
  const router = useRouter()
  const pending = session.data?.mode !== 'local' && session.data?.onboarded === false && pathname === '/schedule'

  useEffect(() => {
    if (pending) {
      router.replace('/welcome')
    }
  }, [pending, router])

  return pending ? null : children
}

/**
 * Cloud with billing: the account owner of a workspace that never had a plan goes to the welcome (its last step is
 * the plan) before any of the app renders, so the sidebar never flashes. Billing is asked only for a locked
 * workspace; others wait just for the usage, which the app reads anyway.
 */
export function NoPlanGate({ children }: { children: ReactNode }) {
  const session = useSession()
  const { isOwner } = useRole()
  const router = useRouter()
  const cloudOwner = session.data?.mode === 'cloud' && isOwner
  const usage = useUsage()
  const locked = cloudOwner && !!usage.data?.suspended
  const billing = useBilling({ enabled: locked })
  const toWelcome = locked && !!billing.data?.enabled && !billing.data.subscription_status
  const waiting = cloudOwner && (usage.isPending || (locked && billing.isPending))

  useEffect(() => {
    if (toWelcome) {
      router.replace('/welcome')
    }
  }, [toWelcome, router])

  return waiting || toWelcome ? null : children
}
