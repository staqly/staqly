'use client'

import { useRouter } from 'next/navigation'
import { useEffect } from 'react'

/**
 * /settings/billing (old links, bookmarks, Stripe return URLs made before the merge) now lives in Plan & usage:
 * go there with the same query, so `?checkout=success` and `?credits=success` still show their toasts.
 */
export function BillingRedirect() {
  const router = useRouter()

  useEffect(() => {
    router.replace(`/settings/plan${window.location.search}`)
  }, [router])

  return null
}
