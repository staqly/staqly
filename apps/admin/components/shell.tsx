'use client'

import { Logo } from '@staqly/brand/logo'
import { useQuery } from '@tanstack/react-query'
import {
  CreditCardIcon,
  LayoutDashboardIcon,
  LogOutIcon,
  MenuIcon,
  MoonIcon,
  ScrollTextIcon,
  ShieldCheckIcon,
  SunIcon,
  TicketPercentIcon,
  UsersIcon,
  BuildingIcon,
} from 'lucide-react'
import { useTheme } from 'next-themes'
import Link from 'next/link'
import { usePathname } from 'next/navigation'
import { useState, type ReactNode } from 'react'

import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import { Sheet, SheetContent, SheetDescription, SheetTitle, SheetTrigger } from '@/components/ui/sheet'
import { Skeleton } from '@/components/ui/skeleton'
import { adminApi } from '@/lib/api'
import { cn } from '@/lib/utils'

const NAV = [
  { href: '/', label: 'Overview', icon: LayoutDashboardIcon },
  { href: '/workspaces', label: 'Workspaces', icon: BuildingIcon },
  { href: '/users', label: 'Users', icon: UsersIcon },
  { href: '/payments', label: 'Payments', icon: CreditCardIcon },
  { href: '/promo-codes', label: 'Promo codes', icon: TicketPercentIcon },
  { href: '/audit', label: 'Audit log', icon: ScrollTextIcon },
  { href: '/admins', label: 'Admins', icon: ShieldCheckIcon },
]

function active(path: string, href: string) {
  const p = path.replace(/\/$/, '') || '/'

  return href === '/' ? p === '/' : p === href || p.startsWith(`${href}/`)
}

async function signOut() {
  await adminApi.send('POST', '/v1/admin/auth/sign-out').catch(() => {})
  window.location.assign('/sign-in')
}

function Sidebar({ email, onNavigate }: { email: string; onNavigate?: () => void }) {
  const path = usePathname()

  return (
    <div className="flex h-full flex-col bg-sidebar text-sidebar-foreground">
      <div className="flex h-14 items-center gap-2 border-b border-sidebar-border px-4">
        <Logo colorway="dark" wordmarkClassName="h-4 text-white" />
        <Badge className="bg-sidebar-primary text-sidebar-primary-foreground">Admin</Badge>
      </div>
      <nav aria-label="Main" className="flex-1 space-y-1 p-3">
        {NAV.map(({ href, label, icon: Icon }) => (
          <Link
            key={href}
            href={href}
            onClick={onNavigate}
            aria-current={active(path, href) ? 'page' : undefined}
            className={cn(
              'flex items-center gap-3 rounded-lg px-3 py-2 text-sm font-medium outline-offset-2 transition-colors hover:bg-sidebar-accent hover:text-sidebar-accent-foreground',
              active(path, href) && 'bg-sidebar-accent text-sidebar-accent-foreground',
            )}
          >
            <Icon className="size-4" aria-hidden />
            {label}
          </Link>
        ))}
      </nav>
      <div className="space-y-2 border-t border-sidebar-border p-3">
        <p className="truncate px-3 text-xs text-sidebar-foreground/70" title={email}>
          {email}
        </p>
        <button
          type="button"
          onClick={signOut}
          className="flex w-full items-center gap-3 rounded-lg px-3 py-2 text-sm font-medium outline-offset-2 transition-colors hover:bg-sidebar-accent hover:text-sidebar-accent-foreground"
        >
          <LogOutIcon className="size-4" aria-hidden />
          Sign out
        </button>
      </div>
    </div>
  )
}

function ThemeToggle() {
  const { resolvedTheme, setTheme } = useTheme()

  return (
    <Button
      variant="ghost"
      size="icon"
      aria-label="Toggle theme"
      onClick={() => setTheme(resolvedTheme === 'dark' ? 'light' : 'dark')}
    >
      <SunIcon className="hidden dark:block" />
      <MoonIcon className="dark:hidden" />
    </Button>
  )
}

export function Shell({ title, actions, children }: { title: string; actions?: ReactNode; children: ReactNode }) {
  const [open, setOpen] = useState(false)
  const me = useQuery({
    queryKey: ['admin-me'],
    queryFn: () => adminApi.get<{ email: string }>('/v1/admin/me'),
  })

  if (!me.data) {
    return (
      <div className="p-6" aria-busy="true">
        <Skeleton className="mb-6 h-8 w-48" />
        <Skeleton className="h-64 w-full" />
      </div>
    )
  }

  return (
    <div className="min-h-svh">
      <aside className="fixed inset-y-0 left-0 hidden w-60 md:block">
        <Sidebar email={me.data.email} />
      </aside>
      <div className="md:pl-60">
        <header className="sticky top-0 z-30 flex h-14 items-center gap-2 border-b bg-background/85 px-4 backdrop-blur md:px-8">
          <Sheet open={open} onOpenChange={setOpen}>
            <SheetTrigger asChild>
              <Button variant="ghost" size="icon" className="md:hidden" aria-label="Open menu">
                <MenuIcon />
              </Button>
            </SheetTrigger>
            <SheetContent side="left" closeOnOutsideClick showCloseButton={false} className="w-64 gap-0 p-0">
              <SheetTitle className="sr-only">Menu</SheetTitle>
              <SheetDescription className="sr-only">Admin navigation</SheetDescription>
              <Sidebar email={me.data.email} onNavigate={() => setOpen(false)} />
            </SheetContent>
          </Sheet>
          <h1 className="min-w-0 flex-1 truncate text-lg font-semibold tracking-tight">{title}</h1>
          {actions}
          <ThemeToggle />
        </header>
        <main className="mx-auto w-full max-w-6xl space-y-6 px-4 py-6 md:px-8">{children}</main>
      </div>
    </div>
  )
}
