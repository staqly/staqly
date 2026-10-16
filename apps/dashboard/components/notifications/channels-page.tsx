'use client'

import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import {
  BellRing,
  CircleAlert,
  History,
  Info,
  Link2,
  MoreHorizontal,
  MousePointerClick,
  Pencil,
  Plus,
  Power,
  Send,
  Trash2,
} from 'lucide-react'
import Link from 'next/link'
import { useState } from 'react'
import { toast } from 'sonner'

import { Callout } from '@/components/common/callout'
import { EmptyState, ErrorState } from '@/components/common/error-state'
import { PageContainer } from '@/components/common/page-header'
import { UsageChip } from '@/components/plans/usage-chip'
import { SettingsHeader } from '@/components/settings/settings-header'
import { SettingsNav } from '@/components/settings/settings-nav'
import {
  AlertDialog,
  AlertDialogAction,
  AlertDialogCancel,
  AlertDialogContent,
  AlertDialogDescription,
  AlertDialogFooter,
  AlertDialogHeader,
  AlertDialogTitle,
} from '@/components/ui/alert-dialog'
import { Button } from '@/components/ui/button'
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuSeparator,
  DropdownMenuTrigger,
} from '@/components/ui/dropdown-menu'
import { Sheet, SheetContent, SheetDescription, SheetHeader, SheetTitle } from '@/components/ui/sheet'
import { Skeleton } from '@/components/ui/skeleton'
import { Switch } from '@/components/ui/switch'
import { api, type NotificationChannel, type NotificationDelivery } from '@/lib/api'
import { CHANNEL_TYPE_INFO, channelHealth, channelTarget, digestLabel } from '@/lib/channels'
import { formatDateTime, timeAgo } from '@/lib/format'
import { toastApiError } from '@/lib/notify'
import { qk, useChannels, useCollections } from '@/lib/queries'
import { TONES } from '@/lib/tones'
import { cn } from '@/lib/utils'

import { ChannelFormSheet } from './channel-form-sheet'
import { ChannelTypeIcon } from './channel-type-icon'

const HEALTH: Record<ReturnType<typeof channelHealth>, { label: string; cls: string }> = {
  active: { label: 'Active', cls: TONES.success.badge },
  paused: { label: 'Paused', cls: 'bg-muted text-muted-foreground ring-border' },
  disabled: { label: 'Disabled after failures', cls: TONES.danger.badge },
  failing: { label: 'Failing', cls: TONES.warning.badge },
}

const DELIVERY_STATUS: Record<string, string> = {
  pending: TONES.info.badge,
  sent: TONES.success.badge,
  failed: TONES.danger.badge,
  skipped: 'bg-muted text-muted-foreground ring-border',
}

function Pill({ cls, children }: { cls: string; children: React.ReactNode }) {
  return (
    <span
      className={cn('inline-flex h-5 items-center rounded-md px-1.5 text-[11px] font-medium ring-1 ring-inset', cls)}
    >
      {children}
    </span>
  )
}

function useTestChannel() {
  return useMutation({
    mutationFn: (ch: NotificationChannel) => api.testChannel(ch.id),
    onSuccess: (r, ch) => {
      if (r.ok) {
        toast.success('Test message sent', { description: `Check “${ch.name}”.` })
      } else {
        toast.error('Test message failed', { description: r.error || 'The channel did not accept the message.' })
      }
    },
    onError: (e) => toastApiError(e, 'Could not send a test message'),
  })
}

function DeliveriesSheet({
  channel,
  onOpenChange,
}: {
  channel: NotificationChannel | null
  onOpenChange: (o: boolean) => void
}) {
  const open = !!channel
  const deliveries = useQuery({
    queryKey: qk.channelDeliveries(channel?.id ?? ''),
    queryFn: async () => (await api.channelDeliveries(channel!.id)).data ?? [],
    enabled: open,
    refetchInterval: open ? 5000 : false,
  })
  const list: NotificationDelivery[] = deliveries.data ?? []

  return (
    <Sheet open={open} onOpenChange={onOpenChange}>
      <SheetContent className="gap-0 data-[side=right]:w-full data-[side=right]:sm:max-w-lg">
        <SheetHeader className="border-b pt-[max(1rem,env(safe-area-inset-top))]">
          <SheetTitle>Deliveries</SheetTitle>
          <SheetDescription className="truncate">
            {channel?.name} · recent notifications, refreshes every 5s
          </SheetDescription>
        </SheetHeader>
        <div className="flex-1 overflow-y-auto overscroll-contain p-4 pb-[max(1rem,env(safe-area-inset-bottom))]">
          {deliveries.isLoading ? (
            <Skeleton className="h-32" />
          ) : deliveries.error ? (
            <ErrorState error={deliveries.error} onRetry={() => deliveries.refetch()} />
          ) : !list.length ? (
            <div className="rounded-lg border border-dashed p-6 text-center text-sm text-muted-foreground">
              Nothing sent yet. Test messages are sent directly and don&apos;t show up here.
            </div>
          ) : (
            <ul className="grid gap-2" data-testid="channel-deliveries">
              {list.map((d) => (
                <li key={d.id} className="grid gap-1.5 rounded-xl border bg-card p-3 text-sm">
                  <div className="flex items-center gap-2">
                    <span className="min-w-0 flex-1 truncate font-mono text-xs">{d.event_type}</span>
                    <Pill cls={DELIVERY_STATUS[d.status ?? ''] ?? DELIVERY_STATUS.skipped}>{d.status}</Pill>
                  </div>
                  <div className="flex flex-wrap gap-x-3 gap-y-0.5 text-xs text-muted-foreground">
                    <span title={formatDateTime(d.created_at)}>{timeAgo(d.created_at)}</span>
                    <span className="tabular-nums">
                      {d.attempts ?? 0} attempt{d.attempts === 1 ? '' : 's'}
                    </span>
                    {d.status === 'sent' && d.sent_at && <span>sent {timeAgo(d.sent_at)}</span>}
                    {d.status === 'pending' && d.next_attempt_at && <span>next {timeAgo(d.next_attempt_at)}</span>}
                    {d.item_id && (
                      <Link href={`/items/${d.item_id}`} className="text-brand hover:underline">
                        Open item
                      </Link>
                    )}
                  </div>
                  {d.last_error && <p className="text-xs break-words text-red-600 dark:text-red-400">{d.last_error}</p>}
                </li>
              ))}
            </ul>
          )}
        </div>
      </SheetContent>
    </Sheet>
  )
}

function ChannelCard({
  channel: ch,
  collectionName,
  onEdit,
  onDelete,
  onDeliveries,
}: {
  channel: NotificationChannel
  collectionName: (id: string) => string
  onEdit: () => void
  onDelete: () => void
  onDeliveries: () => void
}) {
  const qc = useQueryClient()
  const health = channelHealth(ch)
  const test = useTestChannel()
  const update = useMutation({
    mutationFn: (enabled: boolean) => api.updateChannel(ch.id, { enabled }),
    onSuccess: (nc, enabled) => {
      void qc.invalidateQueries({ queryKey: qk.channels })
      toast.success(enabled ? (ch.disabled_reason ? 'Channel re-enabled' : 'Channel enabled') : 'Channel paused', {
        description: enabled && nc.last_error ? nc.last_error : undefined,
      })
    },
    onError: (e) => toastApiError(e),
  })

  return (
    <article
      className={cn('grid content-start gap-3 rounded-xl border bg-card p-4', health === 'paused' && 'opacity-80')}
      data-testid="channel-card"
      aria-label={ch.name}
    >
      <div className="flex items-start gap-3">
        <ChannelTypeIcon type={ch.type} />
        <div className="min-w-0 flex-1">
          <div className="flex flex-wrap items-center gap-x-2 gap-y-1">
            <h3 className="min-w-0 truncate text-sm font-semibold">{ch.name}</h3>
            <Pill cls={HEALTH[health].cls}>{HEALTH[health].label}</Pill>
          </div>
          <p className="truncate text-xs text-muted-foreground">
            {CHANNEL_TYPE_INFO[ch.type].label} · {channelTarget(ch)}
          </p>
        </div>
        <Switch
          checked={ch.enabled}
          onCheckedChange={(v) => update.mutate(v)}
          disabled={update.isPending}
          aria-label={ch.enabled ? `Pause ${ch.name}` : `Enable ${ch.name}`}
          className="mt-1"
        />
      </div>

      {health === 'disabled' && (
        <Callout tone="danger" size="xs" role="status">
          <strong className="font-medium">{ch.disabled_reason}</strong>
          {ch.last_error && ch.last_error !== ch.disabled_reason && (
            <span className="block text-muted-foreground">Last error: {ch.last_error}</span>
          )}
          <span className="mt-2 flex flex-wrap items-center gap-2">
            <Button size="sm" variant="outline" onClick={() => update.mutate(true)} disabled={update.isPending}>
              <Power /> Re-enable
            </Button>
            <span className="text-muted-foreground">
              Fix the cause first — queued messages are delivered once it works.
            </span>
          </span>
        </Callout>
      )}
      {health === 'failing' && ch.last_error && (
        <Callout icon={CircleAlert} size="xs">
          {ch.fail_count} failed send{ch.fail_count === 1 ? '' : 's'} in a row: {ch.last_error}
        </Callout>
      )}

      <div className="flex flex-wrap gap-1">
        {ch.events.map((e) => (
          <span key={e} className="rounded bg-muted px-1.5 py-0.5 font-mono text-[11px]">
            {e}
          </span>
        ))}
        <span className="rounded px-1.5 py-0.5 text-[11px] text-muted-foreground">
          {ch.collection_ids.length ? `in ${ch.collection_ids.map(collectionName).join(', ')}` : 'all collections'}
        </span>
      </div>

      <div className="flex flex-wrap items-center gap-x-3 gap-y-1 text-xs text-muted-foreground">
        <span className="inline-flex items-center gap-1">
          {ch.interactive ? (
            <MousePointerClick className="size-3.5" aria-hidden />
          ) : (
            <Link2 className="size-3.5" aria-hidden />
          )}
          {ch.interactive ? 'Buttons in chat' : 'One-tap links'}
        </span>
        <span>{digestLabel(ch.digest_window_seconds)}</span>
        <span>{ch.last_sent_at ? `Last sent ${timeAgo(ch.last_sent_at)}` : 'Nothing sent yet'}</span>
      </div>

      <div className="flex flex-wrap items-center gap-2">
        <Button size="sm" variant="outline" onClick={() => test.mutate(ch)} disabled={test.isPending}>
          <Send /> {test.isPending ? 'Sending...' : 'Send test'}
        </Button>
        <Button size="sm" variant="ghost" onClick={onDeliveries}>
          <History /> Deliveries
        </Button>
        <DropdownMenu>
          <DropdownMenuTrigger asChild>
            <Button
              size="icon-sm"
              variant="ghost"
              aria-label={`${ch.name} options`}
              className="ml-auto pointer-coarse:size-10"
            >
              <MoreHorizontal />
            </Button>
          </DropdownMenuTrigger>
          <DropdownMenuContent align="end">
            <DropdownMenuItem onSelect={onEdit}>
              <Pencil /> Edit
            </DropdownMenuItem>
            <DropdownMenuSeparator />
            <DropdownMenuItem variant="destructive" onSelect={onDelete}>
              <Trash2 /> Delete
            </DropdownMenuItem>
          </DropdownMenuContent>
        </DropdownMenu>
      </div>
    </article>
  )
}

export function ChannelsPage() {
  const qc = useQueryClient()
  const channels = useChannels()
  const collections = useCollections(true)
  const [formOpen, setFormOpen] = useState(false)
  const [editing, setEditing] = useState<NotificationChannel | null>(null)
  const [deleting, setDeleting] = useState<NotificationChannel | null>(null)
  const [deliveriesOf, setDeliveriesOf] = useState<NotificationChannel | null>(null)
  const list = channels.data ?? []
  const collectionName = (id: string) => collections.data?.find((c) => c.id === id)?.name ?? id

  const del = useMutation({
    mutationFn: (id: string) => api.deleteChannel(id),
    onSuccess: () => {
      void qc.invalidateQueries({ queryKey: qk.channels })
      void qc.invalidateQueries({ queryKey: qk.usage })
      toast.success('Channel deleted')
    },
    onError: (e) => toastApiError(e),
  })

  const openNew = () => {
    setEditing(null)
    setFormOpen(true)
  }

  return (
    <PageContainer>
      <SettingsHeader />
      <SettingsNav className="mt-4" />
      <section className="mt-6 grid gap-4" aria-labelledby="channels-heading">
        <div className="flex flex-wrap items-start justify-between gap-3">
          <div className="min-w-0 flex-1 basis-64">
            <h2
              id="channels-heading"
              className="flex flex-wrap items-center gap-2 text-base font-semibold tracking-tight"
            >
              Notification channels {list.length > 0 && <UsageChip metric="notification_channels" noun="channels" />}
            </h2>
            <p className="text-sm text-muted-foreground">
              Telegram, Slack, Discord, ntfy or email — get pinged when something needs review and approve right from
              the message.
            </p>
          </div>
          {list.length > 0 && (
            <Button onClick={openNew}>
              <Plus /> New channel
            </Button>
          )}
        </div>

        {/* How decisions from a channel behave: a note under the section's subtitle. */}
        <div className="flex items-start gap-2 rounded-lg border bg-muted/30 p-3 text-xs text-muted-foreground">
          <Info className="mt-0.5 size-3.5 shrink-0" aria-hidden />
          <p>
            Decisions from chat or links go through the same path as the dashboard: events and webhooks fire as usual.
          </p>
        </div>

        {channels.isLoading ? (
          <div className="grid gap-3 md:grid-cols-2">
            <Skeleton className="h-44 rounded-xl" />
            <Skeleton className="h-44 rounded-xl" />
          </div>
        ) : channels.error ? (
          <ErrorState error={channels.error} onRetry={() => channels.refetch()} />
        ) : !list.length ? (
          <EmptyState
            className="pt-[12vh]"
            icon={<BellRing />}
            title="No notification channels"
            description="Add your phone via Telegram or ntfy and decide on new items with one tap — no dashboard needed."
            action={
              <Button onClick={openNew}>
                <Plus /> New channel
              </Button>
            }
          />
        ) : (
          <div className="grid gap-3 md:grid-cols-2" data-testid="channels-list">
            {list.map((ch) => (
              <ChannelCard
                key={ch.id}
                channel={ch}
                collectionName={collectionName}
                onEdit={() => {
                  setEditing(ch)
                  setFormOpen(true)
                }}
                onDelete={() => setDeleting(ch)}
                onDeliveries={() => setDeliveriesOf(ch)}
              />
            ))}
          </div>
        )}
      </section>

      <ChannelFormSheet open={formOpen} onOpenChange={setFormOpen} channel={editing} />
      <DeliveriesSheet channel={deliveriesOf} onOpenChange={(o) => !o && setDeliveriesOf(null)} />
      <AlertDialog open={!!deleting} onOpenChange={(o) => !o && setDeleting(null)}>
        <AlertDialogContent>
          <AlertDialogHeader>
            <AlertDialogTitle>Delete “{deleting?.name}”?</AlertDialogTitle>
            <AlertDialogDescription>
              It stops receiving notifications and its delivery history is removed.
            </AlertDialogDescription>
          </AlertDialogHeader>
          <AlertDialogFooter>
            <AlertDialogCancel>Cancel</AlertDialogCancel>
            <AlertDialogAction variant="destructive" onClick={() => deleting && del.mutate(deleting.id)}>
              Delete channel
            </AlertDialogAction>
          </AlertDialogFooter>
        </AlertDialogContent>
      </AlertDialog>
    </PageContainer>
  )
}
