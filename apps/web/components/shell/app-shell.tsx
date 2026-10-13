"use client";

import { useMutation, useQueryClient } from "@tanstack/react-query";
import {
  Activity,
  BellRing,
  Blocks,
  ChevronRight,
  ChevronsUpDown,
  FolderOpen,
  Gauge,
  Inbox,
  ListChecks,
  LogIn,
  LogOut,
  Menu,
  Plus,
  QrCode,
  Radar,
  Search,
  Settings,
  Webhook,
} from "lucide-react";
import Link from "next/link";
import { usePathname, useRouter } from "next/navigation";
import { createContext, useContext, useMemo, useState, useSyncExternalStore, type ReactNode } from "react";
import { toast } from "sonner";
import { Collapsible, CollapsibleContent, CollapsibleTrigger } from "@/components/ui/collapsible";
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuGroup,
  DropdownMenuItem,
  DropdownMenuLabel,
  DropdownMenuSeparator,
  DropdownMenuTrigger,
} from "@/components/ui/dropdown-menu";
import { Kbd } from "@/components/ui/kbd";
import {
  Sidebar,
  SidebarContent,
  SidebarFooter,
  SidebarGroup,
  SidebarGroupAction,
  SidebarGroupContent,
  SidebarGroupLabel,
  SidebarHeader,
  SidebarInset,
  SidebarMenu,
  SidebarMenuBadge,
  SidebarMenuButton,
  SidebarMenuItem,
  SidebarMenuSkeleton,
  SidebarMenuSub,
  SidebarMenuSubButton,
  SidebarMenuSubItem,
  SidebarProvider,
  SidebarRail,
  SidebarTrigger,
  useSidebar,
} from "@/components/ui/sidebar";
import { LogoMark, Wordmark } from "@/components/common/logo";
import { CreateCollectionDialog } from "@/components/collections/create-collection-dialog";
import { ConnectDeviceDialog } from "@/components/devices/connect-device-dialog";
import { PlanBadge } from "@/components/plans/plan-badge";
import { api, setCsrfToken, UNLIMITED, type Collection } from "@/lib/api";
import { toastApiError } from "@/lib/notify";
import { useCollections, useMode, useSession, useUsage } from "@/lib/queries";
import { cn } from "@/lib/utils";
import { CommandPalette } from "./command-palette";
import { ThemeToggle } from "./theme-toggle";

interface ShellContextValue {
  openCreateCollection: () => void;
  openCommandPalette: () => void;
  openConnectDevice: () => void;
}

const ShellContext = createContext<ShellContextValue>({
  openCreateCollection: () => {},
  openCommandPalette: () => {},
  openConnectDevice: () => {},
});

export function useShell() {
  return useContext(ShellContext);
}

export const INTEGRATION_LINKS = [
  { href: "/integrations", label: "Overview" },
  { href: "/integrations/api-keys", label: "API keys", cloudOnly: true },
  { href: "/integrations/api", label: "REST API" },
  { href: "/integrations/mcp", label: "MCP" },
  { href: "/integrations/claude-code", label: "Claude Code" },
  { href: "/integrations/codex", label: "Codex" },
  { href: "/integrations/opencode", label: "OpenCode" },
  { href: "/integrations/n8n", label: "n8n" },
];

/** Integration links visible in the current mode (API keys exist only in cloud mode). */
export function useIntegrationLinks() {
  const { isLocal } = useMode();
  return INTEGRATION_LINKS.filter((l) => !(isLocal && "cloudOnly" in l && l.cloudOnly));
}

export function pendingTotal(collections?: Collection[]) {
  return (collections ?? []).reduce((n, c) => n + (c.status_counts?.pending ?? 0), 0);
}

export function useLogout(opts?: { message?: string }) {
  const qc = useQueryClient();
  const router = useRouter();
  return useMutation({
    mutationFn: () => api.logout(),
    onSuccess: () => {
      setCsrfToken(null);
      qc.clear();
      router.replace("/login");
      toast.success(opts?.message ?? "Signed out");
    },
    onError: (e) => toastApiError(e),
  });
}

// ---- responsive helpers ----

const WIDE_QUERY = "(min-width: 1280px)";

function subscribeWide(cb: () => void) {
  const mql = window.matchMedia(WIDE_QUERY);
  mql.addEventListener("change", cb);
  return () => mql.removeEventListener("change", cb);
}

/** True on wide desktops; tablets (768–1279px) get a collapsed icon rail by default. */
function useIsWide() {
  return useSyncExternalStore(
    subscribeWide,
    () => window.matchMedia(WIDE_QUERY).matches,
    () => true,
  );
}

// ---- workspace menu ----

export function WorkspaceMenu({ compact = false }: { compact?: boolean }) {
  const session = useSession();
  const { isLocal } = useMode();
  const router = useRouter();
  const { openConnectDevice } = useShell();
  const { setOpenMobile } = useSidebar();
  const logout = useLogout();
  const switchWorkspace = useLogout({ message: "Signed out — sign in to another workspace" });
  const ws = session.data?.workspace;
  const plan = isLocal ? "unlocked" : ws?.plan;
  const name = ws?.name ?? "staq.ly";
  const go = (href: string) => {
    setOpenMobile(false);
    router.push(href);
  };

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
        <span className="flex min-w-0 items-center gap-1.5 text-[11px] text-sidebar-foreground/60">
          {plan && <PlanBadge plan={plan} />}
          <span className="truncate">{isLocal ? "Local install" : "Cloud"}</span>
        </span>
      </div>
      <ChevronsUpDown className="ml-auto size-4 text-sidebar-foreground/50 group-data-[collapsible=icon]:hidden" />
    </SidebarMenuButton>
  );

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
            <span className="truncate text-sm font-semibold text-foreground">{name}</span>
            <span className="text-[11px] font-normal text-muted-foreground">
              {isLocal ? "Local install · no limits" : "Cloud workspace"}
            </span>
          </div>
          {plan && <PlanBadge plan={plan} className="ml-auto" />}
        </DropdownMenuLabel>
        <DropdownMenuSeparator />
        <DropdownMenuGroup>
          <DropdownMenuItem onSelect={() => go("/settings/plan")}>
            <Gauge /> Plan & usage
          </DropdownMenuItem>
          <DropdownMenuItem
            onSelect={() => {
              setOpenMobile(false);
              openConnectDevice();
            }}
          >
            <QrCode /> Connect a device
          </DropdownMenuItem>
          <DropdownMenuItem onSelect={() => go("/settings")}>
            <Settings /> Settings
          </DropdownMenuItem>
        </DropdownMenuGroup>
        {!isLocal && (
          <>
            <DropdownMenuSeparator />
            <DropdownMenuGroup>
              <DropdownMenuItem onSelect={() => go("/setup")}>
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
  );
}

// ---- plan footer ----

function PlanFooterLink() {
  const usage = useUsage();
  const { isLocal } = useMode();
  const pathname = usePathname();
  const { setOpenMobile } = useSidebar();
  const plan = usage.data?.plan;
  const unlocked = isLocal || plan?.id === "unlocked";
  const max = plan?.limits.items_per_month ?? UNLIMITED;
  const used = usage.data?.usage.items_per_month ?? 0;
  const pct = max > 0 ? Math.min(100, (used / max) * 100) : 0;
  return (
    <SidebarMenu>
      <SidebarMenuItem>
        <SidebarMenuButton
          asChild
          tooltip="Plan & usage"
          isActive={pathname === "/settings/plan"}
          className="h-auto min-h-8 py-1.5 in-data-[mobile=true]:h-auto in-data-[mobile=true]:min-h-12"
        >
          <Link href="/settings/plan" onClick={() => setOpenMobile(false)} data-testid="sidebar-plan-link">
            <Gauge className="text-sidebar-foreground/70" />
            <div className="grid min-w-0 flex-1 gap-1">
              <span className="flex items-center gap-1.5 text-xs">
                <span className="font-medium">Plan & usage</span>
                {(plan || isLocal) && <PlanBadge plan={unlocked ? "unlocked" : plan?.id} className="ml-auto" />}
              </span>
              {unlocked ? (
                <span className="text-[11px] text-sidebar-foreground/60">No limits on this install</span>
              ) : usage.data ? (
                <>
                  <span className="block h-1 overflow-hidden rounded-full bg-sidebar-accent">
                    <span
                      className={cn(
                        "block h-full rounded-full",
                        pct >= 100 ? "bg-rose-500" : pct > 80 ? "bg-amber-500" : "bg-emerald-500",
                      )}
                      style={{ width: `${max === UNLIMITED ? 0 : Math.max(pct, used ? 3 : 0)}%` }}
                    />
                  </span>
                  <span className="text-[11px] text-sidebar-foreground/60 tabular-nums">
                    {used.toLocaleString("en-US")} / {max === UNLIMITED ? "∞" : max.toLocaleString("en-US")} items this
                    month
                  </span>
                </>
              ) : null}
            </div>
          </Link>
        </SidebarMenuButton>
      </SidebarMenuItem>
    </SidebarMenu>
  );
}

// ---- sidebar ----

function NavCollections({ collections, isLoading }: { collections?: Collection[]; isLoading: boolean }) {
  const pathname = usePathname();
  const { setOpenMobile } = useSidebar();
  const visible = (collections ?? []).filter((c) => !c.archived);
  if (isLoading) {
    return (
      <>
        {Array.from({ length: 3 }, (_, i) => (
          <SidebarMenuItem key={i}>
            <SidebarMenuSkeleton showIcon />
          </SidebarMenuItem>
        ))}
      </>
    );
  }
  return (
    <>
      {visible.map((c) => {
        const href = `/collections/${c.id}`;
        const pending = c.status_counts?.pending ?? 0;
        return (
          <SidebarMenuItem key={c.id}>
            <SidebarMenuButton asChild isActive={pathname === href} tooltip={c.name} size="sm">
              <Link href={href} onClick={() => setOpenMobile(false)}>
                <span
                  className="size-2 shrink-0 rounded-[3px] bg-muted-foreground/40"
                  style={c.color ? { background: c.color } : undefined}
                  aria-hidden
                />
                <span className="truncate">{c.name}</span>
              </Link>
            </SidebarMenuButton>
            <SidebarMenuBadge
              className={cn(
                "in-data-[mobile=true]:top-2.5",
                pending ? "text-amber-600 dark:text-amber-400" : "text-muted-foreground/70",
              )}
              title={pending ? `${pending} pending of ${c.item_count}` : `${c.item_count} items`}
            >
              {pending || c.item_count}
            </SidebarMenuBadge>
          </SidebarMenuItem>
        );
      })}
      {visible.length === 0 && <div className="px-2 py-1.5 text-xs text-sidebar-foreground/60">No collections yet</div>}
    </>
  );
}

function AppSidebar() {
  const pathname = usePathname();
  const collections = useCollections(true);
  const { openCreateCollection, openCommandPalette } = useShell();
  const { setOpenMobile } = useSidebar();
  const integrationLinks = useIntegrationLinks();
  const pending = pendingTotal(collections.data);
  const close = () => setOpenMobile(false);
  const isActive = (href: string) => pathname === href || pathname.startsWith(href + "/");

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
            close();
            openCommandPalette();
          }}
          className="flex h-8 items-center gap-2 rounded-md border bg-background/60 px-2 text-xs text-muted-foreground transition-colors group-data-[collapsible=icon]:hidden hover:bg-background in-data-[mobile=true]:h-11 in-data-[mobile=true]:text-sm"
        >
          <Search className="size-3.5" />
          <span className="flex-1 text-left">Search or jump to…</span>
          <Kbd className="in-data-[mobile=true]:hidden">⌘K</Kbd>
        </button>
      </SidebarHeader>

      <SidebarContent>
        <SidebarGroup>
          <SidebarGroupContent>
            <SidebarMenu>
              <SidebarMenuItem>
                <SidebarMenuButton asChild isActive={isActive("/inbox")} tooltip="Inbox">
                  <Link href="/inbox" onClick={close}>
                    <Inbox /> <span>Inbox</span>
                  </Link>
                </SidebarMenuButton>
                {pending > 0 && (
                  <SidebarMenuBadge
                    data-testid="inbox-count"
                    className="rounded-full bg-brand px-1.5 text-brand-foreground peer-data-[active=true]/menu-button:text-brand-foreground in-data-[mobile=true]:top-3"
                  >
                    {pending > 999 ? "999+" : pending}
                  </SidebarMenuBadge>
                )}
              </SidebarMenuItem>
              <SidebarMenuItem>
                <SidebarMenuButton asChild isActive={pathname === "/collections"} tooltip="Collections">
                  <Link href="/collections" onClick={close}>
                    <FolderOpen /> <span>Collections</span>
                  </Link>
                </SidebarMenuButton>
              </SidebarMenuItem>
              <SidebarMenuItem>
                <SidebarMenuButton asChild isActive={isActive("/activity")} tooltip="Activity">
                  <Link href="/activity" onClick={close}>
                    <Activity /> <span>Activity</span>
                  </Link>
                </SidebarMenuButton>
              </SidebarMenuItem>
            </SidebarMenu>
          </SidebarGroupContent>
        </SidebarGroup>

        <SidebarGroup className="group-data-[collapsible=icon]:hidden">
          <SidebarGroupLabel>Collections</SidebarGroupLabel>
          <SidebarGroupAction
            title="New collection"
            onClick={() => {
              close();
              openCreateCollection();
            }}
            className="in-data-[mobile=true]:size-9"
          >
            <Plus /> <span className="sr-only">New collection</span>
          </SidebarGroupAction>
          <SidebarGroupContent>
            <SidebarMenu>
              <NavCollections collections={collections.data} isLoading={collections.isLoading} />
            </SidebarMenu>
          </SidebarGroupContent>
        </SidebarGroup>

        <SidebarGroup className="mt-auto">
          <SidebarGroupLabel>Automation</SidebarGroupLabel>
          <SidebarGroupContent>
            <SidebarMenu>
              <SidebarMenuItem>
                <SidebarMenuButton asChild isActive={isActive("/rules")} tooltip="Rules">
                  <Link href="/rules" onClick={close}>
                    <ListChecks /> <span>Rules</span>
                  </Link>
                </SidebarMenuButton>
              </SidebarMenuItem>
              <SidebarMenuItem>
                <SidebarMenuButton asChild isActive={isActive("/watchers")} tooltip="Watchers">
                  <Link href="/watchers" onClick={close}>
                    <Radar /> <span>Watchers</span>
                  </Link>
                </SidebarMenuButton>
              </SidebarMenuItem>
              <SidebarMenuItem>
                <SidebarMenuButton asChild isActive={isActive("/settings/notifications")} tooltip="Notifications">
                  <Link href="/settings/notifications" onClick={close}>
                    <BellRing /> <span>Notifications</span>
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
              <Collapsible asChild defaultOpen={pathname.startsWith("/integrations")} className="group/collapsible">
                <SidebarMenuItem>
                  <CollapsibleTrigger asChild>
                    <SidebarMenuButton tooltip="Integrations" isActive={pathname === "/integrations"}>
                      <Blocks /> <span>Integrations</span>
                      <ChevronRight className="ml-auto transition-transform duration-200 group-data-[collapsible=icon]:hidden group-data-[state=open]/collapsible:rotate-90" />
                    </SidebarMenuButton>
                  </CollapsibleTrigger>
                  <CollapsibleContent>
                    <SidebarMenuSub>
                      {integrationLinks.map((l) => (
                        <SidebarMenuSubItem key={l.href}>
                          <SidebarMenuSubButton
                            asChild
                            isActive={pathname === l.href}
                            className="in-data-[mobile=true]:h-10"
                          >
                            <Link href={l.href} onClick={close}>
                              <span>{l.label}</span>
                            </Link>
                          </SidebarMenuSubButton>
                        </SidebarMenuSubItem>
                      ))}
                    </SidebarMenuSub>
                  </CollapsibleContent>
                </SidebarMenuItem>
              </Collapsible>
              <SidebarMenuItem>
                <SidebarMenuButton asChild isActive={isActive("/webhooks")} tooltip="Webhooks">
                  <Link href="/webhooks" onClick={close}>
                    <Webhook /> <span>Webhooks</span>
                  </Link>
                </SidebarMenuButton>
              </SidebarMenuItem>
              <SidebarMenuItem>
                <SidebarMenuButton
                  asChild
                  isActive={
                    pathname === "/settings" || pathname === "/settings/data" || pathname === "/settings/system"
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
        <PlanFooterLink />
        <div className="flex items-center gap-1 group-data-[collapsible=icon]:flex-col">
          <span className="flex-1 truncate px-2 text-sidebar-foreground/50 group-data-[collapsible=icon]:hidden">
            <Wordmark className="text-xs font-medium" />
          </span>
          <ThemeToggle />
          <SidebarTrigger className="hidden md:inline-flex" aria-label="Toggle sidebar (⌘B)" />
        </div>
      </SidebarFooter>
      <SidebarRail />
    </Sidebar>
  );
}

// ---- mobile chrome ----

function MobileHeader() {
  const { openCommandPalette } = useShell();
  return (
    <header
      className="sticky top-0 z-30 border-b bg-background/85 pt-[env(safe-area-inset-top)] backdrop-blur-xl supports-[backdrop-filter]:bg-background/70 md:hidden"
      data-testid="mobile-header"
    >
      <div className="flex h-(--mobile-header-h) items-center gap-1 px-2">
        <div className="flex min-w-0 flex-1 items-center">
          <WorkspaceMenu compact />
        </div>
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
  );
}

function MobileTabBar() {
  const pathname = usePathname();
  const collections = useCollections(true);
  const pending = pendingTotal(collections.data);
  const { setOpenMobile, openMobile } = useSidebar();
  const tabs: { href: string; label: string; Icon: typeof Inbox; badge?: number; prefixes?: string[] }[] = [
    { href: "/inbox", label: "Inbox", Icon: Inbox, badge: pending },
    { href: "/collections", label: "Collections", Icon: FolderOpen, prefixes: ["/collections", "/items"] },
    { href: "/activity", label: "Activity", Icon: Activity },
    { href: "/webhooks", label: "Webhooks", Icon: Webhook },
  ];
  return (
    <nav
      aria-label="Primary"
      data-testid="mobile-tabbar"
      className="fixed inset-x-0 bottom-0 z-40 border-t bg-background/90 pb-[env(safe-area-inset-bottom)] backdrop-blur-xl supports-[backdrop-filter]:bg-background/75 md:hidden"
    >
      <ul className="mx-auto grid h-(--mobile-tabbar-h) max-w-lg grid-cols-5 px-1">
        {tabs.map(({ href, label, Icon, badge, prefixes }) => {
          const active = (prefixes ?? [href]).some((p) => pathname === p || pathname.startsWith(p + "/"));
          return (
            <li key={href} className="flex">
              <Link
                href={href}
                aria-current={active ? "page" : undefined}
                className={cn(
                  "relative flex flex-1 flex-col items-center justify-center gap-0.5 rounded-xl text-[10.5px] font-medium transition-colors",
                  active ? "text-foreground" : "text-muted-foreground",
                )}
              >
                <span
                  className={cn(
                    "relative flex h-7 w-12 items-center justify-center rounded-full transition-colors",
                    active && "bg-brand-soft text-brand",
                  )}
                >
                  <Icon className="size-[19px]" strokeWidth={active ? 2.3 : 2} />
                  {!!badge && badge > 0 && (
                    <span className="absolute -top-1 right-0.5 min-w-4 rounded-full bg-brand px-1 text-center text-[10px] leading-4 font-semibold text-brand-foreground tabular-nums ring-2 ring-background">
                      {badge > 99 ? "99+" : badge}
                    </span>
                  )}
                </span>
                {label}
              </Link>
            </li>
          );
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
  );
}

export function AppShell({ children }: { children: ReactNode }) {
  const [createOpen, setCreateOpen] = useState(false);
  const [commandOpen, setCommandOpen] = useState(false);
  const [connectOpen, setConnectOpen] = useState(false);
  const wide = useIsWide();
  // The user's explicit toggle wins until the breakpoint changes again.
  const [override, setOverride] = useState<{ wide: boolean; open: boolean } | null>(null);
  const sidebarOpen = override && override.wide === wide ? override.open : wide;
  const ctx = useMemo<ShellContextValue>(
    () => ({
      openCreateCollection: () => setCreateOpen(true),
      openCommandPalette: () => setCommandOpen(true),
      openConnectDevice: () => setConnectOpen(true),
    }),
    [],
  );

  return (
    <ShellContext.Provider value={ctx}>
      <SidebarProvider open={sidebarOpen} onOpenChange={(open) => setOverride({ wide, open })}>
        <AppSidebar />
        <SidebarInset className="min-w-0 pb-(--mobile-chrome-bottom) md:pb-0">
          <MobileHeader />
          {children}
        </SidebarInset>
        <MobileTabBar />
      </SidebarProvider>
      <CommandPalette open={commandOpen} onOpenChange={setCommandOpen} />
      <CreateCollectionDialog open={createOpen} onOpenChange={setCreateOpen} />
      <ConnectDeviceDialog open={connectOpen} onOpenChange={setConnectOpen} />
    </ShellContext.Provider>
  );
}
