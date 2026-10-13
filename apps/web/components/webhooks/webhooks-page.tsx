'use client'

import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { MoreHorizontal, Pencil, Plus, RefreshCw, RotateCw, Send, Trash2, Webhook as WebhookIcon } from 'lucide-react'
import { useState } from 'react'
import { toast } from 'sonner'

import { EmptyState, ErrorState } from '@/components/common/error-state'
import { PageContainer, PageHeader } from '@/components/common/page-header'
import { SecretField } from '@/components/common/secret-field'
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
import { Skeleton } from '@/components/ui/skeleton'
import { Switch } from '@/components/ui/switch'
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from '@/components/ui/table'
import { Tooltip, TooltipContent, TooltipTrigger } from '@/components/ui/tooltip'
import { useIsMobile } from '@/hooks/use-mobile'
import { api, type Delivery, type Webhook } from '@/lib/api'
import { formatDateTime, timeAgo, truncate } from '@/lib/format'
import { toastApiError } from '@/lib/notify'
import { qk, useCollections } from '@/lib/queries'
import { cn } from '@/lib/utils'

import { WebhookFormDialog } from './webhook-form-dialog'

const DELIVERY_STATUS: Record<string, string> = {
  pending: 'bg-sky-500/12 text-sky-700 ring-sky-500/25 dark:text-sky-300',
  succeeded: 'bg-emerald-500/12 text-emerald-700 ring-emerald-500/25 dark:text-emerald-300',
  failed: 'bg-rose-500/12 text-rose-700 ring-rose-500/25 dark:text-rose-300',
}

function DeliveryBadge({ status }: { status: string }) {
  return (
    <span
      className={cn(
        'inline-flex h-5 items-center rounded-md px-1.5 text-[11px] font-medium capitalize ring-1 ring-inset',
        DELIVERY_STATUS[status] ?? 'bg-muted text-muted-foreground ring-border',
      )}
    >
      {status}
    </span>
  )
}

export function DeliveriesTable({ webhookId }: { webhookId: string }) {
  const qc = useQueryClient()
  const isMobile = useIsMobile()
  const deliveries = useQuery({
    queryKey: qk.deliveries(webhookId),
    queryFn: async () => (await api.deliveries({ webhook_id: webhookId, limit: 50 })).data ?? [],
    refetchInterval: 3000,
  })
  const retry = useMutation({
    mutationFn: (id: string) => api.retryDelivery(id),
    onSuccess: () => {
      toast.success('Delivery re-queued')
      void qc.invalidateQueries({ queryKey: qk.deliveries(webhookId) })
    },
    onError: (e) => toastApiError(e),
  })

  if (deliveries.isLoading) {
    return <Skeleton className="h-32" />
  }

  if (deliveries.error) {
    return <ErrorState error={deliveries.error} onRetry={() => deliveries.refetch()} />
  }

  const list: Delivery[] = deliveries.data ?? []

  if (!list.length) {
    return (
      <div className="rounded-lg border border-dashed p-6 text-center text-sm text-muted-foreground">
        No deliveries yet. Use <strong>Send test</strong> to try the endpoint.
      </div>
    )
  }

  if (isMobile) {
    return (
      <ul className="grid gap-2" data-testid="deliveries-table">
        {list.map((d) => (
          <li key={d.id} className="grid gap-2 rounded-xl border bg-card p-3 text-sm">
            <div className="flex items-center gap-2">
              <span className="min-w-0 flex-1 truncate font-mono text-xs">{d.event_type}</span>
              <DeliveryBadge status={d.status} />
            </div>
            <dl className="grid grid-cols-3 gap-2 text-xs">
              <div>
                <dt className="text-muted-foreground">Response</dt>
                <dd className="font-mono">
                  {d.last_status_code ? (
                    <span className={d.last_status_code < 300 ? 'text-emerald-600' : 'text-rose-600'}>
                      {d.last_status_code}
                    </span>
                  ) : (
                    '—'
                  )}
                </dd>
              </div>
              <div>
                <dt className="text-muted-foreground">Attempts</dt>
                <dd className="tabular-nums">{d.attempts}</dd>
              </div>
              <div>
                <dt className="text-muted-foreground">
                  {d.status === 'pending' && d.next_attempt_at ? 'Next' : 'Created'}
                </dt>
                <dd title={formatDateTime(d.created_at)}>
                  {d.status === 'pending' && d.next_attempt_at ? timeAgo(d.next_attempt_at) : timeAgo(d.created_at)}
                </dd>
              </div>
            </dl>
            {d.last_error && <p className="line-clamp-2 text-xs break-words text-rose-600">{d.last_error}</p>}
            {d.status !== 'pending' && (
              <Button
                size="sm"
                variant="outline"
                onClick={() => retry.mutate(d.id)}
                disabled={retry.isPending}
                aria-label={`Retry delivery ${d.id}`}
              >
                <RefreshCw /> Retry
              </Button>
            )}
          </li>
        ))}
      </ul>
    )
  }

  return (
    <div className="scrollbar-thin overflow-x-auto rounded-lg border" data-testid="deliveries-table">
      <Table>
        <TableHeader>
          <TableRow className="hover:bg-transparent">
            <TableHead>Event</TableHead>
            <TableHead>Status</TableHead>
            <TableHead className="text-right">Attempts</TableHead>
            <TableHead>Response</TableHead>
            <TableHead>Error</TableHead>
            <TableHead>Next attempt</TableHead>
            <TableHead>Created</TableHead>
            <TableHead className="w-10" />
          </TableRow>
        </TableHeader>
        <TableBody>
          {list.map((d) => (
            <TableRow key={d.id}>
              <TableCell className="font-mono text-xs">{d.event_type}</TableCell>
              <TableCell>
                <DeliveryBadge status={d.status} />
              </TableCell>
              <TableCell className="text-right tabular-nums">{d.attempts}</TableCell>
              <TableCell className="font-mono text-xs">
                {d.last_status_code ? (
                  <span className={d.last_status_code < 300 ? 'text-emerald-600' : 'text-rose-600'}>
                    {d.last_status_code}
                  </span>
                ) : (
                  '—'
                )}
              </TableCell>
              <TableCell className="max-w-56">
                {d.last_error ? (
                  <Tooltip>
                    <TooltipTrigger asChild>
                      <span className="block truncate text-xs text-rose-600">{d.last_error}</span>
                    </TooltipTrigger>
                    <TooltipContent className="max-w-sm break-words">
                      {d.last_error}
                      {d.last_response && (
                        <span className="mt-1 block font-mono opacity-80">{truncate(d.last_response, 300)}</span>
                      )}
                    </TooltipContent>
                  </Tooltip>
                ) : (
                  <span className="text-xs text-muted-foreground">—</span>
                )}
              </TableCell>
              <TableCell className="text-xs text-muted-foreground" title={formatDateTime(d.next_attempt_at)}>
                {d.status === 'pending' && d.next_attempt_at ? timeAgo(d.next_attempt_at) : '—'}
              </TableCell>
              <TableCell className="text-xs text-muted-foreground" title={formatDateTime(d.created_at)}>
                {timeAgo(d.created_at)}
              </TableCell>
              <TableCell>
                {d.status !== 'pending' && (
                  <Button
                    size="xs"
                    variant="outline"
                    onClick={() => retry.mutate(d.id)}
                    disabled={retry.isPending}
                    aria-label={`Retry delivery ${d.id}`}
                  >
                    <RefreshCw /> Retry
                  </Button>
                )}
              </TableCell>
            </TableRow>
          ))}
        </TableBody>
      </Table>
    </div>
  )
}

function WebhookCard({
  webhook: w,
  selected,
  onSelect,
  onEdit,
  onDelete,
  collectionName,
}: {
  webhook: Webhook
  selected: boolean
  onSelect: () => void
  onEdit: () => void
  onDelete: () => void
  collectionName: (id: string) => string
}) {
  const qc = useQueryClient()
  const update = useMutation({
    mutationFn: (body: Parameters<typeof api.updateWebhook>[1]) => api.updateWebhook(w.id, body),
    onSuccess: (nw, body) => {
      void qc.invalidateQueries({ queryKey: qk.webhooks })

      if (body.rotate_secret) {
        toast.success('Secret rotated — update your receiver')
      } else {
        toast.success(nw.active ? 'Webhook enabled' : 'Webhook paused')
      }
    },
    onError: (e) => toastApiError(e),
  })
  const test = useMutation({
    mutationFn: () => api.testWebhook(w.id),
    onSuccess: () => {
      toast.success('Test event queued')
      onSelect()
      void qc.invalidateQueries({ queryKey: qk.deliveries(w.id) })
    },
    onError: (e) => toastApiError(e),
  })

  return (
    <div
      className={cn(
        'grid gap-3 rounded-xl border bg-card p-4 transition-colors',
        selected && 'border-brand/50 ring-1 ring-brand/30',
        !w.active && 'opacity-75',
      )}
      data-testid="webhook-card"
    >
      <div className="flex flex-wrap items-start gap-3">
        <button type="button" className="min-w-0 flex-1 basis-56 text-left" onClick={onSelect}>
          <div className="truncate font-mono text-sm">{w.url}</div>
          <div className="text-sm text-muted-foreground">{w.description || 'No description'}</div>
        </button>
        <div className="flex items-center gap-2 max-sm:w-full max-sm:justify-between">
          <Switch
            checked={w.active}
            onCheckedChange={(v) => update.mutate({ active: v })}
            aria-label={w.active ? 'Pause webhook' : 'Enable webhook'}
          />
          <Button size="sm" variant="outline" onClick={() => test.mutate()} disabled={test.isPending}>
            <Send /> Send test
          </Button>
          <DropdownMenu>
            <DropdownMenuTrigger asChild>
              <Button size="icon-sm" variant="ghost" aria-label="Webhook options">
                <MoreHorizontal />
              </Button>
            </DropdownMenuTrigger>
            <DropdownMenuContent align="end">
              <DropdownMenuItem onSelect={onEdit}>
                <Pencil /> Edit
              </DropdownMenuItem>
              <DropdownMenuItem onSelect={() => update.mutate({ rotate_secret: true })}>
                <RotateCw /> Rotate secret
              </DropdownMenuItem>
              <DropdownMenuSeparator />
              <DropdownMenuItem variant="destructive" onSelect={onDelete}>
                <Trash2 /> Delete
              </DropdownMenuItem>
            </DropdownMenuContent>
          </DropdownMenu>
        </div>
      </div>
      <div className="flex flex-wrap gap-1">
        {w.events.map((e) => (
          <span key={e} className="rounded bg-muted px-1.5 py-0.5 font-mono text-[11px]">
            {e}
          </span>
        ))}
        <span className="rounded px-1.5 py-0.5 text-[11px] text-muted-foreground">
          {w.collection_ids.length ? `in ${w.collection_ids.map(collectionName).join(', ')}` : 'all collections'}
        </span>
      </div>
      <div className="grid gap-1.5">
        <div className="text-xs text-muted-foreground">Signing secret</div>
        <SecretField value={w.secret} label="Signing secret" masked />
      </div>
      <div className="text-xs text-muted-foreground">
        Timeout {w.timeout_ms / 1000}s · up to {w.max_attempts} attempts · updated {timeAgo(w.updated_at)}
      </div>
    </div>
  )
}

export function WebhooksPage() {
  const qc = useQueryClient()
  const webhooks = useQuery({ queryKey: qk.webhooks, queryFn: async () => (await api.webhooks()).data ?? [] })
  const collections = useCollections(true)
  const [selectedId, setSelectedId] = useState<string | null>(null)
  const [editing, setEditing] = useState<Webhook | null>(null)
  const [formOpen, setFormOpen] = useState(false)
  const [deleting, setDeleting] = useState<Webhook | null>(null)

  const list = webhooks.data ?? []
  const selected = list.find((w) => w.id === selectedId) ?? list[0]
  const collectionName = (id: string) => collections.data?.find((c) => c.id === id)?.name ?? id

  const del = useMutation({
    mutationFn: (id: string) => api.deleteWebhook(id),
    onSuccess: () => {
      void qc.invalidateQueries({ queryKey: qk.webhooks })
      toast.success('Webhook deleted')
    },
    onError: (e) => toastApiError(e),
  })

  return (
    <PageContainer>
      <PageHeader
        title="Webhooks"
        icon={<WebhookIcon className="size-4" />}
        description="Notify n8n and your own services when items are created, approved, rejected or acted on."
        actions={
          <Button
            onClick={() => {
              setEditing(null)
              setFormOpen(true)
            }}
          >
            <Plus /> New webhook
          </Button>
        }
      />
      <div className="mt-6 grid gap-6">
        {webhooks.isLoading ? (
          <Skeleton className="h-40" />
        ) : webhooks.error ? (
          <ErrorState error={webhooks.error} onRetry={() => webhooks.refetch()} />
        ) : list.length === 0 ? (
          <EmptyState
            icon={<WebhookIcon />}
            title="No webhooks"
            description="Send review decisions to n8n or any HTTP endpoint. Every request is signed with HMAC-SHA256."
            action={
              <Button onClick={() => setFormOpen(true)}>
                <Plus /> New webhook
              </Button>
            }
          />
        ) : (
          <>
            <div className="grid gap-3 lg:grid-cols-2">
              {list.map((w) => (
                <WebhookCard
                  key={w.id}
                  webhook={w}
                  selected={selected?.id === w.id}
                  onSelect={() => setSelectedId(w.id)}
                  onEdit={() => {
                    setEditing(w)
                    setFormOpen(true)
                  }}
                  onDelete={() => setDeleting(w)}
                  collectionName={collectionName}
                />
              ))}
            </div>
            {selected && (
              <section className="grid gap-3">
                <div className="flex flex-wrap items-baseline justify-between gap-x-2 gap-y-1">
                  <h2 className="min-w-0 text-sm font-semibold">
                    Deliveries{' '}
                    <span className="block truncate font-mono text-xs font-normal text-muted-foreground sm:inline">
                      {selected.url}
                    </span>
                  </h2>
                  <span className="text-xs text-muted-foreground">Refreshes every 3s</span>
                </div>
                <DeliveriesTable webhookId={selected.id} />
              </section>
            )}
          </>
        )}
      </div>

      <WebhookFormDialog
        open={formOpen}
        onOpenChange={setFormOpen}
        webhook={editing}
        onSaved={(w) => setSelectedId(w.id)}
      />
      <AlertDialog open={!!deleting} onOpenChange={(o) => !o && setDeleting(null)}>
        <AlertDialogContent>
          <AlertDialogHeader>
            <AlertDialogTitle>Delete this webhook?</AlertDialogTitle>
            <AlertDialogDescription>
              {deleting?.url} will stop receiving events and its delivery history is removed.
            </AlertDialogDescription>
          </AlertDialogHeader>
          <AlertDialogFooter>
            <AlertDialogCancel>Cancel</AlertDialogCancel>
            <AlertDialogAction variant="destructive" onClick={() => deleting && del.mutate(deleting.id)}>
              Delete webhook
            </AlertDialogAction>
          </AlertDialogFooter>
        </AlertDialogContent>
      </AlertDialog>
    </PageContainer>
  )
}
