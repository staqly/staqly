'use client'

import { useMutation, useQueryClient } from '@tanstack/react-query'
import { Ban, CheckCheck, Ellipsis, XCircle } from 'lucide-react'
import Link from 'next/link'
import { useState } from 'react'
import { toast } from 'sonner'

import { Button } from '@/components/ui/button'
import { DropdownMenu, DropdownMenuContent, DropdownMenuItem, DropdownMenuTrigger } from '@/components/ui/dropdown-menu'
import { Popover, PopoverContent, PopoverTrigger } from '@/components/ui/popover'
import { useNow } from '@/hooks/use-now'
import { api, type Item } from '@/lib/api'
import { formatDateTime, formatInterval, parseDate } from '@/lib/format'
import { namedToast, toastApiError } from '@/lib/notify'
import { invalidateAfterItemChange, syncItemInCache, useSession } from '@/lib/queries'
import { CANCELED_ERROR, isCanceled } from '@/lib/status'
import { TONES } from '@/lib/tones'
import { cn } from '@/lib/utils'

/** 5 s, 3 min 05 s, 1 h 02 min: precise while it's short, calmer once it's long. */
function formatElapsed(seconds: number) {
  const s = Math.floor(seconds)

  if (s < 60) {
    return `${s} s`
  }

  if (s < 3600) {
    return `${Math.floor(s / 60)} min ${String(s % 60).padStart(2, '0')} s`
  }

  return `${Math.floor(s / 3600)} h ${String(Math.floor((s % 3600) / 60)).padStart(2, '0')} min`
}

const LIMITS = [900, 1800, 3600, 7200, 14400, 28800, 43200, 86400]

/** "Change the limit": this item's own processing limit, or back to the workspace's. */
function LimitPicker({ item, workspaceLimit }: { item: Item; workspaceLimit: number }) {
  const qc = useQueryClient()
  const [open, setOpen] = useState(false)
  const save = useMutation({
    mutationFn: (seconds: number) => api.updateItem(item.id, { processing_timeout_seconds: seconds }),
    onSuccess: (updated) => {
      syncItemInCache(qc, updated)
      setOpen(false)
      toast.success(
        updated.processing_timeout_seconds
          ? `This item now waits up to ${formatInterval(updated.processing_timeout_seconds)}`
          : 'This item uses the workspace limit again',
      )
    },
    onError: (e) => toastApiError(e),
  })
  const current = item.processing_timeout_seconds ?? 0

  return (
    <Popover open={open} onOpenChange={setOpen}>
      <PopoverTrigger asChild>
        <button type="button" className="text-foreground underline underline-offset-2 hover:no-underline">
          Change the limit
        </button>
      </PopoverTrigger>
      <PopoverContent align="start" className="grid w-64 gap-1 p-1.5 text-sm">
        <p className="px-2 pt-1 pb-1.5 text-xs text-muted-foreground">Fail this item if nothing is reported within</p>
        {[0, ...LIMITS].map((s) => (
          <button
            key={s}
            type="button"
            disabled={save.isPending}
            onClick={() => save.mutate(s)}
            className={cn(
              'flex items-center justify-between rounded-md px-2 py-1.5 text-left hover:bg-muted',
              current === s && 'bg-muted font-medium',
            )}
          >
            {s === 0 ? `Workspace default (${formatInterval(workspaceLimit)})` : formatInterval(s)}
            {current === s && <CheckCheck className="size-3.5 text-muted-foreground" />}
          </button>
        ))}
        <Link
          href="/settings"
          className="px-2 pt-1.5 pb-1 text-xs text-muted-foreground underline underline-offset-2 hover:no-underline"
        >
          Change the workspace default
        </Link>
      </PopoverContent>
    </Popover>
  )
}

type Report = 'completed' | 'failed' | 'canceled'

/** The results a person can report, as buttons (the note) and menu rows (inbox rows). */
const REPORTS: { status: Report; label: string; Icon: typeof CheckCheck; className: string }[] = [
  { status: 'completed', label: 'Completed', Icon: CheckCheck, className: TONES.success.text },
  { status: 'failed', label: 'Failed', Icon: XCircle, className: TONES.danger.text },
  { status: 'canceled', label: 'Canceled', Icon: Ban, className: TONES.neutral.text },
]

/** Report a processing item's result by hand (the automation never did). */
function useReportResult(item: Item) {
  const qc = useQueryClient()
  const report = useMutation({
    mutationFn: (status: Report) =>
      api.reportResult(
        item.id,
        status === 'canceled'
          ? { status: 'failed', error: CANCELED_ERROR }
          : status === 'failed'
            ? { status, error: 'Marked as failed in the dashboard' }
            : { status },
      ),
    onSuccess: (updated) => {
      syncItemInCache(qc, updated)
      invalidateAfterItemChange(qc, updated.id)
      namedToast(updated.title, isCanceled(updated) ? 'was canceled' : `was marked ${updated.status}`)
    },
    onError: (e) => toastApiError(e),
  })

  return { pending: report.isPending, onReport: report.mutate }
}

/** How long it has waited, of which limit, and when it fails; ticks every second. */
function useWaiting(item: Item) {
  const now = useNow()
  const workspaceLimit = useSession().data?.workspace.processing_timeout_seconds ?? 0
  const limit = item.processing_timeout_seconds ?? workspaceLimit
  const since = parseDate(item.updated_at)?.getTime() ?? now
  const waited = Math.max(0, (now - since) / 1000)
  const deadline = new Date(since + limit * 1000).toISOString()

  return { workspaceLimit, limit, waited, deadline }
}

function WaitBar({ waited, limit, className }: { waited: number; limit: number; className?: string }) {
  return (
    <div
      className={cn('h-1 overflow-hidden rounded-full', TONES.info.chip, className)}
      role="progressbar"
      aria-label="Time waited of the processing limit"
      aria-valuemin={0}
      aria-valuemax={limit}
      aria-valuenow={Math.min(limit, Math.round(waited))}
    >
      <div className={cn('h-full', TONES.info.dot)} style={{ width: `${Math.min(100, (waited / limit) * 100)}%` }} />
    </div>
  )
}

function ReportButtons({ item, size = 'sm' }: { item: Item; size?: 'sm' | 'xs' }) {
  const { pending, onReport } = useReportResult(item)

  return (
    <div className="flex gap-1.5">
      {REPORTS.map(({ status, label, Icon, className }) => (
        <Button key={status} size={size} variant="outline" disabled={pending} onClick={() => onReport(status)}>
          <Icon className={className} /> {label}
        </Button>
      ))}
    </div>
  )
}

/** Inbox row, time column: "22 s of 1 h" over a thin bar; the deadline on hover. */
export function ProcessingWait({ item }: { item: Item }) {
  const { limit, waited, deadline } = useWaiting(item)

  return (
    <div
      className="grid gap-1"
      title={limit > 0 ? `Fails automatically at ${formatDateTime(deadline)}` : 'No time limit'}
      data-testid="processing-wait"
    >
      <span className="text-sm text-foreground tabular-nums">
        {formatElapsed(waited)}
        {limit > 0 && <span className="text-xs text-muted-foreground"> of {formatInterval(limit)}</span>}
      </span>
      {limit > 0 && <WaitBar waited={waited} limit={limit} />}
    </div>
  )
}

/** Inbox row, actions column: one small menu to report the result by hand. */
export function ProcessingReport({ item }: { item: Item }) {
  const { pending, onReport } = useReportResult(item)

  return (
    <DropdownMenu>
      <DropdownMenuTrigger asChild>
        <Button size="xs" variant="outline" disabled={pending} aria-label="Report result">
          <Ellipsis />
        </Button>
      </DropdownMenuTrigger>
      <DropdownMenuContent align="end">
        {REPORTS.map(({ status, label, Icon, className }) => (
          <DropdownMenuItem key={status} onSelect={() => onReport(status)}>
            <Icon className={className} /> {label}
          </DropdownMenuItem>
        ))}
      </DropdownMenuContent>
    </DropdownMenu>
  )
}

/**
 * An item waiting in "processing": how long it has waited, the workspace's limit after which it fails, and a
 * way to report the result by hand if the automation never does.
 */
export function ProcessingNote({ item }: { item: Item }) {
  const { workspaceLimit, limit, waited, deadline } = useWaiting(item)

  return (
    <div
      className={cn('grid basis-full gap-2 rounded-xl border px-3 py-2.5', TONES.info.callout)}
      data-testid="processing-note"
    >
      <div className="flex flex-wrap items-center gap-3">
        <div className="min-w-0 flex-1 text-sm">
          <div className="font-medium">Waiting for your automation</div>
          <div className="text-xs text-muted-foreground" data-testid="processing-timing">
            <span title={`Since ${formatDateTime(item.updated_at)}`} className="tabular-nums">
              Waiting {formatElapsed(waited)}
            </span>
            {limit > 0 ? (
              <>
                {' '}
                of {formatInterval(limit)} · fails automatically at{' '}
                <time dateTime={deadline}>{formatDateTime(deadline)}</time>.{' '}
                <LimitPicker item={item} workspaceLimit={workspaceLimit} />
              </>
            ) : (
              ' · no time limit'
            )}
          </div>
        </div>
        <ReportButtons item={item} />
      </div>
      {limit > 0 && <WaitBar waited={waited} limit={limit} />}
    </div>
  )
}
