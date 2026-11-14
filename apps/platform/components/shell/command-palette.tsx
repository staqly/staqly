'use client'

import {
  Activity,
  BarChart3,
  BellRing,
  Blocks,
  CalendarClock,
  PenSquare,
  Gauge,
  HeartPulse,
  Share2,
  KeyRound,
  Monitor,
  Moon,
  Plus,
  QrCode,
  Settings,
  ShieldCheck,
  Sun,
  Webhook,
} from 'lucide-react'
import { useTheme } from 'next-themes'
import { useRouter } from 'next/navigation'

import {
  Command,
  CommandDialog,
  CommandEmpty,
  CommandGroup,
  CommandInput,
  CommandItem,
  CommandList,
  CommandSeparator,
  CommandShortcut,
} from '@/components/ui/command'
import { useCollections } from '@/lib/queries'
import { useMode, useRole } from '@/lib/queries'

import { useIntegrationLinks, useShell } from './app-shell'

export function CommandPalette({ open, onOpenChange }: { open: boolean; onOpenChange: (o: boolean) => void }) {
  const router = useRouter()
  const { setTheme } = useTheme()
  const { openCreateCollection, openConnectDevice } = useShell()
  const collections = useCollections(true)
  const links = useIntegrationLinks()
  const { isCloud } = useMode()
  const { can } = useRole()

  const go = (href: string) => {
    onOpenChange(false)
    router.push(href)
  }

  return (
    <CommandDialog
      open={open}
      onOpenChange={onOpenChange}
      title="Command palette"
      description="Jump anywhere in staqly"
    >
      {/* cmdk's root: CommandInput/List/Item read their store from it (without it the palette crashed on open). */}
      {/* cmdk names its search box after this label (aria-labelledby). */}
      <Command label="Search pages, collections and commands">
        <CommandInput placeholder="Type a command or search..." />
        <CommandList>
          <CommandEmpty>No results.</CommandEmpty>
          <CommandGroup heading="Navigate">
            <CommandItem onSelect={() => go('/schedule')}>
              <CalendarClock /> Schedule
            </CommandItem>
            <CommandItem onSelect={() => go('/posts')}>
              <PenSquare /> Posts
            </CommandItem>
            <CommandItem onSelect={() => go('/channels')}>
              <Share2 /> Channels
            </CommandItem>
            <CommandItem onSelect={() => go('/analytics')}>
              <BarChart3 /> Analytics
            </CommandItem>
            <CommandItem onSelect={() => go('/activity')}>
              <Activity /> Activity
            </CommandItem>
            {can('webhooks:manage') && (
              <CommandItem onSelect={() => go('/webhooks')}>
                <Webhook /> Webhooks
              </CommandItem>
            )}
            <CommandItem onSelect={() => go('/settings')}>
              <Settings /> Settings
            </CommandItem>
            <CommandItem onSelect={() => go('/settings/plan')}>
              <Gauge /> Plan & usage
            </CommandItem>
            {can('alerts:manage') && (
              <CommandItem onSelect={() => go('/alerts')}>
                <BellRing /> Alerts
              </CommandItem>
            )}
            <CommandItem onSelect={() => go('/settings/data')}>
              <ShieldCheck /> Data & privacy
            </CommandItem>
            <CommandItem onSelect={() => go('/settings/system')}>
              <HeartPulse /> System status
            </CommandItem>
          </CommandGroup>
          {(collections.data?.length ?? 0) > 0 && (
            <CommandGroup heading="Collections">
              {collections.data!.map((c) => (
                <CommandItem
                  key={c.id}
                  value={`collection ${c.name} ${c.id}`}
                  onSelect={() => go(`/posts?collection=${c.id}`)}
                >
                  <PenSquare /> {c.name}
                  <CommandShortcut>{c.archived ? 'archived' : c.post_count}</CommandShortcut>
                </CommandItem>
              ))}
            </CommandGroup>
          )}
          <CommandGroup heading="Integrations">
            {links.map((l) => (
              <CommandItem key={l.href} value={`integrations ${l.label}`} onSelect={() => go(l.href)}>
                {l.href.endsWith('api-keys') ? <KeyRound /> : <Blocks />} {l.label}
              </CommandItem>
            ))}
          </CommandGroup>
          <CommandSeparator />
          <CommandGroup heading="Actions">
            <CommandItem
              onSelect={() => {
                onOpenChange(false)
                openCreateCollection()
              }}
            >
              <Plus /> New collection
            </CommandItem>
            <CommandItem
              onSelect={() => {
                onOpenChange(false)
                openConnectDevice()
              }}
            >
              <QrCode /> Connect a device
            </CommandItem>
            {isCloud && (
              <CommandItem onSelect={() => go('/integrations/api-keys?new=1')}>
                <KeyRound /> Create API key
              </CommandItem>
            )}
            <CommandItem
              onSelect={() => {
                setTheme('light')
                onOpenChange(false)
              }}
            >
              <Sun /> Light theme
            </CommandItem>
            <CommandItem
              onSelect={() => {
                setTheme('dark')
                onOpenChange(false)
              }}
            >
              <Moon /> Dark theme
            </CommandItem>
            <CommandItem
              onSelect={() => {
                setTheme('system')
                onOpenChange(false)
              }}
            >
              <Monitor /> System theme
            </CommandItem>
          </CommandGroup>
        </CommandList>
      </Command>
    </CommandDialog>
  )
}
