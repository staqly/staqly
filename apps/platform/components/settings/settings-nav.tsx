'use client'

import { usePathname } from 'next/navigation'

import { PageTabs } from '@/components/common/page-tabs'

const LINKS = [
  { href: '/settings', label: 'General' },
  { href: '/settings/plan', label: 'Plan & usage' },
  { href: '/settings/data', label: 'Data & privacy' },
  { href: '/settings/backups', label: 'Backups' },
  { href: '/settings/system', label: 'System' },
]

export function SettingsNav({ className }: { className?: string }) {
  const pathname = usePathname()

  return (
    <PageTabs
      label="Settings sections"
      className={className}
      tabs={LINKS.map(({ href, label }) => ({ href, label, active: pathname === href }))}
    />
  )
}
