'use client'

import Link from 'next/link'
import { usePathname } from 'next/navigation'

import { useBackupAccess } from '@/lib/queries'
import { cn } from '@/lib/utils'

const LINKS = [
  { href: '/settings', label: 'General' },
  { href: '/settings/plan', label: 'Plan & usage' },
  { href: '/settings/data', label: 'Data & privacy' },
  { href: '/settings/backups', label: 'Backups' },
  { href: '/settings/system', label: 'System' },
]
const LINKS_WITHOUT_BACKUPS = LINKS.filter((l) => l.href !== '/settings/backups')

export function SettingsNav({ className }: { className?: string }) {
  const pathname = usePathname()
  // Backups are a paid feature in cloud mode: no tab on Free (or until the plan is known).
  const backups = useBackupAccess()
  const links = backups === 'local' || backups === 'paid' ? LINKS : LINKS_WITHOUT_BACKUPS

  return (
    <nav
      aria-label="Settings sections"
      className={cn('scrollbar-none -mx-4 flex gap-1 overflow-x-auto border-b px-4 sm:mx-0 sm:px-0', className)}
    >
      {links.map((l) => {
        const active = pathname === l.href

        return (
          <Link
            key={l.href}
            href={l.href}
            aria-current={active ? 'page' : undefined}
            className={cn(
              'relative -mb-px inline-flex h-10 shrink-0 items-center px-3 whitespace-nowrap text-sm font-medium transition-colors pointer-coarse:h-11',
              active
                ? 'text-foreground after:absolute after:inset-x-2 after:bottom-0 after:h-0.5 after:rounded-full after:bg-brand'
                : 'text-muted-foreground hover:text-foreground',
            )}
          >
            {l.label}
          </Link>
        )
      })}
    </nav>
  )
}
