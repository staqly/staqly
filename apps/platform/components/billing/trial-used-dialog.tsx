'use client'

import dynamic from 'next/dynamic'
import { useState } from 'react'

import { useBilling, useMode, useRole } from '@/lib/queries'
import { closeTrialUsed, useTrialUsed, type TrialUsedKind } from '@/lib/trial-used'

const TrialUsedContent = dynamic(() => import('./trial-used-content'), { ssr: false })

// Shown once per page load: the dashboard is one page, so moving around keeps it closed; a reload asks again.
let seenKind = ''

/** Tests start each case as a fresh page load. */
export function forgetTrialUsedSeen() {
  seenKind = ''
}

/**
 * Why the free trial is paused (the workspace connected accounts of an earlier unpaid trial) or gone (its card was
 * used for one). Opens when a connect or publish is refused for it (openTrialUsed) and, for owners and admins,
 * once a page load while the billing state says so.
 */
export function TrialUsedDialog() {
  const { isLocal } = useMode()
  const { role, canManagePeople } = useRole()
  const billing = useBilling({ enabled: !isLocal && canManagePeople })
  const asked = useTrialUsed()
  const [seen, setSeen] = useState(seenKind)
  const fromBilling: TrialUsedKind | null = billing.data?.trial_review
    ? 'paused'
    : billing.data?.trial_revoked
      ? 'card'
      : null
  const kind = asked ?? (fromBilling && seen !== fromBilling ? fromBilling : null)

  if (isLocal || !kind) {
    return null
  }

  const close = () => {
    seenKind = kind
    setSeen(kind)
    closeTrialUsed()
  }

  return (
    <TrialUsedContent
      kind={kind}
      canPay={role === 'owner'}
      charged={billing.data?.subscription_status === 'active'}
      onClose={close}
    />
  )
}
