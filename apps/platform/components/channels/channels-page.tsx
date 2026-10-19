'use client'

import { useMutation, useQueryClient } from '@tanstack/react-query'
import {
  Ellipsis,
  ExternalLink,
  Pencil,
  PlugZap,
  Plus,
  Power,
  RefreshCw,
  Settings2,
  Share2,
  Trash2,
} from 'lucide-react'
import { useState } from 'react'

import { ConfirmDialog } from '@/components/common/confirm-dialog'
import { EmptyState, ErrorState } from '@/components/common/error-state'
import { PageContainer, PageHeader } from '@/components/common/page-header'
import { SkeletonList } from '@/components/common/skeleton-list'
import { ChannelStatusBadge } from '@/components/common/status-badge'
import { Button } from '@/components/ui/button'
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from '@/components/ui/dialog'
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuSeparator,
  DropdownMenuTrigger,
} from '@/components/ui/dropdown-menu'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import { useLazyDialog } from '@/hooks/use-lazy-dialog'
import { api, type Channel, type Provider } from '@/lib/api'
import { namedErrorToast, namedToast, toastApiError } from '@/lib/notify'
import { ProviderIcon } from '@/lib/providers'
import { qk, useChannels, useProviders } from '@/lib/queries'
import { safeHttpUrl } from '@/lib/safe'

import { AddChannelSheet } from './add-channel-sheet'
import { ChannelAvatar } from './channel-avatars'

function RenameDialog({
  channel,
  open,
  onOpenChange,
}: {
  channel: Channel
  open: boolean
  onOpenChange: (o: boolean) => void
}) {
  const qc = useQueryClient()
  const [name, setName] = useState(channel.name)
  const save = useMutation({
    mutationFn: () => api.updateChannel(channel.id, { name: name.trim() }),
    onSuccess: (c) => {
      void qc.invalidateQueries({ queryKey: qk.channels })
      namedToast(c.name, 'was renamed')
      onOpenChange(false)
    },
    onError: (e) => toastApiError(e),
  })

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent className="sm:max-w-md">
        <form
          className="grid gap-4"
          onSubmit={(e) => {
            e.preventDefault()
            save.mutate()
          }}
        >
          <DialogHeader>
            <DialogTitle>Rename channel</DialogTitle>
            <DialogDescription>The name is only for you; the platform keeps its own.</DialogDescription>
          </DialogHeader>
          <div className="grid gap-2">
            <Label htmlFor="channel-name">Name</Label>
            <Input id="channel-name" value={name} onChange={(e) => setName(e.target.value)} />
          </div>
          <DialogFooter>
            <Button type="button" variant="outline" onClick={() => onOpenChange(false)}>
              Cancel
            </Button>
            <Button type="submit" disabled={save.isPending || !name.trim() || name.trim() === channel.name}>
              Save
            </Button>
          </DialogFooter>
        </form>
      </DialogContent>
    </Dialog>
  )
}

/** One connected channel: avatar, name, handle, state, and its menu. */
function ChannelRow({
  channel,
  provider,
  onReconnect,
}: {
  channel: Channel
  provider?: Provider
  onReconnect: () => void
}) {
  const qc = useQueryClient()
  const [rename, setRename] = useState(false)
  const [remove, setRemove] = useState(false)
  const refresh = () => qc.invalidateQueries({ queryKey: qk.channels })
  const test = useMutation({
    mutationFn: () => api.testChannel(channel.id),
    onSuccess: (r) => {
      void refresh()

      if (r.ok) {
        namedToast(channel.name, 'works', r.profile?.handle ? `Signed in as ${r.profile.handle}.` : undefined)
      } else {
        namedErrorToast(channel.name, 'needs to be reconnected', r.error || undefined)
      }
    },
    onError: (e) => toastApiError(e),
  })
  const toggle = useMutation({
    mutationFn: () =>
      api.updateChannel(channel.id, { status: channel.status === 'disabled' ? 'connected' : 'disabled' }),
    onSuccess: (c) => {
      void refresh()
      namedToast(c.name, c.status === 'disabled' ? 'is disabled' : 'is enabled again')
    },
    onError: (e) => toastApiError(e),
  })
  const del = useMutation({
    mutationFn: () => api.deleteChannel(channel.id),
    onSuccess: () => {
      void refresh()
      void qc.invalidateQueries({ queryKey: qk.postsAll })
      namedToast(channel.name, 'was removed', 'Scheduled posts lose this channel.')
      setRemove(false)
    },
    onError: (e) => toastApiError(e),
  })
  const profile = channel.profile_url ? safeHttpUrl(channel.profile_url) : null

  return (
    <li className="grid grid-cols-[auto_minmax(0,1fr)_auto] items-center gap-3 px-3 py-2.5" data-testid="channel-row">
      <ChannelAvatar channel={channel} className="size-10" />
      <span className="min-w-0">
        <span className="flex flex-wrap items-center gap-2">
          <span className="truncate text-sm font-medium">{channel.name}</span>
          <ChannelStatusBadge status={channel.status} title={channel.last_error || undefined} />
        </span>
        <span className="mt-0.5 flex flex-wrap items-center gap-x-2 text-xs text-muted-foreground">
          <span className="inline-flex items-center gap-1">
            <ProviderIcon id={channel.provider} className="size-3" /> {provider?.name ?? channel.provider}
          </span>
          {channel.handle && <span className="truncate">{channel.handle}</span>}
          {profile && (
            <a
              href={profile}
              target="_blank"
              rel="noopener noreferrer nofollow"
              className="inline-flex items-center gap-1 hover:text-foreground hover:underline"
            >
              Open profile <ExternalLink className="size-3" aria-hidden />
            </a>
          )}
        </span>
        {channel.last_error && channel.status === 'needs_reconnect' && (
          <span className="mt-0.5 block text-xs text-destructive wrap-anywhere">{channel.last_error}</span>
        )}
      </span>
      <div className="flex items-center gap-1.5">
        {channel.status === 'needs_reconnect' && (
          <Button size="sm" onClick={onReconnect}>
            <PlugZap /> Reconnect
          </Button>
        )}
        <DropdownMenu>
          <DropdownMenuTrigger asChild>
            <Button size="icon" variant="ghost" aria-label={`Actions for ${channel.name}`}>
              <Ellipsis />
            </Button>
          </DropdownMenuTrigger>
          <DropdownMenuContent align="end">
            <DropdownMenuItem onSelect={() => setRename(true)}>
              <Pencil /> Rename
            </DropdownMenuItem>
            <DropdownMenuItem onSelect={() => test.mutate()} disabled={test.isPending}>
              <RefreshCw /> Test connection
            </DropdownMenuItem>
            {channel.status !== 'needs_reconnect' && (
              <DropdownMenuItem onSelect={() => toggle.mutate()} disabled={toggle.isPending}>
                <Power /> {channel.status === 'disabled' ? 'Enable' : 'Disable'}
              </DropdownMenuItem>
            )}
            {channel.status === 'needs_reconnect' && (
              <DropdownMenuItem onSelect={onReconnect}>
                <PlugZap /> Reconnect
              </DropdownMenuItem>
            )}
            {channel.status !== 'needs_reconnect' &&
              provider?.auth !== 'oauth2' &&
              (provider?.credential_fields?.length ?? 0) > 0 && (
                <DropdownMenuItem onSelect={onReconnect}>
                  <Settings2 /> Edit connection
                </DropdownMenuItem>
              )}
            <DropdownMenuSeparator />
            <DropdownMenuItem variant="destructive" onSelect={() => setRemove(true)}>
              <Trash2 /> Remove
            </DropdownMenuItem>
          </DropdownMenuContent>
        </DropdownMenu>
      </div>
      {rename && <RenameDialog channel={channel} open={rename} onOpenChange={setRename} />}
      <ConfirmDialog
        open={remove}
        onOpenChange={setRemove}
        title={`Remove ${channel.name}?`}
        description="Staqly forgets the sign-in and the channel. Posts already published stay on the platform; scheduled posts lose this channel."
        confirmLabel={del.isPending ? 'Removing...' : 'Remove'}
        variant="destructive"
        onConfirm={() => del.mutate()}
      />
    </li>
  )
}

/** Connected channels, grouped by platform, with Add channel. */
export function ChannelsPage() {
  const channels = useChannels()
  const providers = useProviders()
  const add = useLazyDialog()
  const [reconnect, setReconnect] = useState<Channel | null>(null)
  const byProvider = new Map<string, Channel[]>()

  for (const c of channels.data ?? []) {
    byProvider.set(c.provider, [...(byProvider.get(c.provider) ?? []), c])
  }

  const groups = [...byProvider.entries()].toSorted(([a], [b]) => a.localeCompare(b))
  const providerOf = (id: string) => providers.data?.find((p) => p.id === id)

  return (
    <PageContainer>
      <PageHeader
        title="Channels"
        description="The accounts your posts go to. Connect a platform once; every post picks its channels."
        actions={
          <Button onClick={() => add.setOpen(true)} data-testid="add-channel">
            <Plus /> Add channel
          </Button>
        }
      />
      <div className="mt-6">
        {channels.isLoading ? (
          <SkeletonList count={4} className="h-14 rounded-lg" />
        ) : channels.error ? (
          <ErrorState error={channels.error} onRetry={() => channels.refetch()} />
        ) : groups.length === 0 ? (
          <EmptyState
            icon={<Share2 />}
            title="No channels yet"
            description="Connect the first platform. Your posts can then go there at their time, with retries when the platform is slow."
            action={
              <Button onClick={() => add.setOpen(true)}>
                <Plus /> Add channel
              </Button>
            }
          />
        ) : (
          <div className="grid gap-6">
            {groups.map(([providerId, list]) => (
              <section key={providerId} aria-label={providerOf(providerId)?.name ?? providerId} className="grid gap-2">
                <h2 className="flex items-center gap-2 text-sm font-semibold">
                  <ProviderIcon id={providerId} className="size-4" /> {providerOf(providerId)?.name ?? providerId}
                  <span className="font-normal text-muted-foreground">{list.length}</span>
                </h2>
                <ul className="divide-y rounded-xl border" data-testid={`channels-${providerId}`}>
                  {list.map((c) => (
                    <ChannelRow
                      key={c.id}
                      channel={c}
                      provider={providerOf(providerId)}
                      onReconnect={() => {
                        setReconnect(c)
                        add.setOpen(true)
                      }}
                    />
                  ))}
                </ul>
              </section>
            ))}
          </div>
        )}
      </div>
      {add.used && (
        <AddChannelSheet
          open={add.open}
          onOpenChange={(o) => {
            add.setOpen(o)

            if (!o) {
              setReconnect(null)
            }
          }}
          reconnect={reconnect}
        />
      )}
    </PageContainer>
  )
}
