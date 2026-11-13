'use client'

import dynamic from 'next/dynamic'
import { useMemo, useState } from 'react'

import { useBilling, useMode, useRole, useSession } from '@/lib/queries'
import { closeTrialUsed, useTrialUsed, type TrialUsedKind } from '@/lib/trial-used'

const TrialUsedContent = dynamic(() => import('./trial-used-content'), { ssr: false })

// Shown once per page load: the dashboard is one page, so moving around keeps it closed; a reload asks again.
let seenKind = ''

const GOT_IT_KEY = 'staqly.trial-used.card.'

/** "Got it" on the card dialog, remembered per workspace: this browser doesn't show it again. */
function readGotIt(workspace: string): boolean {
  try {
    return localStorage.getItem(GOT_IT_KEY + workspace) === '1'
  } catch {
    return false
  }
}

function writeGotIt(workspace: string) {
  try {
    localStorage.setItem(GOT_IT_KEY + workspace, '1')
  } catch {
    // Private windows and blocked storage: the dialog simply returns with the next page load.
  }
}

/** Tests start each case as a fresh page load. */
export function forgetTrialUsedSeen() {
  seenKind = ''
}

/**
 * Why the free trial is paused (the workspace connected accounts of an earlier unpaid trial) or gone (its card was
 * used for one). Opens when a connect or publish is refused for it (openTrialUsed) and, for owners and admins,
 * once a page load while the billing state says so. A card that was charged leaves nothing to do, and the state
 * stays for good: after "Got it" this browser is not told again (a charge that failed keeps returning).
 */
export function TrialUsedDialog() {
  const { isLocal } = useMode()
  const { role, canManagePeople } = useRole()
  const billing = useBilling({ enabled: !isLocal && canManagePeople })
  const asked = useTrialUsed()
  const [seen, setSeen] = useState(seenKind)
  const workspace = useSession().data?.workspace?.id ?? ''
  const charged = billing.data?.subscription_status === 'active'
  const gotIt = useMemo(() => readGotIt(workspace), [workspace])
  const fromBilling: TrialUsedKind | null = billing.data?.trial_review
    ? 'paused'
    : billing.data?.trial_revoked && !(charged && gotIt)
      ? 'card'
      : null
  const kind = asked ?? (fromBilling && seen !== fromBilling ? fromBilling : null)

  if (isLocal || !kind) {
    return null
  }

  const close = () => {
    if (kind === 'card' && charged) {
      writeGotIt(workspace)
    }

    seenKind = kind
    setSeen(kind)
    closeTrialUsed()
  }

  return <TrialUsedContent kind={kind} canPay={role === 'owner'} charged={charged} onClose={close} />
}
