'use client'

import { useMutation, useQueryClient } from '@tanstack/react-query'
import {
  Activity,
  Bell,
  BellRing,
  Blocks,
  CalendarClock,
  ChevronsUpDown,
  Clock,
  CreditCard,
  Gauge,
  Images,
  Lock,
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
import { createContext, useContext, useEffect, useMemo, useState, useSyncExternalStore, type ReactNode } from 'react'
import { toast } from 'sonner'

import { daysLeft } from '@/components/billing/billing-format'
import { PlanBadge } from '@/components/plans/plan-badge'
import { NewWorkspaceDialog } from '@/components/settings/new-workspace-dialog'
import { UserAvatar } from '@/components/settings/user-avatar'
import { WorkspaceLogo, WorkspaceTile } from '@/components/shell/workspace-tile'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuGroup,
  DropdownMenuItem,
  DropdownMenuLabel,
  DropdownMenuRadioGroup,
  DropdownMenuRadioItem,
  DropdownMenuSeparator,
  DropdownMenuTrigger,
} from '@/components/ui/dropdown-menu'
import { Kbd, KbdGroup } from '@/components/ui/kbd'
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
  SidebarMenuBadge,
  SidebarMenuButton,
  SidebarMenuItem,
  SidebarProvider,
  SidebarRail,
  useSidebar,
} from '@/components/ui/sidebar'
import { useLazyDialog } from '@/hooks/use-lazy-dialog'
import { useIsMobile } from '@/hooks/use-mobile'
import { useAiStatus } from '@/lib/ai-status'
import { api, setCsrfToken, type MyWorkspace } from '@/lib/api'
import { pluralize } from '@/lib/format'
import { planName, toastApiError } from '@/lib/notify'
import { LIVE_SUBSCRIPTION } from '@/lib/plans'
import { useBilling, useMode, usePlans, useRole, useSession, useSwitchWorkspace, useUsage } from '@/lib/queries'
import { rememberApiKey } from '@/lib/session-key'
import { cn } from '@/lib/utils'
import { useXPostsWarnings } from '@/lib/x-posts'

import { LiveUpdates } from './live-updates'
import { NotificationsBell, NotificationsPopover, useNotifications } from './notifications-bell'

interface ShellContextValue {
  openCreateCollection: () => void
  openCommandPalette: () => void
  openConnectDevice: () => void
  /** The assistant: null when the server has no AI for it. */
  assistant: { open: boolean; toggle: () => void } | null
}

// Loaded when first opened: most pages never need the chat.
const AssistantPanel = dynamic(() => import('@/components/assistant/assistant-panel').then((m) => m.AssistantPanel), {
  ssr: false,
})

const ShellContext = createContext<ShellContextValue>({
  openCreateCollection: () => {},
  openCommandPalette: () => {},
  openConnectDevice: () => {},
  assistant: null,
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

const NO_WORKSPACES: MyWorkspace[] = []
// ponytail: past the first few, the menu keeps the server's order; recently opened first when people ask for it.
const MENU_WORKSPACES = 6

function WorkspaceMenu({ compact = false }: { compact?: boolean }) {
  const session = useSession()
  const { isLocal } = useMode()
  const router = useRouter()
  const { openConnectDevice } = useShell()
  const { setOpenMobile } = useSidebar()
  const logout = useLogout()
  const { accountsEnabled, isOwner } = useRole()
  const openWorkspace = useSwitchWorkspace()
  const [creating, setCreating] = useState(false)
  const [searching, setSearching] = useState(false)
  const others = session.data?.workspaces ?? NO_WORKSPACES
  // The menu lists a few (the open one first); the rest are a search away.
  const listed = useMemo(
    () =>
      others.length > MENU_WORKSPACES
        ? others.toSorted((a, b) => +b.current - +a.current).slice(0, MENU_WORKSPACES - 1)
        : others,
    [others],
  )
  const ws = session.data?.workspace
  const plan = isLocal ? 'unlocked' : ws?.plan
  const name = ws?.name ?? 'staqly'
  // Starts with what the trigger shows (the name, then the plan) so speech input can say what is on screen.
  const triggerLabel = `${name}${plan ? ` ${planName(plan)}` : ''}, workspace menu`
  const go = (href: string) => {
    setOpenMobile(false)
    router.push(href)
  }

  const trigger = compact ? (
    <button
      type="button"
      className="flex min-h-11 min-w-0 items-center gap-2 rounded-lg px-1.5 text-left outline-none select-none focus-visible:ring-3 focus-visible:ring-ring/50"
      aria-label={triggerLabel}
      data-testid="workspace-menu-trigger"
    >
      <WorkspaceLogo src={ws?.logo} className="size-7" />
      <span className="min-w-0 truncate text-[15px] font-semibold">{name}</span> {plan && <PlanBadge plan={plan} />}
      <ChevronsUpDown className="size-3.5 shrink-0 text-muted-foreground" />
    </button>
  ) : (
    <SidebarMenuButton
      size="lg"
      tooltip={name}
      // Collapsed, the button is the logo itself (the default icon padding would clip it away).
      className="select-none data-[state=open]:bg-sidebar-accent group-data-[collapsible=icon]:p-0!"
      aria-label={triggerLabel}
      data-testid="workspace-menu-trigger"
    >
      {/* Collapsed, only the logo shows: it keeps its size and the name and plan step aside. */}
      <WorkspaceLogo src={ws?.logo} className="size-8 shrink-0" />
      <div className="grid min-w-0 flex-1 text-left leading-tight group-data-[collapsible=icon]:hidden">
        <span className="truncate text-sm font-semibold" data-testid="workspace-name">
          {name}
        </span>{' '}
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
          <WorkspaceLogo src={ws?.logo} className="size-6" />
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
        {/* People in an account: this menu is the workspaces (switch, create); the workspace's plan, members and
            settings are in the person's menu. Without accounts there is one workspace: its pages are here. */}
        {accountsEnabled ? (
          <>
            <DropdownMenuLabel className="text-xs font-normal text-muted-foreground">
              Switch workspace
            </DropdownMenuLabel>
            <DropdownMenuRadioGroup
              aria-label="Switch workspace"
              value={ws?.id ?? ''}
              onValueChange={(id) => id !== ws?.id && openWorkspace.mutate(id)}
            >
              {listed.map((w) => (
                <DropdownMenuRadioItem key={w.id} value={w.id} disabled={openWorkspace.isPending}>
                  <WorkspaceTile name={w.name} />
                  <span className="min-w-0 flex-1 truncate" title={w.name}>
                    {w.name}
                  </span>
                </DropdownMenuRadioItem>
              ))}
            </DropdownMenuRadioGroup>
            {listed.length < others.length && (
              <DropdownMenuItem onSelect={() => setSearching(true)} className="text-muted-foreground">
                <Search /> All {others.length} workspaces...
              </DropdownMenuItem>
            )}
            {/* An action, not one of the workspaces: its own group under a line. */}
            {isOwner && (
              <>
                <DropdownMenuSeparator />
                <DropdownMenuItem onSelect={() => setCreating(true)}>
                  <Plus /> New workspace
                </DropdownMenuItem>
              </>
            )}
          </>
        ) : (
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
      {isOwner && creating && <NewWorkspaceDialog open={creating} onOpenChange={setCreating} />}
      {searching && (
        <WorkspaceSwitcherDialog
          open={searching}
          onOpenChange={setSearching}
          workspaces={others}
          pending={openWorkspace.isPending}
          onPick={openWorkspace.mutate}
        />
      )}
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
  const { setOpenMobile, state, isMobile } = useSidebar()
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
  const name = user?.name || (isLocal ? 'System' : 'Signed in')
  // People who sign in with an email code have the address as their name: show it once.
  const roleLabel = role.charAt(0).toUpperCase() + role.slice(1)
  const detail = (user?.email !== name && user?.email) || (isLocal ? 'This computer' : roleLabel)
  // A local install has no welcome: its empty pages guide instead.
  const showGettingStarted = !isLocal && session.data?.onboarded === false
  const go = (href: string) => {
    setOpenMobile(false)
    router.push(href)
  }

  return (
    <SidebarMenu>
      <SidebarMenuItem>
        <DropdownMenu modal={false}>
          <DropdownMenuTrigger asChild>
            <SidebarMenuButton
              size="lg"
              tooltip={name}
              // Named by what it shows (the name, then the address or role) so speech input can say it; the collapsed
              // rail shows only the picture, so there it is named outright.
              aria-label={state === 'collapsed' && !isMobile ? `${name}, account menu` : undefined}
              data-testid="user-menu-trigger"
            >
              <UserAvatar name={name} src={user?.avatar_url} className="size-8 rounded-lg" logo />
              <div className="grid min-w-0 flex-1 text-left leading-tight">
                <span className="truncate text-sm font-medium" data-testid="user-name">
                  {name}
                </span>{' '}
                <span className="truncate text-[11px] text-muted-foreground">{detail}</span>
              </div>
              <ChevronsUpDown className="ml-auto size-4 text-sidebar-foreground/50 group-data-[collapsible=icon]:hidden" />
            </SidebarMenuButton>
          </DropdownMenuTrigger>
          <DropdownMenuContent
            side="top"
            align="start"
            className="w-[min(16rem,calc(100vw-1rem))] [&_[role=menuitem]]:pointer-coarse:min-h-11"
          >
            <DropdownMenuLabel className="grid min-w-0 py-2 leading-tight">
              <span className="flex min-w-0 items-center gap-2">
                <span className="truncate text-sm font-semibold text-foreground">{name}</span>
                {/* With people (cloud), your role in this workspace; the detail line holds it when there is no address. */}
                {accountsEnabled && detail !== roleLabel && (
                  <Badge variant="outline" className="ml-auto shrink-0 font-medium" data-testid="user-role">
                    {roleLabel}
                  </Badge>
                )}
              </span>
              <span className="truncate text-xs font-normal text-muted-foreground">{detail}</span>
            </DropdownMenuLabel>
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
            {/* People in an account: the open workspace's pages, then their own account. */}
            {accountsEnabled ? (
              <>
                <DropdownMenuItem onSelect={() => go('/settings/plan')}>
                  <Gauge /> Plan & usage
                </DropdownMenuItem>
                {canManagePeople && (
                  <DropdownMenuItem onSelect={() => go('/settings/members')}>
                    <Users /> Members
                  </DropdownMenuItem>
                )}
                <DropdownMenuItem onSelect={() => go('/settings')}>
                  <Settings /> Workspace settings
                </DropdownMenuItem>
                <DropdownMenuItem onSelect={() => go('/settings/account')}>
                  <UserRound /> Account settings
                </DropdownMenuItem>
              </>
            ) : (
              <DropdownMenuItem onSelect={() => go('/settings')}>
                <Settings /> Settings
              </DropdownMenuItem>
            )}
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

const TRIAL_DAYS = 7

/** In the trial: the days left, counting down, and for owners and admins the way to a plan. */
function SidebarTrial() {
  const { isLocal } = useMode()
  const { canManagePeople } = useRole()
  const usage = useUsage()
  const { setOpenMobile } = useSidebar()
  const [now] = useState(Date.now)
  const ends = usage.data?.trial_ends_at

  if (isLocal || !ends) {
    return null
  }

  const days = daysLeft(ends, now)
  const left = days === 0 ? 'Your trial ends today' : `${pluralize(days, 'day')} left in your trial`

  return (
    <>
      {/* Collapsed sidebar: the clock alone, the days in its tooltip. */}
      <SidebarMenu className="hidden group-data-[collapsible=icon]:flex">
        <SidebarMenuItem>
          {canManagePeople ? (
            <SidebarMenuButton asChild tooltip={`${left}: choose a plan`} className="text-brand">
              <Link href="/plans" data-testid="trial-icon">
                <Clock /> <span>{left}</span>
              </Link>
            </SidebarMenuButton>
          ) : (
            <SidebarMenuButton tooltip={left} className="text-brand" data-testid="trial-icon">
              <Clock /> <span>{left}</span>
            </SidebarMenuButton>
          )}
        </SidebarMenuItem>
      </SidebarMenu>
      <div
        className="grid gap-2 rounded-lg border border-brand/30 bg-brand-soft/60 p-3 text-xs group-data-[collapsible=icon]:hidden"
        data-testid="trial-banner"
      >
        <p className="flex items-center gap-1.5 text-sm font-medium text-foreground">
          <Clock className="size-4 shrink-0 text-brand" aria-hidden /> {left}
        </p>
        <div
          role="progressbar"
          aria-label="Trial days left"
          aria-valuemin={0}
          aria-valuemax={TRIAL_DAYS}
          aria-valuenow={Math.min(days, TRIAL_DAYS)}
          className="h-1.5 overflow-hidden rounded-full bg-muted"
        >
          <div
            className="h-full rounded-full bg-brand"
            style={{ width: `${(Math.min(days, TRIAL_DAYS) / TRIAL_DAYS) * 100}%` }}
          />
        </div>
        <p className="text-muted-foreground">
          {canManagePeople
            ? 'Pick a plan to keep posting without a break and get more channels.'
            : 'Ask an owner or admin to pick a plan before it ends.'}
        </p>
        {canManagePeople && (
          <Button size="sm" asChild onClick={() => setOpenMobile(false)}>
            <Link href="/plans">
              <Sparkles /> Upgrade
            </Link>
          </Button>
        )}
      </div>
    </>
  )
}

/** Notifications above the user menu, the list beside the sidebar: one panel, never next to another menu. */
const noSubscribe = () => () => {}

/** The shortcut key as this computer names it: ⌘ on a Mac, Ctrl elsewhere (⌘ in the static render). */
function useModKey() {
  return useSyncExternalStore(
    noSubscribe,
    () => (/Mac|iPhone|iPad/.test(navigator.userAgent) ? '⌘' : 'Ctrl'),
    () => '⌘',
  )
}

/** Opens the assistant on the right (⌘J); only when the server has AI for it. */
function SidebarAssistant() {
  const { assistant } = useShell()
  const MOD_KEY = useModKey()

  if (!assistant) {
    return null
  }

  return (
    <SidebarMenu className="in-data-[mobile=true]:hidden">
      <SidebarMenuItem>
        <SidebarMenuButton
          tooltip={`Assistant (${MOD_KEY}J)`}
          isActive={assistant.open}
          onClick={assistant.toggle}
          data-testid="assistant-toggle"
        >
          <Sparkles className="text-brand" />
          <span>Assistant</span>
          <KbdGroup className="ml-auto group-data-[collapsible=icon]:hidden">
            <Kbd className="border bg-sidebar-accent text-sidebar-foreground/80">{MOD_KEY}</Kbd>
            <Kbd className="border bg-sidebar-accent text-sidebar-foreground/80">J</Kbd>
          </KbdGroup>
        </SidebarMenuButton>
      </SidebarMenuItem>
    </SidebarMenu>
  )
}

function SidebarNotifications() {
  const notes = useNotifications()
  const [open, setOpen] = useState(false)

  return (
    <SidebarMenu className="in-data-[mobile=true]:hidden">
      <SidebarMenuItem>
        <NotificationsPopover notes={notes} open={open} onOpenChange={setOpen} side="right" align="end">
          <SidebarMenuButton
            tooltip="Notifications"
            aria-label={notes.label}
            data-testid="notifications-bell"
            data-unread={notes.unread}
            isActive={open}
            className="relative"
          >
            <Bell />
            <span>Notifications</span>
            {notes.unread > 0 && (
              <span
                aria-hidden
                className="absolute top-1 left-5 hidden size-2 rounded-full bg-brand ring-2 ring-sidebar group-data-[collapsible=icon]:block"
              />
            )}
          </SidebarMenuButton>
        </NotificationsPopover>
        {notes.unread > 0 && (
          <SidebarMenuBadge className="rounded-full bg-brand-soft px-1.5 text-brand-strong">
            {notes.count}
          </SidebarMenuBadge>
        )}
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
        {/* Collapsed, search is an icon like the rest. */}
        <SidebarMenu className="hidden group-data-[collapsible=icon]:flex">
          <SidebarMenuItem>
            <SidebarMenuButton
              tooltip="Search or jump to (⌘K)"
              onClick={openCommandPalette}
              aria-label="Search or jump to"
            >
              <Search />
            </SidebarMenuButton>
          </SidebarMenuItem>
        </SidebarMenu>
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
        {/* Theme lives in Settings and ⌘K; collapsing in the sidebar edge (rail) and ⌘B. Phones have the bell in
            the top bar. Plan & usage is in the workspace menu. */}
        <SidebarGroup className="p-0 in-data-[mobile=true]:hidden">
          <SidebarGroupLabel>System</SidebarGroupLabel>
          <SidebarAssistant />
          <SidebarNotifications />
        </SidebarGroup>
        <SidebarTrial />
        <UserMenu />
      </SidebarFooter>
      <SidebarRail />
    </Sidebar>
  )
}

// ---- mobile chrome ----

function MobileHeader() {
  const { openCommandPalette, assistant } = useShell()

  return (
    <header
      className="sticky top-0 z-30 border-b bg-background/85 pt-[env(safe-area-inset-top)] [@media(max-height:500px)]:static backdrop-blur-xl supports-[backdrop-filter]:bg-background/70 md:hidden"
      data-testid="mobile-header"
    >
      <div className="flex h-(--mobile-header-h) items-center gap-1 px-2">
        <div className="flex min-w-0 flex-1 items-center">
          <WorkspaceMenu compact />
        </div>
        {assistant && (
          <button
            type="button"
            onClick={assistant.toggle}
            aria-label="Assistant"
            className="flex size-11 shrink-0 items-center justify-center rounded-lg text-brand hover:bg-muted"
          >
            <Sparkles className="size-5" />
          </button>
        )}
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
      // As wide as the page at least (body min-width): inline, a utility class would cost every route CSS.
      style={{ minWidth: '20rem' }}
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

const WorkspaceSwitcherDialog = dynamic(() => import('./workspace-switcher').then((m) => m.WorkspaceSwitcherDialog), {
  ssr: false,
})
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
  const ai = useAiStatus()
  const hasAssistant = !!ai.data?.operations.some((o) => o.id === 'assistant')
  const [assistantOpen, setAssistantOpen] = useState(() => {
    try {
      return localStorage.getItem('staqly.assistant.open') === '1'
    } catch {
      return false
    }
  })
  const [assistantUsed, setAssistantUsed] = useState(assistantOpen)
  const openAssistant = (open: boolean) => {
    setAssistantOpen(open)
    setAssistantUsed(true)

    try {
      localStorage.setItem('staqly.assistant.open', open ? '1' : '0')
    } catch {
      // Remembered for this page only.
    }
  }

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

      if (e.key.toLowerCase() === 'j' && (e.metaKey || e.ctrlKey) && hasAssistant) {
        e.preventDefault()
        setAssistantOpen((o) => {
          try {
            localStorage.setItem('staqly.assistant.open', o ? '0' : '1')
          } catch {
            // Remembered for this page only.
          }

          return !o
        })
      }
    }

    window.addEventListener('keydown', onKey)

    return () => window.removeEventListener('keydown', onKey)
  }, [toggleCommand, hasAssistant])

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
      assistant: hasAssistant ? { open: assistantOpen, toggle: () => openAssistant(!assistantOpen) } : null,
    }),
    // oxlint-disable-next-line react-hooks/exhaustive-deps -- openAssistant only sets state
    [setCreateOpen, setCommandOpen, setConnectOpen, hasAssistant, assistantOpen],
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
          <PlanGate>{children}</PlanGate>
        </SidebarInset>
        {/* Mounted once opened (its code loads then), so it can slide out when it closes. */}
        {hasAssistant && (assistantOpen || assistantUsed) && (
          <AssistantPanel open={assistantOpen} onOpenChange={openAssistant} />
        )}
        {isMobile && <MobileTabBar />}
      </SidebarProvider>
      {command.used && <CommandPalette open={command.open} onOpenChange={setCommandOpen} />}
      {create.used && <CreateCollectionDialog open={create.open} onOpenChange={setCreateOpen} />}
      {connect.used && <ConnectDeviceDialog open={connect.open} onOpenChange={setConnectOpen} />}
    </ShellContext.Provider>
  )
}

/**
 * Cloud with billing: a workspace opens only with a live plan (paid, or Starter's trial with a card). One that never
 * had one sends its owner to the welcome first (NoPlanGate), whose last step is the plan. One whose plan ended or whose
 * payment failed shows only that, with the way to Plan & usage (the one page that stays open), where paying unlocks it.
 */
function PlanGate({ children }: { children: ReactNode }) {
  const { isLocal } = useMode()
  const { canManagePeople } = useRole()
  const usage = useUsage()
  const billing = useBilling({ enabled: !isLocal && canManagePeople })
  const pathname = usePathname()
  // The account owner of a workspace that never had a plan doesn't get here: NoPlanGate sends them to the welcome.
  const locked = !isLocal && !!usage.data?.suspended
  const neverStarted = locked && !!billing.data?.enabled && !billing.data.subscription_status
  const billingPage = pathname.startsWith('/settings/plan') || pathname.startsWith('/settings/billing')

  if (!locked || (billingPage && canManagePeople)) {
    return children
  }

  return (
    <div className="flex flex-1 items-center justify-center px-4 py-16" data-testid="plan-locked">
      <div className="grid max-w-md justify-items-center gap-4 text-center">
        <span className="flex size-12 items-center justify-center rounded-2xl bg-destructive/10 text-destructive">
          <Lock className="size-6" aria-hidden />
        </span>
        <h1 className="text-2xl font-semibold tracking-tight">
          {neverStarted ? 'This workspace has no plan yet' : 'This workspace is locked'}
        </h1>
        <p className="text-sm text-pretty text-muted-foreground">
          {neverStarted
            ? 'It opens once the account owner chooses a plan for it.'
            : canManagePeople
              ? 'Its plan ended or the last payment did not go through. Pay in Plan & usage and the workspace opens again right away, with everything in it.'
              : 'Its plan ended or the last payment did not go through. An owner or admin can pay in Plan & usage; the workspace opens again right away.'}
        </p>
        {canManagePeople && !neverStarted && (
          <Button asChild>
            <Link href="/settings/plan">
              <CreditCard /> Go to billing
            </Link>
          </Button>
        )}
      </div>
    </div>
  )
}
