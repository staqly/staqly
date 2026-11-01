'use client'

import { usePathname } from 'next/navigation'

import { PageTabs } from '@/components/common/page-tabs'
import { useRole } from '@/lib/queries'

import { GUEST_SETTINGS } from './guest-settings-gate'

const LINKS = [
  { href: '/settings', label: 'General' },
  { href: '/settings/plan', label: 'Plan & usage' },
  { href: '/settings/data', label: 'Data & privacy' },
  { href: '/settings/backups', label: 'Backups' },
  { href: '/settings/system', label: 'System' },
]

export function SettingsNav({ className }: { className?: string }) {
  const pathname = usePathname()
  const { accountsEnabled, canManagePeople, guest } = useRole()
  // Backups are made by owners and admins (the admin scope); others don't see the section.
  const sections = LINKS.slice(1).filter((l) => canManagePeople || l.href !== '/settings/backups')
  // Cloud accounts: members for those who manage them; Account (sign-in methods, and the account itself for
  // its owner) for every signed-in person.
  const links = guest
    ? LINKS.filter((l) => l.href === GUEST_SETTINGS)
    : [
        ...LINKS.slice(0, 1),
        ...(accountsEnabled && canManagePeople ? [{ href: '/settings/members', label: 'Members' }] : []),
        ...(accountsEnabled ? [{ href: '/settings/account', label: 'Account' }] : []),
        // Plan & usage holds the billing too (cloud, for those who manage the workspace).
        ...sections,
      ]

  return (
    <PageTabs
      label="Settings sections"
      className={className}
      tabs={links.map(({ href, label }) => ({ href, label, active: pathname === href }))}
    />
  )
}
