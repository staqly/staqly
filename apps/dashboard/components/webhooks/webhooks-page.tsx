'use client'

import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { MoreHorizontal, Pencil, Plus, RefreshCw, RotateCw, Send, Trash2, Webhook as WebhookIcon } from 'lucide-react'
import dynamic from 'next/dynamic'
import { useState } from 'react'
import { toast } from 'sonner'

import { ConfirmDialog } from '@/components/common/confirm-dialog'
import { EmptyState, ErrorState } from '@/components/common/error-state'
import { PageContainer, PageHeader } from '@/components/common/page-header'
import { SecretField } from '@/components/common/secret-field'
import { Button } from '@/components/ui/button'
import { Dialog, DialogContent, DialogDescription, DialogHeader, DialogTitle } from '@/components/ui/dialog'
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
import { useLazyDialog } from '@/hooks/use-lazy-dialog'
import { useIsMobile } from '@/hooks/use-mobile'
import { api, type Delivery, type Webhook } from '@/lib/api'
import { formatDateTime, timeAgo } from '@/lib/format'
import { toastApiError } from '@/lib/notify'
import { qk, useCollections, useWebhooks } from '@/lib/queries'
import { TONES } from '@/lib/tones'
import { cn } from '@/lib/utils'

// The add/edit sheet loads on first open, not with the page.
const WebhookFormSheet = dynamic(() => import('./webhook-form-sheet').then((m) => m.WebhookFormSheet), { ssr: false })

const DELIVERY_STATUS: Record<string, string> = {
  pending: TONES.info.badge,
  succeeded: TONES.success.badge,
  failed: TONES.danger.badge,
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

function ResponseCode({ code }: { code: number | null }) {
  return code ? <span className={code < 300 ? TONES.success.text : TONES.danger.text}>{code}</span> : <>—</>
}

/** Everything about one delivery: the full error and response body don't fit in a table cell. */
function DeliveryDialog({
  delivery: d,
  onOpenChange,
  onRetry,
  retrying,
}: {
  delivery: Delivery | null
  onOpenChange: (open: boolean) => void
  onRetry: (id: string) => void
  retrying: boolean
}) {
  return (
    <Dialog open={!!d} onOpenChange={onOpenChange}>
      <DialogContent className="sm:max-w-2xl">
        {d && (
          <>
            <DialogHeader>
              <DialogTitle className="flex items-center gap-2">
                <span className="font-mono text-base">{d.event_type}</span>
                <DeliveryBadge status={d.status} />
              </DialogTitle>
              <DialogDescription className="font-mono text-xs">{d.id}</DialogDescription>
            </DialogHeader>
            <dl className="grid grid-cols-2 gap-3 text-sm sm:grid-cols-4">
              <div>
                <dt className="text-xs text-muted-foreground">Response</dt>
                <dd className="font-mono">
                  <ResponseCode code={d.last_status_code} />
                </dd>
              </div>
              <div>
                <dt className="text-xs text-muted-foreground">Attempts</dt>
                <dd className="tabular-nums">{d.attempts}</dd>
              </div>
              <div>
                <dt className="text-xs text-muted-foreground">
                  {d.status === 'pending' ? 'Next attempt' : 'Last attempt'}
                </dt>
                <dd>
                  {d.status === 'pending'
                    ? d.next_attempt_at
                      ? timeAgo(d.next_attempt_at)
                      : '—'
                    : d.last_attempt_at
                      ? formatDateTime(d.last_attempt_at)
                      : '—'}
                </dd>
              </div>
              <div>
                <dt className="text-xs text-muted-foreground">Created</dt>
                <dd>{formatDateTime(d.created_at)}</dd>
              </div>
            </dl>
            {d.last_error && (
              <div className="grid gap-1">
                <div className="text-xs text-muted-foreground">Error</div>
                <pre
                  className={cn(
                    'max-h-48 overflow-auto rounded-lg border p-3 font-mono text-xs break-words whitespace-pre-wrap',
                    TONES.danger.callout,
                    TONES.danger.text,
                  )}
                >
                  {d.last_error}
                </pre>
              </div>
            )}
            {d.last_response && (
              <div className="grid gap-1">
                <div className="text-xs text-muted-foreground">Response body (first 2 KB)</div>
                <pre className="max-h-64 overflow-auto rounded-lg border bg-muted/40 p-3 font-mono text-xs break-words whitespace-pre-wrap">
                  {d.last_response}
                </pre>
              </div>
            )}
            {d.status !== 'pending' && (
              <Button variant="outline" className="justify-self-end" onClick={() => onRetry(d.id)} disabled={retrying}>
                <RefreshCw /> Retry
              </Button>
            )}
          </>
        )}
      </DialogContent>
    </Dialog>
  )
}

function DeliveriesTable({ webhookId }: { webhookId: string }) {
  const qc = useQueryClient()
  // Eight columns need a desktop: phones and tablets get cards.
  const isMobile = useIsMobile(1024)
  const [openId, setOpenId] = useState<string | null>(null)
  const deliveries = useQuery({
    queryKey: qk.deliveries(webhookId),
    queryFn: async () => (await api.deliveries({ webhook_id: webhookId, limit: 50 })).data ?? [],
    // Fast while something waits to be delivered, slower otherwise (new deliveries still show up).
    refetchInterval: (q) => ((q.state.data ?? []).some((d) => d.status === 'pending') ? 3000 : 15_000),
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
  // Looked up by id so the dialog follows the 3 s refresh (a pending delivery's attempts, error...).
  const dialog = (
    <DeliveryDialog
      delivery={list.find((d) => d.id === openId) ?? null}
      onOpenChange={(o) => !o && setOpenId(null)}
      onRetry={(id) => retry.mutate(id)}
      retrying={retry.isPending}
    />
  )

  if (!list.length) {
    return (
      <div className="rounded-lg border border-dashed p-6 text-center text-sm text-muted-foreground">
        No deliveries yet. Use <strong>Send test</strong> to try the endpoint.
      </div>
    )
  }

  if (isMobile) {
    return (
      <ul className="grid grid-cols-1 gap-2 md:grid-cols-2" data-testid="deliveries-table">
        {dialog}
        {list.map((d) => (
          // oxlint-disable-next-line jsx-a11y/click-events-have-key-events, jsx-a11y/no-noninteractive-element-interactions -- the Details button is the keyboard path
          <li
            key={d.id}
            className="grid cursor-pointer grid-cols-1 gap-2 rounded-xl border bg-card p-3 text-sm"
            onClick={() => setOpenId(d.id)}
          >
            <div className="flex items-center gap-2">
              <span className="min-w-0 flex-1 truncate font-mono text-xs">{d.event_type}</span>
              <DeliveryBadge status={d.status} />
            </div>
            <dl className="grid grid-cols-3 gap-2 text-xs">
              <div>
                <dt className="text-muted-foreground">Response</dt>
                <dd className="font-mono">
                  <ResponseCode code={d.last_status_code} />
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
            {d.last_error && (
              <p className={cn('line-clamp-2 text-xs break-words', TONES.danger.text)}>{d.last_error}</p>
            )}
            <div className="flex gap-2">
              <Button
                size="sm"
                variant="outline"
                className="flex-1"
                onClick={(e) => {
                  e.stopPropagation()
                  setOpenId(d.id)
                }}
              >
                Details
              </Button>
              {d.status !== 'pending' && (
                <Button
                  size="sm"
                  variant="outline"
                  className="flex-1"
                  onClick={(e) => {
                    e.stopPropagation()
                    retry.mutate(d.id)
                  }}
                  disabled={retry.isPending}
                  aria-label={`Retry delivery ${d.id}`}
                >
                  <RefreshCw /> Retry
                </Button>
              )}
            </div>
          </li>
        ))}
      </ul>
    )
  }

  return (
    <div className="scrollbar-thin overflow-x-auto rounded-lg border" data-testid="deliveries-table">
      {dialog}
      <Table>
        <TableHeader>
          <TableRow className="hover:bg-transparent">
            <TableHead>Event</TableHead>
            <TableHead>Status</TableHead>
            <TableHead className="text-center">Attempts</TableHead>
            <TableHead className="text-center">Response</TableHead>
            <TableHead>Error</TableHead>
            <TableHead>Next attempt</TableHead>
            <TableHead>Created</TableHead>
            <TableHead />
          </TableRow>
        </TableHeader>
        <TableBody>
          {list.map((d) => (
            // The whole row opens the details; Details is the keyboard path.
            <TableRow key={d.id} className="cursor-pointer" onClick={() => setOpenId(d.id)}>
              <TableCell className="font-mono text-xs">{d.event_type}</TableCell>
              <TableCell>
                <DeliveryBadge status={d.status} />
              </TableCell>
              <TableCell className="text-center tabular-nums">{d.attempts}</TableCell>
              <TableCell className="text-center font-mono text-xs">
                <ResponseCode code={d.last_status_code} />
              </TableCell>
              <TableCell className="max-w-56">
                {d.last_error ? (
                  <span className={cn('block truncate text-xs', TONES.danger.text)}>{d.last_error}</span>
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
              <TableCell onClick={(e) => e.stopPropagation()}>
                <div className="flex justify-end gap-1">
                  <Button size="xs" variant="ghost" onClick={() => setOpenId(d.id)}>
                    Details
                  </Button>
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
                </div>
              </TableCell>
            </TableRow>
          ))}
        </TableBody>
      </Table>
    </div>
  )
}

/** The API returns the full signing secret only on create and rotate; elsewhere it is masked. */
const isFullSecret = (s: string) => !!s && !s.includes('*')

function WebhookCard({
  webhook: w,
  selected,
  onSelect,
  onEdit,
  onDelete,
  collectionName,
  freshSecret,
  onSecret,
}: {
  webhook: Webhook
  selected: boolean
  onSelect: () => void
  /** The full secret right after create/rotate, until dismissed. */
  freshSecret?: string
  onSecret: (secret: string | null) => void
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
        if (isFullSecret(nw.secret)) {
          onSecret(nw.secret)
        }

        toast.success('Secret rotated: update your receiver')
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
    // The whole card selects the webhook (its deliveries show below); controls inside keep their own clicks.
    // The title stays a button so keyboard users can select it too.
    // oxlint-disable-next-line jsx-a11y/click-events-have-key-events, jsx-a11y/no-static-element-interactions -- the title button is the keyboard path
    <div
      className={cn(
        'grid cursor-pointer grid-cols-1 gap-3 rounded-xl border bg-card px-4 py-3 transition-colors hover:border-foreground/25 hover:bg-muted/30',
        selected && 'border-brand/50 ring-1 ring-brand/30 hover:border-brand/60',
      )}
      data-testid="webhook-card"
      onClick={(e) => {
        if (
          !(e.target as HTMLElement).closest(
            'button,a,input,label,[role=switch],[role=menuitem],[data-slot=copy-button]',
          )
        ) {
          onSelect()
        }
      }}
    >
      <div className="flex flex-wrap items-center gap-x-5 gap-y-3">
        {/* On / off first, with its state in words: the first thing to check when nothing arrives. */}
        <label className="flex w-24 shrink-0 cursor-pointer items-center gap-2 text-sm">
          <Switch
            checked={w.active}
            onCheckedChange={(v) => update.mutate({ active: v })}
            disabled={update.isPending}
            aria-label={w.active ? 'Pause webhook' : 'Enable webhook'}
          />
          <span className={cn('font-medium', !w.active && 'text-muted-foreground')}>
            {w.active ? 'Active' : 'Paused'}
          </span>
        </label>
        <button
          type="button"
          className={cn('min-w-0 flex-1 basis-64 text-left', !w.active && 'opacity-70')}
          onClick={onSelect}
        >
          <div className="truncate font-mono text-sm">{w.url}</div>
          <div className="truncate text-sm text-muted-foreground">{w.description || 'No description'}</div>
        </button>
        <div className={cn('grid min-w-0 basis-56 gap-1', !w.active && 'opacity-70')}>
          <div className="flex flex-wrap gap-1">
            {w.events.map((e) => (
              <span key={e} className="max-w-full rounded bg-muted px-1.5 py-0.5 font-mono text-[11px] wrap-anywhere">
                {e}
              </span>
            ))}
          </div>
          <div className="truncate text-xs text-muted-foreground">
            {w.collection_ids.length ? `in ${w.collection_ids.map(collectionName).join(', ')}` : 'All collections'}
          </div>
        </div>
        <div className="hidden w-44 shrink-0 text-xs text-muted-foreground lg:grid">
          <span>
            Timeout {w.timeout_ms / 1000}s · up to {w.max_attempts} attempts
          </span>
          <span
            className="truncate font-mono"
            title="Signing secret, shown in full only when the webhook is created or the secret is rotated. Lost it? Rotate the secret."
          >
            {w.secret}
          </span>
        </div>
        <div className="flex items-center gap-1.5 max-sm:w-full max-sm:justify-end">
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
      {freshSecret && (
        <div className="grid gap-2 rounded-lg border border-brand/40 bg-brand-soft/40 p-3" role="status">
          <div className="text-xs font-medium">Copy the signing secret now. It is not shown again.</div>
          <SecretField value={freshSecret} label="Signing secret" />
          <Button size="xs" variant="outline" className="justify-self-end" onClick={() => onSecret(null)}>
            Done
          </Button>
        </div>
      )}
    </div>
  )
}

export function WebhooksPage() {
  const qc = useQueryClient()
  const webhooks = useWebhooks()
  const collections = useCollections(true)
  const [selectedId, setSelectedId] = useState<string | null>(null)
  const [editing, setEditing] = useState<Webhook | null>(null)
  const [fresh, setFresh] = useState<{ id: string; secret: string } | null>(null)
  const { open: formOpen, used: formUsed, setOpen: setFormOpen } = useLazyDialog()
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

  // Always a blank form: after editing (and deleting) a webhook, `editing` would still hold it.
  function openNew() {
    setEditing(null)
    setFormOpen(true)
  }

  return (
    <PageContainer>
      <PageHeader
        title="Webhooks"
        icon={<WebhookIcon className="size-4" />}
        description="Notify n8n and your own services when items are created, approved, rejected or acted on."
        actions={
          list.length > 0 && (
            <Button onClick={openNew}>
              <Plus /> New webhook
            </Button>
          )
        }
      />
      <div className="mt-6 grid grid-cols-1 gap-6">
        {webhooks.isLoading ? (
          <Skeleton className="h-40" />
        ) : webhooks.error ? (
          <ErrorState error={webhooks.error} onRetry={() => webhooks.refetch()} />
        ) : list.length === 0 ? (
          <EmptyState
            className="pt-[12vh]"
            icon={<WebhookIcon />}
            title="No webhooks"
            description="Send review decisions to n8n or any HTTP endpoint. Every request is signed with HMAC-SHA256."
            action={
              <Button onClick={openNew}>
                <Plus /> New webhook
              </Button>
            }
          />
        ) : (
          <>
            <div className="grid grid-cols-1 gap-2">
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
                  freshSecret={fresh?.id === w.id ? fresh.secret : undefined}
                  onSecret={(secret) => setFresh(secret ? { id: w.id, secret } : null)}
                />
              ))}
            </div>
            {selected && (
              <section className="grid grid-cols-1 gap-3">
                <div className="flex flex-wrap items-center justify-between gap-x-2 gap-y-1">
                  <h2 className="flex min-w-0 items-center gap-2 text-sm font-semibold">
                    Deliveries
                    <span className="min-w-0 truncate rounded-md border bg-muted/50 px-2 py-0.5 font-mono text-xs font-normal text-muted-foreground">
                      {selected.url}
                    </span>
                  </h2>
                  <span className="text-xs text-muted-foreground">Refreshes automatically</span>
                </div>
                <DeliveriesTable webhookId={selected.id} />
              </section>
            )}
          </>
        )}
      </div>

      {formUsed && (
        <WebhookFormSheet
          open={formOpen}
          onOpenChange={setFormOpen}
          webhook={editing}
          onSaved={(w) => {
            setSelectedId(w.id)

            if (isFullSecret(w.secret)) {
              setFresh({ id: w.id, secret: w.secret })
            }
          }}
        />
      )}
      <ConfirmDialog
        open={!!deleting}
        onOpenChange={(o) => !o && setDeleting(null)}
        title="Delete this webhook?"
        description={<>{deleting?.url} will stop receiving events and its delivery history is removed.</>}
        confirmLabel="Delete webhook"
        variant="destructive"
        onConfirm={() => deleting && del.mutate(deleting.id)}
      />
    </PageContainer>
  )
}
