'use client'

import { usePathname } from 'next/navigation'

import { PageTabs } from '@/components/common/page-tabs'
import { useMode, useRole } from '@/lib/queries'

const LINKS = [
  { href: '/settings', label: 'General' },
  { href: '/settings/plan', label: 'Plan & usage' },
  { href: '/settings/data', label: 'Data & privacy' },
  { href: '/settings/backups', label: 'Backups' },
  { href: '/settings/system', label: 'System' },
]

export function SettingsNav({ className }: { className?: string }) {
  const pathname = usePathname()
  const { accountsEnabled, canManagePeople } = useRole()
  const { isLocal } = useMode()
  // Cloud accounts: members for those who manage them; Account (sign-in methods, and the account itself for
  // its owner) for every signed-in person.
  const links = [
    ...LINKS.slice(0, 1),
    ...(accountsEnabled && canManagePeople ? [{ href: '/settings/members', label: 'Members' }] : []),
    ...(accountsEnabled ? [{ href: '/settings/account', label: 'Account' }] : []),
    ...LINKS.slice(1, 2),
    // Cloud: the subscription, cards and invoices, for those who manage the workspace.
    ...(!isLocal && canManagePeople ? [{ href: '/settings/billing', label: 'Billing' }] : []),
    ...LINKS.slice(2),
  ]

  return (
    <PageTabs
      label="Settings sections"
      className={className}
      tabs={links.map(({ href, label }) => ({ href, label, active: pathname === href }))}
    />
  )
}
