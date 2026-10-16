'use client'

import { useMutation, useQueryClient } from '@tanstack/react-query'
import {
  AlertTriangle,
  ArrowRight,
  Clock,
  Inbox,
  MoreHorizontal,
  Pause,
  Pencil,
  Play,
  Plus,
  Radar,
  RefreshCw,
  Trash2,
} from 'lucide-react'
import Link from 'next/link'
import { useRouter } from 'next/navigation'
import { memo, useState } from 'react'
import { toast } from 'sonner'

import { Callout } from '@/components/common/callout'
import { EmptyState, ErrorState } from '@/components/common/error-state'
import { PageContainer, PageHeader } from '@/components/common/page-header'
import { UsageChip } from '@/components/plans/usage-chip'
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
import { api, type Watcher } from '@/lib/api'
import { formatDateTime, formatInterval, hostOf, pluralize, timeAgo } from '@/lib/format'
import { toastApiError } from '@/lib/notify'
import { invalidateAfterItemChange, qk, refetchItemListsFromStart, useCollections, useWatchers } from '@/lib/queries'
import { safeHttpUrl } from '@/lib/safe'
import { cn } from '@/lib/utils'
import { runResultMessage } from '@/lib/watchers'

import { KIND_LABELS, SourceIcon, WatcherStatusBadge } from './watcher-shared'
import { WatcherSheet, type WatcherSheetState } from './watcher-sheet'

// memo: the list polls every 20 s; a check that changes one watcher re-renders only its card. Callbacks take the
// watcher, so the page passes stable functions.
const WatcherCard = memo(function WatcherCard({
  watcher: w,
  collectionName,
  onEdit,
  onDelete,
}: {
  watcher: Watcher
  collectionName: (id: string) => string
  onEdit: (w: Watcher) => void
  onDelete: (w: Watcher) => void
}) {
  const qc = useQueryClient()
  const router = useRouter()
  const run = useMutation({
    mutationFn: () => api.runWatcher(w.id),
    onSuccess: (r) => {
      const m = runResultMessage(r)

      if (m.tone === 'error') {
        toast.error(m.title, { description: m.description })
      } else if (m.tone === 'info') {
        toast.info(m.title, { description: m.description })
      } else {
        toast.success(m.title, {
          description: m.description,
          action: r.new_items ? { label: 'Open Inbox', onClick: () => router.push('/inbox') } : undefined,
        })
      }

      void qc.invalidateQueries({ queryKey: qk.watchers })

      if (r.new_items) {
        // New items: lists reload from their first page, not every loaded page.
        refetchItemListsFromStart(qc)
        invalidateAfterItemChange(qc, undefined, { usage: true })
      }
    },
    onError: (e) => toastApiError(e, 'Check failed'),
  })
  const setStatus = useMutation({
    mutationFn: (status: 'active' | 'paused') => api.updateWatcher(w.id, { status }),
    onSuccess: (nw) => {
      void qc.invalidateQueries({ queryKey: qk.watchers })
      void qc.invalidateQueries({ queryKey: qk.usage })
      toast.success(nw.status === 'paused' ? `Paused ${nw.name}` : `Resumed ${nw.name}`)
    },
    onError: (e) => toastApiError(e),
  })
  const paused = w.status === 'paused'

  return (
    <li
      data-testid="watcher-card"
      className={cn('grid gap-3 rounded-xl border bg-card p-4 shadow-xs', paused && 'bg-muted/30')}
    >
      <div className="flex items-start gap-3">
        <SourceIcon kind={w.kind} className={cn(paused && 'opacity-60 grayscale')} />
        <div className="min-w-0 flex-1">
          <div className="flex flex-wrap items-center gap-x-2 gap-y-1">
            <button
              type="button"
              onClick={() => onEdit(w)}
              className="min-w-0 truncate text-left text-sm font-semibold hover:underline"
            >
              {w.name}
            </button>
            <WatcherStatusBadge status={w.status} />
          </div>
          <div className="mt-0.5 flex min-w-0 flex-wrap items-center gap-x-1.5 text-xs text-muted-foreground">
            <span className="truncate font-mono" title={w.url}>
              {hostOf(w.url)}
            </span>
            <ArrowRight className="size-3 shrink-0" aria-label="into" />
            {(w.collection_ids ?? [w.collection_id]).map((id, i) => (
              <span key={id} className="flex min-w-0 items-center">
                {i > 0 && <span className="mr-1.5">,</span>}
                <Link href={`/collections/${id}`} className="truncate hover:text-foreground hover:underline">
                  {collectionName(id)}
                </Link>
              </span>
            ))}
          </div>
        </div>
        <DropdownMenu>
          <DropdownMenuTrigger asChild>
            <Button
              size="icon-sm"
              variant="ghost"
              aria-label={`Options for ${w.name}`}
              className="-mt-1 -mr-1 shrink-0"
            >
              <MoreHorizontal />
            </Button>
          </DropdownMenuTrigger>
          <DropdownMenuContent align="end" className="[&_[role=menuitem]]:pointer-coarse:min-h-11">
            <DropdownMenuItem onSelect={() => onEdit(w)}>
              <Pencil /> Edit
            </DropdownMenuItem>
            {safeHttpUrl(w.url) && (
              <DropdownMenuItem asChild>
                <a href={safeHttpUrl(w.url)!} target="_blank" rel="noopener noreferrer nofollow">
                  <ArrowRight /> Open source
                </a>
              </DropdownMenuItem>
            )}
            <DropdownMenuSeparator />
            <DropdownMenuItem variant="destructive" onSelect={() => onDelete(w)}>
              <Trash2 /> Delete
            </DropdownMenuItem>
          </DropdownMenuContent>
        </DropdownMenu>
      </div>

      <dl className="grid grid-cols-3 gap-2 text-xs">
        <div className="min-w-0">
          <dt className="text-muted-foreground">Checks</dt>
          <dd className="truncate font-medium">every {formatInterval(w.every_seconds)}</dd>
        </div>
        <div className="min-w-0">
          <dt className="text-muted-foreground">Last check</dt>
          <dd className="truncate font-medium" title={w.last_run_at ? formatDateTime(w.last_run_at) : undefined}>
            {w.last_run_at ? timeAgo(w.last_run_at) : 'Not yet'}
          </dd>
        </div>
        <div className="min-w-0">
          <dt className="text-muted-foreground">Items</dt>
          <dd className="font-medium tabular-nums" data-testid="watcher-items">
            {w.items_created.toLocaleString('en-US')}
          </dd>
        </div>
      </dl>

      {w.status === 'degraded' && (
        <Callout size="xs" role="status">
          <span className="font-medium">{pluralize(w.fail_count, 'failed check')} in a row.</span>{' '}
          {w.last_error && <span className="text-muted-foreground">{w.last_error}</span>}
        </Callout>
      )}
      {w.status === 'active' && w.fail_count > 0 && w.last_error && (
        <Callout tone="danger" icon={AlertTriangle} size="xs" role="status">
          <span className="block font-medium">Last check failed</span>
          <code className="mt-0.5 line-clamp-3 font-mono text-[11px] break-all text-muted-foreground">
            {w.last_error}
          </code>
        </Callout>
      )}
      {paused && w.last_error && (
        <p className="line-clamp-2 text-xs break-words text-muted-foreground">Paused after errors: {w.last_error}</p>
      )}

      <div className="flex flex-wrap items-center gap-2 border-t pt-3">
        <Button size="sm" variant="outline" onClick={() => run.mutate()} disabled={run.isPending}>
          <RefreshCw className={cn(run.isPending && 'animate-spin')} /> {run.isPending ? 'Checking...' : 'Check now'}
        </Button>
        <Button
          size="sm"
          variant={paused ? 'approve' : 'warning'}
          onClick={() => setStatus.mutate(paused ? 'active' : 'paused')}
          disabled={setStatus.isPending}
        >
          {paused ? <Play /> : <Pause />} {paused ? 'Resume' : 'Pause'}
        </Button>
        <span className="ml-auto inline-flex items-center gap-1 text-[11px] text-muted-foreground">
          {!paused && w.next_run_at ? (
            <>
              <Clock className="size-3" /> next {timeAgo(w.next_run_at)}
            </>
          ) : (
            KIND_LABELS[w.kind]
          )}
        </span>
      </div>
    </li>
  )
})

export function WatchersPage() {
  const qc = useQueryClient()
  const watchers = useWatchers({ refetchInterval: 20_000 })
  const collections = useCollections(true)
  const [sheetOpen, setSheetOpen] = useState(false)
  const [sheet, setSheet] = useState<WatcherSheetState>({ mode: 'add' })
  const [deleting, setDeleting] = useState<Watcher | null>(null)
  const list = watchers.data ?? []
  const collectionName = (id: string) => collections.data?.find((c) => c.id === id)?.name ?? 'Unknown collection'
  const openAdd = () => {
    setSheet({ mode: 'add' })
    setSheetOpen(true)
  }
  const openEdit = (watcher: Watcher) => {
    setSheet({ mode: 'edit', watcher })
    setSheetOpen(true)
  }

  const del = useMutation({
    mutationFn: (id: string) => api.deleteWatcher(id),
    onSuccess: () => {
      void qc.invalidateQueries({ queryKey: qk.watchers })
      void qc.invalidateQueries({ queryKey: qk.usage })
      toast.success('Watcher deleted', { description: 'Items it already created are kept.' })
    },
    onError: (e) => toastApiError(e),
  })

  return (
    <PageContainer>
      <PageHeader
        title="Watchers"
        icon={<Radar className="size-4" />}
        description="Follow feeds and pages; new entries arrive as pending items for review."
        actions={
          list.length > 0 && (
            <>
              <UsageChip metric="watchers" noun="watchers" />
              <Button onClick={() => openAdd()}>
                <Plus /> Add watcher
              </Button>
            </>
          )
        }
      />

      <div className="mt-6 grid gap-5">
        {watchers.isLoading ? (
          <div className="grid gap-3">
            <Skeleton className="h-48 rounded-xl" />
            <Skeleton className="h-48 rounded-xl" />
          </div>
        ) : watchers.error ? (
          <ErrorState error={watchers.error} onRetry={() => watchers.refetch()} />
        ) : list.length === 0 ? (
          <EmptyState
            className="pt-[12vh]"
            icon={<Radar />}
            title="No watchers yet"
            description="Follow any RSS/Atom feed, web page or JSON endpoint. staqly checks it on a schedule and drops new entries into a collection."
            action={
              <Button onClick={() => openAdd()}>
                <Plus /> Add watcher
              </Button>
            }
          />
        ) : (
          <>
            <ul className="grid gap-3" data-testid="watchers-list">
              {list.map((w) => (
                <WatcherCard
                  key={w.id}
                  watcher={w}
                  collectionName={collectionName}
                  onEdit={openEdit}
                  onDelete={setDeleting}
                />
              ))}
            </ul>
            <p className="flex items-center gap-2 text-xs text-muted-foreground">
              <Inbox className="size-3.5 shrink-0" />
              New entries are marked External content — open links with care.
            </p>
          </>
        )}
      </div>

      <WatcherSheet open={sheetOpen} onOpenChange={setSheetOpen} state={sheet} />
      <AlertDialog open={!!deleting} onOpenChange={(o) => !o && setDeleting(null)}>
        <AlertDialogContent>
          <AlertDialogHeader>
            <AlertDialogTitle>Delete “{deleting?.name}”?</AlertDialogTitle>
            <AlertDialogDescription>
              staqly stops checking {deleting ? hostOf(deleting.url) : 'this source'}. Items it already created stay in
              their collection.
            </AlertDialogDescription>
          </AlertDialogHeader>
          <AlertDialogFooter>
            <AlertDialogCancel>Cancel</AlertDialogCancel>
            <AlertDialogAction variant="destructive" onClick={() => deleting && del.mutate(deleting.id)}>
              Delete watcher
            </AlertDialogAction>
          </AlertDialogFooter>
        </AlertDialogContent>
      </AlertDialog>
    </PageContainer>
  )
}
