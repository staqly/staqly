'use client'

import { LogoMark } from '@staqly/brand/logo'
import { useMutation, useQueryClient } from '@tanstack/react-query'
import {
  Activity,
  BellRing,
  Blocks,
  CalendarClock,
  ChevronsUpDown,
  Gauge,
  PenSquare,
  KeyRound,
  LogIn,
  LogOut,
  Menu,
  Plus,
  QrCode,
  Share2,
  Search,
  Settings,
  Webhook,
} from 'lucide-react'
import dynamic from 'next/dynamic'
import Link from 'next/link'
import { usePathname, useRouter } from 'next/navigation'
import { createContext, useContext, useEffect, useMemo, useState, type ReactNode } from 'react'
import { toast } from 'sonner'

import { PlanBadge } from '@/components/plans/plan-badge'
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuGroup,
  DropdownMenuItem,
  DropdownMenuLabel,
  DropdownMenuSeparator,
  DropdownMenuTrigger,
} from '@/components/ui/dropdown-menu'
import { Kbd } from '@/components/ui/kbd'
import {
  Sidebar,
  SidebarContent,
  SidebarFooter,
  SidebarGroup,
  SidebarGroupContent,
  SidebarGroupLabel,
  SidebarHeader,
  SidebarInset,
  SidebarMenu,
  SidebarMenuButton,
  SidebarMenuItem,
  SidebarProvider,
  SidebarRail,
  useSidebar,
} from '@/components/ui/sidebar'
import { useLazyDialog } from '@/hooks/use-lazy-dialog'
import { useIsMobile } from '@/hooks/use-mobile'
import { api, setCsrfToken, UNLIMITED } from '@/lib/api'
import { toastApiError } from '@/lib/notify'
import { useMode, useSession, useUsage } from '@/lib/queries'
import { rememberApiKey } from '@/lib/session-key'
import { meterTone, usageMeter } from '@/lib/usage'
import { cn } from '@/lib/utils'

import { LiveUpdates } from './live-updates'
import { NotificationsBell } from './notifications-bell'

interface ShellContextValue {
  openCreateCollection: () => void
  openCommandPalette: () => void
  openConnectDevice: () => void
}

const ShellContext = createContext<ShellContextValue>({
  openCreateCollection: () => {},
  openCommandPalette: () => {},
  openConnectDevice: () => {},
})

export function useShell() {
  return useContext(ShellContext)
}

const INTEGRATION_LINKS = [
  { href: '/integrations', label: 'Integrations' },
  { href: '/integrations?view=agents', label: 'Agents & activity' },
  { href: '/integrations/api-keys', label: 'API keys', cloudOnly: true },
  { href: '/integrations?tab=claude-code', label: 'Claude Code' },
  { href: '/integrations?tab=codex', label: 'Codex' },
  { href: '/integrations?tab=opencode', label: 'OpenCode' },
  { href: '/integrations?tab=n8n', label: 'n8n' },
  { href: '/integrations?tab=api', label: 'REST API & MCP' },
]

/** Integration links visible in the current mode (API keys exist only in cloud mode). */
export function useIntegrationLinks() {
  const { isLocal } = useMode()

  return INTEGRATION_LINKS.filter((l) => !(isLocal && 'cloudOnly' in l && l.cloudOnly))
}

export function useLogout(opts?: { message?: string }) {
  const qc = useQueryClient()
  const router = useRouter()

  return useMutation({
    mutationFn: () => api.logout(),
    onSuccess: () => {
      setCsrfToken(null)
      rememberApiKey(null)
      qc.clear()
      router.replace('/login')
      toast.success(opts?.message ?? 'Signed out')
    },
    onError: (e) => toastApiError(e),
  })
}

// ---- responsive helpers ----

/** True on wide desktops (and while prerendering); tablets (768–1279px) get a collapsed icon rail by default. */
function useIsWide() {
  return !useIsMobile(1280)
}

// ---- workspace menu ----

function WorkspaceMenu({ compact = false }: { compact?: boolean }) {
  const session = useSession()
  const { isLocal } = useMode()
  const router = useRouter()
  const { openConnectDevice } = useShell()
  const { setOpenMobile } = useSidebar()
  const logout = useLogout()
  const switchWorkspace = useLogout({ message: 'Signed out — sign in to another workspace' })
  const ws = session.data?.workspace
  const plan = isLocal ? 'unlocked' : ws?.plan
  const name = ws?.name ?? 'staqly'
  const go = (href: string) => {
    setOpenMobile(false)
    router.push(href)
  }

  const trigger = compact ? (
    <button
      type="button"
      className="flex min-h-11 min-w-0 items-center gap-2 rounded-lg px-1.5 text-left outline-none focus-visible:ring-3 focus-visible:ring-ring/50"
      aria-label="Workspace menu"
      data-testid="workspace-menu-trigger"
    >
      <LogoMark className="size-7" />
      <span className="min-w-0 truncate text-[15px] font-semibold">{name}</span>
      {plan && <PlanBadge plan={plan} />}
      <ChevronsUpDown className="size-3.5 shrink-0 text-muted-foreground" />
    </button>
  ) : (
    <SidebarMenuButton
      size="lg"
      tooltip={name}
      className="data-[state=open]:bg-sidebar-accent pointer-coarse:group-data-[collapsible=icon]:p-1!"
      aria-label="Workspace menu"
      data-testid="workspace-menu-trigger"
    >
      <LogoMark className="size-8" />
      <div className="grid min-w-0 flex-1 text-left leading-tight">
        <span className="truncate text-sm font-semibold" data-testid="workspace-name">
          {name}
        </span>
        <span className="flex min-w-0 items-center gap-1.5 text-[11px] text-muted-foreground">
          {plan && <PlanBadge plan={plan} />}
          <span className="truncate">{isLocal ? 'Local install' : 'Cloud'}</span>
        </span>
      </div>
      <ChevronsUpDown className="ml-auto size-4 text-sidebar-foreground/50 group-data-[collapsible=icon]:hidden" />
    </SidebarMenuButton>
  )

  return (
    <DropdownMenu>
      <DropdownMenuTrigger asChild>{trigger}</DropdownMenuTrigger>
      <DropdownMenuContent
        align="start"
        className="w-[min(18rem,calc(100vw-1rem))] [&_[role=menuitem]]:pointer-coarse:min-h-11"
      >
        <DropdownMenuLabel className="flex items-center gap-2 py-2">
          <LogoMark className="size-6" />
          <div className="grid min-w-0 leading-tight">
            <span className="truncate text-sm font-semibold text-foreground" title={name}>
              {name}
            </span>
            <span className="text-[11px] font-normal text-muted-foreground">
              {isLocal ? 'Local install · no limits' : 'Cloud workspace'}
            </span>
          </div>
          {plan && <PlanBadge plan={plan} className="ml-auto" />}
        </DropdownMenuLabel>
        <DropdownMenuSeparator />
        <DropdownMenuGroup>
          <DropdownMenuItem onSelect={() => go('/settings/plan')}>
            <Gauge /> Plan & usage
          </DropdownMenuItem>
          <DropdownMenuItem
            onSelect={() => {
              setOpenMobile(false)
              openConnectDevice()
            }}
          >
            <QrCode /> Connect a device
          </DropdownMenuItem>
          <DropdownMenuItem onSelect={() => go('/settings')}>
            <Settings /> Settings
          </DropdownMenuItem>
        </DropdownMenuGroup>
        {!isLocal && (
          <>
            <DropdownMenuSeparator />
            <DropdownMenuGroup>
              <DropdownMenuItem onSelect={() => go('/setup')}>
                <Plus /> Create another workspace
              </DropdownMenuItem>
              <DropdownMenuItem onSelect={() => switchWorkspace.mutate()} disabled={switchWorkspace.isPending}>
                <LogIn /> Sign in to another workspace
              </DropdownMenuItem>
            </DropdownMenuGroup>
            <DropdownMenuSeparator />
            <DropdownMenuItem onSelect={() => logout.mutate()} disabled={logout.isPending}>
              <LogOut /> Log out
            </DropdownMenuItem>
          </>
        )}
      </DropdownMenuContent>
    </DropdownMenu>
  )
}

// ---- plan footer ----

function PlanFooterLink() {
  const usage = useUsage()
  const { isLocal } = useMode()
  const pathname = usePathname()
  const { setOpenMobile } = useSidebar()
  const plan = usage.data?.plan
  const unlocked = isLocal || plan?.id === 'unlocked'
  const max = plan?.limits.posts_per_month ?? UNLIMITED
  const used = usage.data?.usage.posts_per_month ?? 0
  const { level, pct } = usageMeter(used, max)

  return (
    <SidebarMenu>
      <SidebarMenuItem>
        <SidebarMenuButton
          asChild
          tooltip="Plan & usage"
          isActive={pathname === '/settings/plan'}
          className="h-auto min-h-8 py-1.5 in-data-[mobile=true]:h-auto in-data-[mobile=true]:min-h-12"
        >
          <Link href="/settings/plan" onClick={() => setOpenMobile(false)} data-testid="sidebar-plan-link">
            <Gauge className="text-sidebar-foreground/70" />
            <div className="grid min-w-0 flex-1 gap-1">
              <span className="flex items-center gap-1.5 text-xs">
                <span className="font-medium">Plan & usage</span>
                {(plan || isLocal) && <PlanBadge plan={unlocked ? 'unlocked' : plan?.id} className="ml-auto" />}
              </span>
              {unlocked ? (
                <span className="text-[11px] text-muted-foreground">No limits on this install</span>
              ) : usage.data ? (
                <>
                  <span className="block h-1 overflow-hidden rounded-full bg-sidebar-accent">
                    <span
                      className={cn('block h-full rounded-full', meterTone(level).fill)}
                      style={{ width: `${level === 'unlimited' ? 0 : Math.max(pct, used ? 3 : 0)}%` }}
                    />
                  </span>
                  <span className="text-[11px] text-muted-foreground tabular-nums">
                    {used.toLocaleString('en-US')} / {level === 'unlimited' ? '∞' : max.toLocaleString('en-US')} posts
                    this month
                  </span>
                </>
              ) : null}
            </div>
          </Link>
        </SidebarMenuButton>
      </SidebarMenuItem>
    </SidebarMenu>
  )
}

// ---- sidebar ----

function AppSidebar() {
  const pathname = usePathname()
  const { openCommandPalette } = useShell()
  const { setOpenMobile } = useSidebar()
  const { isLocal } = useMode()
  const close = () => setOpenMobile(false)
  const isActive = (href: string) => pathname === href || pathname.startsWith(href + '/')

  return (
    <Sidebar collapsible="icon" variant="sidebar">
      <SidebarHeader className="gap-2 in-data-[mobile=true]:pt-[max(0.5rem,env(safe-area-inset-top))]">
        <SidebarMenu>
          <SidebarMenuItem>
            <WorkspaceMenu />
          </SidebarMenuItem>
        </SidebarMenu>
        <button
          type="button"
          onClick={() => {
            close()
            openCommandPalette()
          }}
          className="flex h-8 items-center gap-2 rounded-md border bg-background/60 px-2 text-xs text-muted-foreground transition-colors group-data-[collapsible=icon]:hidden hover:bg-background in-data-[mobile=true]:h-11 in-data-[mobile=true]:text-sm"
        >
          <Search className="size-3.5" />
          <span className="flex-1 text-left">Search or jump to...</span>
          <Kbd className="in-data-[mobile=true]:hidden">⌘K</Kbd>
        </button>
      </SidebarHeader>

      {/* A landmark, so screen readers can jump to the navigation (and skip it). */}
      <SidebarContent role="navigation" aria-label="Main">
        <SidebarGroup>
          <SidebarGroupContent>
            <SidebarMenu>
              <SidebarMenuItem>
                <SidebarMenuButton asChild isActive={isActive('/schedule')} tooltip="Schedule">
                  <Link href="/schedule" onClick={close}>
                    <CalendarClock /> <span>Schedule</span>
                  </Link>
                </SidebarMenuButton>
              </SidebarMenuItem>
              <SidebarMenuItem>
                <SidebarMenuButton asChild isActive={isActive('/posts')} tooltip="Posts">
                  <Link href="/posts" onClick={close}>
                    <PenSquare /> <span>Posts</span>
                  </Link>
                </SidebarMenuButton>
              </SidebarMenuItem>
              <SidebarMenuItem>
                <SidebarMenuButton asChild isActive={isActive('/channels')} tooltip="Channels">
                  <Link href="/channels" onClick={close}>
                    <Share2 /> <span>Channels</span>
                  </Link>
                </SidebarMenuButton>
              </SidebarMenuItem>
              <SidebarMenuItem>
                <SidebarMenuButton asChild isActive={isActive('/activity')} tooltip="Activity">
                  <Link href="/activity" onClick={close}>
                    <Activity /> <span>Activity</span>
                  </Link>
                </SidebarMenuButton>
              </SidebarMenuItem>
            </SidebarMenu>
          </SidebarGroupContent>
        </SidebarGroup>

        <SidebarGroup className="mt-auto">
          <SidebarGroupLabel>Automation</SidebarGroupLabel>
          <SidebarGroupContent>
            <SidebarMenu>
              <SidebarMenuItem>
                <SidebarMenuButton asChild isActive={isActive('/alerts')} tooltip="Alerts">
                  <Link href="/alerts" onClick={close}>
                    <BellRing /> <span>Alerts</span>
                  </Link>
                </SidebarMenuButton>
              </SidebarMenuItem>
              <SidebarMenuItem>
                <SidebarMenuButton asChild isActive={isActive('/webhooks')} tooltip="Webhooks">
                  <Link href="/webhooks" onClick={close}>
                    <Webhook /> <span>Webhooks</span>
                  </Link>
                </SidebarMenuButton>
              </SidebarMenuItem>
            </SidebarMenu>
          </SidebarGroupContent>
        </SidebarGroup>

        <SidebarGroup>
          <SidebarGroupLabel>Configure</SidebarGroupLabel>
          <SidebarGroupContent>
            <SidebarMenu>
              <SidebarMenuItem>
                <SidebarMenuButton asChild isActive={pathname === '/integrations'} tooltip="Integrations">
                  <Link href="/integrations" onClick={close}>
                    <Blocks /> <span>Integrations</span>
                  </Link>
                </SidebarMenuButton>
              </SidebarMenuItem>
              {!isLocal && (
                <SidebarMenuItem>
                  <SidebarMenuButton asChild isActive={isActive('/integrations/api-keys')} tooltip="API keys">
                    <Link href="/integrations/api-keys" onClick={close}>
                      <KeyRound /> <span>API keys</span>
                    </Link>
                  </SidebarMenuButton>
                </SidebarMenuItem>
              )}
              <SidebarMenuItem>
                <SidebarMenuButton
                  asChild
                  isActive={
                    pathname === '/settings' ||
                    pathname === '/settings/data' ||
                    pathname === '/settings/backups' ||
                    pathname === '/settings/system'
                  }
                  tooltip="Settings"
                >
                  <Link href="/settings" onClick={close}>
                    <Settings /> <span>Settings</span>
                  </Link>
                </SidebarMenuButton>
              </SidebarMenuItem>
            </SidebarMenu>
          </SidebarGroupContent>
        </SidebarGroup>
      </SidebarContent>

      <SidebarFooter className="in-data-[mobile=true]:pb-[max(0.5rem,env(safe-area-inset-bottom))]">
        {/* Theme lives in Settings and ⌘K; collapsing in the sidebar edge (rail) and ⌘B. The bell (what happened
            while nobody was looking) sits here on desktop; phones have it in the top bar. */}
        <SidebarMenu>
          <NotificationsBell sidebar />
        </SidebarMenu>
        <PlanFooterLink />
      </SidebarFooter>
      <SidebarRail />
    </Sidebar>
  )
}

// ---- mobile chrome ----

function MobileHeader() {
  const { openCommandPalette } = useShell()

  return (
    <header
      className="sticky top-0 z-30 border-b bg-background/85 pt-[env(safe-area-inset-top)] [@media(max-height:500px)]:static backdrop-blur-xl supports-[backdrop-filter]:bg-background/70 md:hidden"
      data-testid="mobile-header"
    >
      <div className="flex h-(--mobile-header-h) items-center gap-1 px-2">
        <div className="flex min-w-0 flex-1 items-center">
          <WorkspaceMenu compact />
        </div>
        <NotificationsBell className="size-11 shrink-0 rounded-lg [&_svg]:size-5" />
        <button
          type="button"
          onClick={openCommandPalette}
          aria-label="Search"
          className="flex size-11 shrink-0 items-center justify-center rounded-lg text-muted-foreground hover:bg-muted hover:text-foreground"
        >
          <Search className="size-5" />
        </button>
      </div>
    </header>
  )
}

function MobileTabBar() {
  const pathname = usePathname()
  const { setOpenMobile, openMobile } = useSidebar()
  const tabs: { href: string; label: string; Icon: typeof CalendarClock; badge?: number; prefixes?: string[] }[] = [
    { href: '/schedule', label: 'Schedule', Icon: CalendarClock },
    { href: '/posts', label: 'Posts', Icon: PenSquare, prefixes: ['/posts'] },
    { href: '/channels', label: 'Channels', Icon: Share2 },
    { href: '/activity', label: 'Activity', Icon: Activity },
  ]

  return (
    <nav
      aria-label="Primary"
      data-testid="mobile-tabbar"
      className="fixed inset-x-0 bottom-0 z-40 border-t bg-background/90 pb-[env(safe-area-inset-bottom)] backdrop-blur-xl supports-[backdrop-filter]:bg-background/75 md:hidden"
    >
      <ul className="mx-auto grid h-(--mobile-tabbar-h) max-w-lg grid-cols-5 px-1">
        {tabs.map(({ href, label, Icon, badge, prefixes }) => {
          const active = (prefixes ?? [href]).some((p) => pathname === p || pathname.startsWith(p + '/'))

          return (
            <li key={href} className="flex">
              <Link
                href={href}
                aria-current={active ? 'page' : undefined}
                className={cn(
                  'relative flex flex-1 flex-col items-center justify-center gap-0.5 rounded-xl text-[10.5px] font-medium transition-colors',
                  active ? 'text-foreground' : 'text-muted-foreground',
                )}
              >
                <span
                  className={cn(
                    'relative flex h-7 w-12 items-center justify-center rounded-full transition-colors',
                    active && 'bg-brand-soft text-brand',
                  )}
                >
                  <Icon className="size-[19px]" strokeWidth={active ? 2.3 : 2} />
                  {!!badge && badge > 0 && (
                    <span className="absolute -top-1 right-0.5 min-w-4 rounded-full bg-brand px-1 text-center text-[10px] leading-4 font-semibold text-brand-foreground tabular-nums ring-2 ring-background">
                      {badge > 99 ? '99+' : badge}
                    </span>
                  )}
                </span>
                {label}
              </Link>
            </li>
          )
        })}
        <li className="flex">
          <button
            type="button"
            onClick={() => setOpenMobile(true)}
            aria-expanded={openMobile}
            className="flex flex-1 flex-col items-center justify-center gap-0.5 rounded-xl text-[10.5px] font-medium text-muted-foreground"
          >
            <span className="flex h-7 w-12 items-center justify-center rounded-full">
              <Menu className="size-[19px]" />
            </span>
            More
          </button>
        </li>
      </ul>
    </nav>
  )
}

// Loaded on first open, not with every page: cmdk, the QR code library and zod + react-hook-form stay out of
// the JavaScript every route ships.
/**
 * Links to /posts/<id> don't prefetch (`prefetch={false}`): every id is the same
 * exported page (`_`), so prefetching each link in view repeated two requests per link (a HEAD and the route
 * tree), ~50 on /collections and two per row of the activity feed. The pages' code is fetched once here
 * instead, when the browser is idle, so the first click on any of them is still instant.
 */
function IdRoutePrefetch() {
  const router = useRouter()

  useEffect(() => {
    const run = () => {
      router.prefetch('/posts/_')
    }

    if (typeof window.requestIdleCallback !== 'function') {
      const t = setTimeout(run, 2000)

      return () => clearTimeout(t)
    }

    const id = window.requestIdleCallback(run, { timeout: 5000 })

    return () => window.cancelIdleCallback(id)
  }, [router])

  return null
}

const CommandPalette = dynamic(() => import('./command-palette').then((m) => m.CommandPalette), { ssr: false })
const CreateCollectionDialog = dynamic(
  () => import('@/components/collections/create-collection-dialog').then((m) => m.CreateCollectionDialog),
  { ssr: false },
)
const ConnectDeviceDialog = dynamic(
  () => import('@/components/devices/connect-device-dialog').then((m) => m.ConnectDeviceDialog),
  { ssr: false },
)

export function AppShell({ children }: { children: ReactNode }) {
  const create = useLazyDialog()
  const command = useLazyDialog()
  const connect = useLazyDialog()
  const setCreateOpen = create.setOpen
  const setCommandOpen = command.setOpen
  const setConnectOpen = connect.setOpen
  const toggleCommand = command.toggle

  // ⌘K / Ctrl+K lives here, not in the palette, so the palette's code loads only on first use.
  useEffect(() => {
    const onKey = (e: KeyboardEvent) => {
      if (e.key.toLowerCase() === 'k' && (e.metaKey || e.ctrlKey)) {
        e.preventDefault()
        toggleCommand()
      }
    }

    window.addEventListener('keydown', onKey)

    return () => window.removeEventListener('keydown', onKey)
  }, [toggleCommand])

  const wide = useIsWide()
  // The user's explicit toggle wins until the breakpoint changes again.
  const [override, setOverride] = useState<{ wide: boolean; open: boolean } | null>(null)
  const sidebarOpen = override && override.wide === wide ? override.open : wide
  const ctx = useMemo<ShellContextValue>(
    () => ({
      openCreateCollection: () => setCreateOpen(true),
      openCommandPalette: () => setCommandOpen(true),
      openConnectDevice: () => setConnectOpen(true),
    }),
    [setCreateOpen, setCommandOpen, setConnectOpen],
  )

  return (
    <ShellContext.Provider value={ctx}>
      <LiveUpdates />
      <IdRoutePrefetch />
      <SidebarProvider open={sidebarOpen} onOpenChange={(open) => setOverride({ wide, open })}>
        {/* The first Tab on every page: keyboard users skip the sidebar's two dozen links. */}
        <a
          href="#main"
          className="sr-only z-50 rounded-md bg-background px-3 py-2 text-sm font-medium shadow-md outline-ring focus:not-sr-only focus:fixed focus:top-2 focus:left-2 focus:outline-2"
        >
          Skip to content
        </a>
        <AppSidebar />
        <SidebarInset id="main" tabIndex={-1} className="min-w-0 pb-(--mobile-chrome-bottom) outline-none md:pb-0">
          <MobileHeader />
          {children}
        </SidebarInset>
        <MobileTabBar />
      </SidebarProvider>
      {command.used && <CommandPalette open={command.open} onOpenChange={setCommandOpen} />}
      {create.used && <CreateCollectionDialog open={create.open} onOpenChange={setCreateOpen} />}
      {connect.used && <ConnectDeviceDialog open={connect.open} onOpenChange={setConnectOpen} />}
    </ShellContext.Provider>
  )
}
