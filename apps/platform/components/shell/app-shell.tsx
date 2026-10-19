'use client'

import { LogoMark } from '@staqly/brand/logo'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import {
  Activity,
  Bell,
  BellRing,
  Blocks,
  CalendarClock,
  Check,
  ChevronsUpDown,
  CreditCard,
  Gauge,
  Images,
  LogOut,
  Menu,
  PenSquare,
  Plus,
  QrCode,
  Rocket,
  Search,
  Settings,
  Share2,
  Sparkles,
  UserRound,
  Users,
  Webhook,
} from 'lucide-react'
import dynamic from 'next/dynamic'
import Link from 'next/link'
import { usePathname, useRouter } from 'next/navigation'
import { createContext, useContext, useEffect, useMemo, useState, type ReactNode } from 'react'
import { toast } from 'sonner'

import { PlanBadge } from '@/components/plans/plan-badge'
import { NewWorkspaceDialog } from '@/components/settings/new-workspace-dialog'
import { UserAvatar } from '@/components/settings/user-avatar'
import { Button } from '@/components/ui/button'
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
import { api, setCsrfToken } from '@/lib/api'
import { toastApiError } from '@/lib/notify'
import { LIVE_SUBSCRIPTION } from '@/lib/plans'
import { qk, useBilling, useMode, usePlans, useRole, useSession, useSwitchWorkspace, useUsage } from '@/lib/queries'
import { rememberApiKey } from '@/lib/session-key'
import { cn } from '@/lib/utils'
import { useXPostsWarnings } from '@/lib/x-posts'

import { LiveUpdates } from './live-updates'
import { NotificationsBell, NotificationsPopover, useNotifications } from './notifications-bell'

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

/** Inside the notifications list (beside the user menu). */
const inNotes = (t: EventTarget | null) => !!(t as Element | null)?.closest?.('[data-notifications]')

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
  const { accountsEnabled, accountOwner, canManagePeople } = useRole()
  const openWorkspace = useSwitchWorkspace()
  const [creating, setCreating] = useState(false)
  const others = session.data?.workspaces ?? []
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
      className="flex min-h-11 min-w-0 items-center gap-2 rounded-lg px-1.5 text-left outline-none select-none focus-visible:ring-3 focus-visible:ring-ring/50"
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
      className="select-none data-[state=open]:bg-sidebar-accent pointer-coarse:group-data-[collapsible=icon]:p-1!"
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
            {isLocal && (
              <span className="text-[11px] font-normal text-muted-foreground">Community Edition · no plan limits</span>
            )}
          </div>
          {plan && <PlanBadge plan={plan} className="ml-auto" />}
        </DropdownMenuLabel>
        <DropdownMenuSeparator />
        <DropdownMenuGroup>
          <DropdownMenuItem onSelect={() => go('/settings/plan')}>
            <Gauge /> Plan & usage
          </DropdownMenuItem>
          {!accountsEnabled && (
            <DropdownMenuItem
              onSelect={() => {
                setOpenMobile(false)
                openConnectDevice()
              }}
            >
              <QrCode /> Connect a device
            </DropdownMenuItem>
          )}
          {accountsEnabled && canManagePeople && (
            <DropdownMenuItem onSelect={() => go('/settings/members')}>
              <Users /> Members
            </DropdownMenuItem>
          )}
          <DropdownMenuItem onSelect={() => go('/settings')}>
            <Settings /> Settings
          </DropdownMenuItem>
        </DropdownMenuGroup>
        {accountsEnabled && (
          <>
            <DropdownMenuSeparator />
            <DropdownMenuGroup aria-label="Workspaces">
              <DropdownMenuLabel className="text-xs font-normal text-muted-foreground">Workspaces</DropdownMenuLabel>
              {others.map((w) => (
                <DropdownMenuItem
                  key={w.id}
                  disabled={openWorkspace.isPending}
                  onSelect={() => !w.current && openWorkspace.mutate(w.id)}
                  data-testid="workspace-switch-item"
                >
                  <span className="min-w-0 flex-1 truncate">{w.name}</span>
                  {w.current && <Check className="ml-auto" aria-label="Open now" />}
                </DropdownMenuItem>
              ))}
              {accountOwner && (
                <DropdownMenuItem onSelect={() => setCreating(true)}>
                  <Plus /> New workspace
                </DropdownMenuItem>
              )}
            </DropdownMenuGroup>
          </>
        )}
        {/* A session without a person (an API key on a development server): it can only end. New workspaces and
            switching belong to people in an account (above). */}
        {!isLocal && !accountsEnabled && (
          <>
            <DropdownMenuSeparator />
            <DropdownMenuItem onSelect={() => logout.mutate()} disabled={logout.isPending}>
              <LogOut /> Log out
            </DropdownMenuItem>
          </>
        )}
      </DropdownMenuContent>
      {accountOwner && creating && <NewWorkspaceDialog open={creating} onOpenChange={setCreating} />}
    </DropdownMenu>
  )
}

// ---- signed-in person ----

/**
 * The person at the bottom of the sidebar: their name, address and picture in the cloud, the computer's
 * user name in a local install (one person, signed in with the access token).
 */
function UserMenu() {
  const session = useSession()
  const { isLocal } = useMode()
  const { accountsEnabled, role, canManagePeople } = useRole()
  const router = useRouter()
  const { setOpenMobile } = useSidebar()
  const logout = useLogout()
  const user = session.data?.user
  // Cloud, for owners and admins: Billing, and the loud row. In the trial or after a plan ran out: Choose a plan.
  // Paying: Upgrade to the next plan up while there is one (none on Business).
  const manageBilling = !isLocal && canManagePeople
  const usage = useUsage()
  const plans = usePlans({ enabled: manageBilling })
  const billing = useBilling({ enabled: manageBilling })
  const paying = LIVE_SUBSCRIPTION.includes(billing.data?.subscription_status ?? '')
  const current = usage.data?.plan
  const nextPlan = (plans.data ?? [])
    .filter((p) => (p.price_month ?? 0) > (current?.price_month ?? 0))
    .toSorted((a, b) => (a.price_month ?? 0) - (b.price_month ?? 0))[0]
  const upgradeLabel =
    !manageBilling || !current || !billing.data
      ? undefined
      : !paying && (usage.data?.suspended || usage.data?.plan_expires_at)
        ? 'Choose a plan'
        : nextPlan && `Upgrade to ${nextPlan.name}`
  // Cloud accounts: the name set in Settings > Account wins over the sign-in identity's name.
  const account = useQuery({ queryKey: qk.account, queryFn: api.account, enabled: accountsEnabled })
  const name = account.data?.name || user?.name || (isLocal ? 'System' : 'Signed in')
  // People who sign in with an email code have the address as their name: show it once.
  const roleLabel = role ? role.charAt(0).toUpperCase() + role.slice(1) : ''
  const detail = (user?.email !== name && user?.email) || (isLocal ? 'This computer' : roleLabel)
  // A local install has no welcome: its empty pages guide instead.
  const showGettingStarted = !isLocal && session.data?.onboarded === false
  const notes = useNotifications()
  const [notesOpen, setNotesOpen] = useState(false)
  // The bell keeps the menu open and shows the list beside it; closing the menu closes the list too.
  const [menuOpen, setMenuOpen] = useState(false)
  const go = (href: string) => {
    setOpenMobile(false)
    router.push(href)
  }

  return (
    <SidebarMenu>
      <SidebarMenuItem>
        <DropdownMenu
          modal={false}
          open={menuOpen}
          onOpenChange={(o) => {
            setMenuOpen(o)

            if (!o) {
              setNotesOpen(false)
            }
          }}
        >
          <DropdownMenuTrigger asChild>
            <SidebarMenuButton
              size="lg"
              tooltip={name}
              aria-label={`Account menu, ${name}${notes.unread > 0 ? `, ${notes.unread} unread notifications` : ''}`}
              data-testid="user-menu-trigger"
              className="relative"
            >
              <UserAvatar name={name} src={user?.avatar_url} className="size-8 rounded-lg" logo />
              {notes.unread > 0 && (
                <span
                  aria-hidden
                  className="absolute top-1.5 left-8 size-2.5 rounded-full bg-brand ring-2 ring-sidebar"
                />
              )}
              <div className="grid min-w-0 flex-1 text-left leading-tight">
                <span className="truncate text-sm font-medium" data-testid="user-name">
                  {name}
                </span>
                <span className="truncate text-[11px] text-muted-foreground">{detail}</span>
              </div>
              <ChevronsUpDown className="ml-auto size-4 text-sidebar-foreground/50 group-data-[collapsible=icon]:hidden" />
            </SidebarMenuButton>
          </DropdownMenuTrigger>
          <DropdownMenuContent
            side="top"
            align="start"
            className="w-[min(16rem,calc(100vw-1rem))] [&_[role=menuitem]]:pointer-coarse:min-h-11"
            data-user-menu=""
            // Working in the list beside the menu does not close the menu.
            onInteractOutside={(e) => {
              if (inNotes(e.target)) {
                e.preventDefault()
              }
            }}
            onFocusOutside={(e) => {
              if (inNotes(e.target)) {
                e.preventDefault()
              }
            }}
          >
            {/* Who is signed in, with the bell beside them: it opens the list next to the menu, which stays open. */}
            <div className="flex items-center gap-2 pr-1">
              <DropdownMenuLabel className="grid min-w-0 flex-1 py-2 leading-tight">
                <span className="truncate text-sm font-semibold text-foreground">{name}</span>
                <span className="truncate text-xs font-normal text-muted-foreground">{detail}</span>
              </DropdownMenuLabel>
              <NotificationsPopover
                notes={notes}
                open={notesOpen}
                onOpenChange={setNotesOpen}
                side="right"
                align="start"
                anchor
                keepOpenWithin="[data-user-menu]"
              >
                <DropdownMenuItem
                  data-testid="notifications-bell"
                  aria-label={notes.label}
                  data-unread={notes.unread}
                  className="relative size-8 shrink-0 justify-center p-0 pointer-coarse:size-11"
                  aria-expanded={notesOpen}
                  onSelect={(e) => {
                    e.preventDefault()
                    setNotesOpen((o) => !o)
                  }}
                >
                  <Bell />
                  {notes.unread > 0 && (
                    <span
                      aria-hidden
                      className="absolute -top-0.5 -right-0.5 min-w-4 rounded-full bg-emerald-700 px-1 text-center text-[10px] leading-4 font-semibold text-white tabular-nums dark:bg-brand dark:text-brand-foreground"
                    >
                      {notes.count}
                    </span>
                  )}
                </DropdownMenuItem>
              </NotificationsPopover>
            </div>
            {(showGettingStarted || upgradeLabel) && <DropdownMenuSeparator />}
            {/* Until the welcome wizard is finished or skipped: first, outlined so it never reads as the plan row. */}
            {showGettingStarted && (
              <DropdownMenuItem
                data-testid="menu-getting-started"
                className="border border-dashed border-[color-mix(in_oklch,var(--brand)_50%,transparent)] font-medium [&_svg]:text-brand"
                onSelect={() => go('/welcome')}
              >
                <Rocket /> Getting started
                <span className="ml-auto text-xs font-normal text-muted-foreground">Continue</span>
              </DropdownMenuItem>
            )}
            {upgradeLabel && (
              <>
                <DropdownMenuItem
                  data-testid="menu-upgrade"
                  className="mt-1 bg-brand-soft font-medium text-brand-strong focus:bg-brand-soft focus:text-brand-strong focus:brightness-110 [&_svg]:text-brand-strong!"
                  onSelect={() => go('/plans')}
                >
                  <Sparkles /> {upgradeLabel}
                </DropdownMenuItem>
              </>
            )}
            <DropdownMenuSeparator />
            {manageBilling && (
              <DropdownMenuItem onSelect={() => go('/settings/plan')}>
                <CreditCard /> Billing
              </DropdownMenuItem>
            )}
            {accountsEnabled && (
              <DropdownMenuItem onSelect={() => go('/settings/account')}>
                <UserRound /> Account
              </DropdownMenuItem>
            )}
            <DropdownMenuItem onSelect={() => go('/settings')}>
              <Settings /> Settings
            </DropdownMenuItem>
            {/* Locally too: a browser signs in with the access token and can sign out again. */}
            <DropdownMenuSeparator />
            <DropdownMenuItem onSelect={() => logout.mutate()} disabled={logout.isPending}>
              <LogOut /> Sign out
            </DropdownMenuItem>
          </DropdownMenuContent>
        </DropdownMenu>
      </SidebarMenuItem>
    </SidebarMenu>
  )
}

// ---- sidebar ----

function AppSidebar() {
  const pathname = usePathname()
  const { openCommandPalette } = useShell()
  const { setOpenMobile } = useSidebar()
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
                <SidebarMenuButton asChild isActive={isActive('/channels')} tooltip="Channels">
                  <Link href="/channels" onClick={close}>
                    <Share2 /> <span>Channels</span>
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
                <SidebarMenuButton asChild isActive={isActive('/media')} tooltip="Media">
                  <Link href="/media" onClick={close}>
                    <Images /> <span>Media</span>
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
              <SidebarMenuItem>
                <SidebarMenuButton asChild isActive={isActive('/integrations')} tooltip="Integrations">
                  <Link href="/integrations" onClick={close}>
                    <Blocks /> <span>Integrations</span>
                  </Link>
                </SidebarMenuButton>
              </SidebarMenuItem>
            </SidebarMenu>
          </SidebarGroupContent>
        </SidebarGroup>
      </SidebarContent>

      <SidebarFooter className="in-data-[mobile=true]:pb-[max(0.5rem,env(safe-area-inset-bottom))]">
        {/* Theme lives in Settings and ⌘K; collapsing in the sidebar edge (rail) and ⌘B. Notifications open from
            the user menu on desktop; phones have the bell in the top bar. Plan & usage is in the workspace menu. */}
        <UserMenu />
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
  useXPostsWarnings()
  const create = useLazyDialog()
  const command = useLazyDialog()
  const connect = useLazyDialog()
  const setCreateOpen = create.setOpen
  const setCommandOpen = command.setOpen
  const setConnectOpen = connect.setOpen
  const toggleCommand = command.toggle

  // ⌘K / Ctrl+K lives here, not in the palette, so the palette's code loads only on first use. Not inside a rich
  // text editor: there it adds a link.
  useEffect(() => {
    const onKey = (e: KeyboardEvent) => {
      if (e.target instanceof HTMLElement && e.target.isContentEditable) {
        return
      }

      if (e.key.toLowerCase() === 'k' && (e.metaKey || e.ctrlKey)) {
        e.preventDefault()
        toggleCommand()
      }
    }

    window.addEventListener('keydown', onKey)

    return () => window.removeEventListener('keydown', onKey)
  }, [toggleCommand])

  const wide = useIsWide()
  // The phone top bar and tab bar exist only on phones: hidden by CSS alone, desktops would still run a second
  // workspace menu and notifications list. The shell renders after the session loads, never while hydrating
  // the static page, so this reads the real width on the first render (no flash on phones).
  const isMobile = useIsMobile()
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
          {isMobile && <MobileHeader />}
          <NoPlanBanner />
          {children}
        </SidebarInset>
        {isMobile && <MobileTabBar />}
      </SidebarProvider>
      {command.used && <CommandPalette open={command.open} onOpenChange={setCommandOpen} />}
      {create.used && <CreateCollectionDialog open={create.open} onOpenChange={setCreateOpen} />}
      {connect.used && <ConnectDeviceDialog open={connect.open} onOpenChange={setConnectOpen} />}
    </ShellContext.Provider>
  )
}

/**
 * Cloud with billing: a workspace without a live plan (never started, or ended) is read-only. A bar on every
 * page says so and leads to the plans: Starter's trial for a new workspace, any plan after one ended.
 */
function NoPlanBanner() {
  const { isLocal } = useMode()
  const { canManagePeople } = useRole()
  const usage = useUsage()
  const billing = useBilling({ enabled: !isLocal && canManagePeople })

  if (isLocal || !usage.data?.suspended) {
    return null
  }

  const neverStarted = !!billing.data?.enabled && !billing.data.subscription_status

  return (
    <div
      role="status"
      data-testid="no-plan-banner"
      className="flex flex-wrap items-center justify-between gap-3 border-b border-destructive/30 bg-destructive/5 px-4 py-2.5 text-sm sm:px-6"
    >
      <p className="min-w-0">
        <span className="font-medium">
          {neverStarted ? 'Start your 7-day free trial to use this workspace.' : 'This workspace is read-only.'}
        </span>{' '}
        <span className="text-muted-foreground">
          {neverStarted
            ? 'Starter: card required, nothing charged for 7 days, cancel anytime.'
            : 'You can read and export everything; choose a plan to keep working.'}
        </span>
      </p>
      {canManagePeople && (
        <Button asChild size="sm">
          <Link href="/plans">{neverStarted ? 'Start free trial' : 'Choose a plan'}</Link>
        </Button>
      )}
    </div>
  )
}
