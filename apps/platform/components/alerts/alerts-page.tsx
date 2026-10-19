'use client'

import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { BellRing, CircleAlert, History, MoreHorizontal, Pencil, Plus, Power, Send, Trash2 } from 'lucide-react'
import dynamic from 'next/dynamic'
import Link from 'next/link'
import { useState } from 'react'
import { toast } from 'sonner'

import { Callout } from '@/components/common/callout'
import { ConfirmDialog } from '@/components/common/confirm-dialog'
import { EmptyState, ErrorState } from '@/components/common/error-state'
import { PageContainer, PageHeader } from '@/components/common/page-header'
import { UsageChip } from '@/components/plans/usage-chip'
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
import { useLazyDialog } from '@/hooks/use-lazy-dialog'
import { CHANNEL_TYPE_INFO, channelHealth, channelTarget, digestLabel } from '@/lib/alerts'
import { api, type Alert, type AlertDelivery } from '@/lib/api'
import { formatDateTime, pluralize, timeAgo } from '@/lib/format'
import { toastApiError } from '@/lib/notify'
import { qk, useAlerts, useCollections } from '@/lib/queries'
import { TONES } from '@/lib/tones'
import { cn } from '@/lib/utils'

import { ChannelTypeIcon } from './alert-type-icon'

// The add/edit sheet (every channel's fields and setup guides) loads on first open, not with the page.
const ChannelFormSheet = dynamic(() => import('./alert-form-sheet').then((m) => m.ChannelFormSheet), { ssr: false })

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
    mutationFn: (ch: Alert) => api.testAlert(ch.id),
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

function DeliveriesSheet({ channel, onOpenChange }: { channel: Alert | null; onOpenChange: (o: boolean) => void }) {
  const open = !!channel
  const deliveries = useQuery({
    queryKey: qk.alertDeliveries(channel?.id ?? ''),
    queryFn: async () => (await api.alertDeliveries(channel!.id)).data ?? [],
    enabled: open,
    refetchInterval: open ? 5000 : false,
  })
  const list: AlertDelivery[] = deliveries.data ?? []

  return (
    <Sheet open={open} onOpenChange={onOpenChange}>
      <SheetContent className="gap-0 data-[side=right]:w-full data-[side=right]:sm:max-w-lg">
        <SheetHeader className="border-b pt-[max(1rem,env(safe-area-inset-top))]">
          <SheetTitle>Deliveries</SheetTitle>
          <SheetDescription className="truncate">{channel?.name} · recent alerts, refreshes every 5s</SheetDescription>
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
            <ul className="grid grid-cols-1 gap-2" data-testid="channel-deliveries">
              {list.map((d) => (
                <li key={d.id} className="grid grid-cols-1 gap-1.5 rounded-xl border bg-card p-3 text-sm">
                  <div className="flex items-center gap-2">
                    <span className="min-w-0 flex-1 truncate font-mono text-xs">{d.event_type}</span>
                    <Pill cls={cn('capitalize', DELIVERY_STATUS[d.status ?? ''] ?? DELIVERY_STATUS.skipped)}>
                      {d.status}
                    </Pill>
                  </div>
                  <div className="flex flex-wrap gap-x-3 gap-y-0.5 text-xs text-muted-foreground">
                    <span title={formatDateTime(d.created_at)}>{timeAgo(d.created_at)}</span>
                    <span className="tabular-nums">{pluralize(d.attempts ?? 0, 'attempt')}</span>
                    {d.status === 'sent' && d.sent_at && <span>sent {timeAgo(d.sent_at)}</span>}
                    {d.status === 'pending' && d.next_attempt_at && <span>next {timeAgo(d.next_attempt_at)}</span>}
                    {d.post_id && (
                      <Link prefetch={false} href={`/posts/${d.post_id}`} className="text-brand hover:underline">
                        Open post
                      </Link>
                    )}
                  </div>
                  {d.last_error && <p className={cn('text-xs wrap-anywhere', TONES.danger.text)}>{d.last_error}</p>}
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
  channel: Alert
  collectionName: (id: string) => string
  onEdit: () => void
  onDelete: () => void
  onDeliveries: () => void
}) {
  const qc = useQueryClient()
  const health = channelHealth(ch)
  const test = useTestChannel()
  const update = useMutation({
    mutationFn: (enabled: boolean) => api.updateAlert(ch.id, { enabled }),
    onSuccess: (nc, enabled) => {
      void qc.invalidateQueries({ queryKey: qk.alerts })
      toast.success(enabled ? (ch.disabled_reason ? 'Channel re-enabled' : 'Channel enabled') : 'Channel paused', {
        description: enabled && nc.last_error ? nc.last_error : undefined,
      })
    },
    onError: (e) => toastApiError(e),
  })

  return (
    <article
      className={cn(
        'grid grid-cols-1 content-start gap-3 rounded-xl border bg-card p-4',
        health === 'paused' && 'opacity-80',
      )}
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
          {pluralize(ch.fail_count, 'failed send')} in a row: {ch.last_error}
        </Callout>
      )}

      <div className="flex flex-wrap gap-1">
        {ch.events.map((e) => (
          <span key={e} className="max-w-full rounded bg-muted px-1.5 py-0.5 font-mono text-[11px] wrap-anywhere">
            {e}
          </span>
        ))}
        <span className="rounded px-1.5 py-0.5 text-[11px] text-muted-foreground">
          {ch.collection_ids.length ? `in ${ch.collection_ids.map(collectionName).join(', ')}` : 'all collections'}
        </span>
      </div>

      <div className="flex flex-wrap items-center gap-x-3 gap-y-1 text-xs text-muted-foreground">
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

export function AlertsPage() {
  const qc = useQueryClient()
  const channels = useAlerts()
  const collections = useCollections(true)
  const { open: formOpen, used: formUsed, setOpen: setFormOpen } = useLazyDialog()
  const [editing, setEditing] = useState<Alert | null>(null)
  const [deleting, setDeleting] = useState<Alert | null>(null)
  const [deliveriesOf, setDeliveriesOf] = useState<Alert | null>(null)
  const list = channels.data ?? []
  const collectionName = (id: string) => collections.data?.find((c) => c.id === id)?.name ?? id

  const del = useMutation({
    mutationFn: (id: string) => api.deleteAlert(id),
    onSuccess: () => {
      void qc.invalidateQueries({ queryKey: qk.alerts })
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
      <PageHeader
        title="Alerts"
        icon={<BellRing className="size-4" />}
        description="Telegram, Slack, Discord, ntfy or email: get pinged when a post goes out, fails, or a channel needs reconnecting."
        actions={
          list.length > 0 && (
            <>
              <UsageChip metric="alerts" noun="alerts" />
              <Button onClick={openNew}>
                <Plus /> New alert
              </Button>
            </>
          )
        }
      />
      <section className="mt-6 grid grid-cols-1 gap-4" aria-label="Alerts">
        {/* How decisions from a channel behave: a note under the section's subtitle. */}
        <Callout tone="neutral" size="xs" className="text-muted-foreground">
          Decisions from chat or links go through the same path as the dashboard: events and webhooks fire as usual.
        </Callout>

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
            title="No alerts yet"
            description="Add your phone via Telegram or ntfy and know the moment a post is out or a channel needs attention."
            action={
              <Button onClick={openNew}>
                <Plus /> New alert
              </Button>
            }
          />
        ) : (
          <div className="grid grid-cols-1 gap-3 md:grid-cols-2" data-testid="channels-list">
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

      {formUsed && <ChannelFormSheet open={formOpen} onOpenChange={setFormOpen} channel={editing} />}
      <DeliveriesSheet channel={deliveriesOf} onOpenChange={(o) => !o && setDeliveriesOf(null)} />
      <ConfirmDialog
        open={!!deleting}
        onOpenChange={(o) => !o && setDeleting(null)}
        title={<>Delete “{deleting?.name}”?</>}
        description="It stops receiving alerts and its delivery history is removed."
        confirmLabel="Delete channel"
        variant="destructive"
        onConfirm={() => deleting && del.mutate(deleting.id)}
      />
    </PageContainer>
  )
}
