'use client'

import { useMutation, useQueryClient } from '@tanstack/react-query'
import { ListChecks, RotateCcw, X } from 'lucide-react'
import { useState } from 'react'

import { ErrorState } from '@/components/common/error-state'
import { SkeletonList } from '@/components/common/skeleton-list'
import { Button } from '@/components/ui/button'
import { ToggleGroup, ToggleGroupItem } from '@/components/ui/toggle-group'
import { api, type Job } from '@/lib/api'
import { formatDateTime, timeAgo } from '@/lib/format'
import { namedToast, toastApiError } from '@/lib/notify'
import { qk, useJobStats, useJobs } from '@/lib/queries'
import { TONES } from '@/lib/tones'
import { cn } from '@/lib/utils'

import { SettingsCard } from './settings-card'

// The work people start; the rest (Stripe seat sync, alerts, timeouts, token refreshes) is staqly's own and the
// server refuses to cancel it (app.cancellableJobKinds).
const CANCELLABLE = new Set([
  'publish_post',
  'publish_target',
  'run_schedule',
  'repeat_post',
  'publish_reply',
  'repost',
])

const KIND_LABELS: Record<string, string> = {
  publish_post: 'Publish a post',
  publish_target: 'Publish on a channel',
  run_schedule: 'Run a schedule',
  refresh_channel: 'Refresh a channel sign-in',
  target_timeout: 'Time limit for a channel',
}

const STATUS_TONE: Record<string, keyof typeof TONES> = {
  queued: 'info',
  running: 'info',
  done: 'success',
  failed: 'danger',
  cancelled: 'neutral',
}

function JobRow({ job }: { job: Job }) {
  const qc = useQueryClient()
  const refresh = () => {
    void qc.invalidateQueries({ queryKey: qk.jobsAll })
  }
  const retry = useMutation({
    mutationFn: () => api.retryJob(job.id),
    onSuccess: () => {
      refresh()
      namedToast(KIND_LABELS[job.kind] ?? job.kind, 'is queued again')
    },
    onError: (e) => toastApiError(e),
  })
  const cancel = useMutation({
    mutationFn: () => api.cancelJob(job.id),
    onSuccess: () => {
      refresh()
      namedToast(KIND_LABELS[job.kind] ?? job.kind, 'was cancelled')
    },
    onError: (e) => toastApiError(e),
  })
  const tone = TONES[STATUS_TONE[job.status] ?? 'neutral']

  return (
    <li className="grid grid-cols-[minmax(0,1fr)_auto] items-center gap-3 px-3 py-2 text-sm" data-testid="job-row">
      <span className="min-w-0">
        <span className="flex flex-wrap items-center gap-2">
          <span className="font-medium">{KIND_LABELS[job.kind] ?? job.kind}</span>
          <span className={cn('rounded-full px-1.5 text-[11px] font-medium ring-1 ring-inset', tone.badge)}>
            {job.status}
          </span>
          <span className="text-xs text-muted-foreground tabular-nums">
            {job.attempts}/{job.max_attempts} tries
          </span>
        </span>
        <span className="mt-0.5 block text-xs text-muted-foreground" title={formatDateTime(job.run_at)}>
          {job.status === 'queued' ? `Runs ${timeAgo(job.run_at)}` : `Last change ${timeAgo(job.updated_at)}`}
          {job.payload?.post_id && ` · post ${job.payload.post_id}`}
          {job.payload?.channel_id && ` · channel ${job.payload.channel_id}`}
        </span>
        {job.last_error && (
          <span className="mt-0.5 block text-xs text-destructive wrap-anywhere">{job.last_error}</span>
        )}
      </span>
      <span className="flex items-center gap-1.5">
        {job.status === 'failed' && (
          <Button size="sm" variant="outline" onClick={() => retry.mutate()} disabled={retry.isPending}>
            <RotateCcw /> Retry
          </Button>
        )}
        {job.status === 'queued' && CANCELLABLE.has(job.kind) && (
          <Button
            size="sm"
            variant="ghost"
            onClick={() => cancel.mutate()}
            disabled={cancel.isPending}
            aria-label="Cancel job"
          >
            <X />
          </Button>
        )}
      </span>
    </li>
  )
}

/** What the server is doing for you: queued, running and failed jobs, with Retry. Refreshes every few seconds. */
export function QueueCard() {
  const [status, setStatus] = useState<'failed' | 'queued' | 'running' | ''>('')
  const stats = useJobStats({ refetchInterval: 5000 })
  const jobs = useJobs(status || undefined, { refetchInterval: 5000 })
  const s = stats.data

  return (
    <SettingsCard
      title="Queue"
      icon={<ListChecks />}
      description="Publishing, schedule runs and sign-in refreshes run as jobs with retries. Failed ones can be retried here."
    >
      <div className="grid gap-3">
        <dl className="grid grid-cols-2 gap-2 sm:grid-cols-4" data-testid="queue-stats">
          {(
            [
              ['Queued', s?.queued],
              ['Running', s?.running],
              ['Failed', s?.failed],
              ['Done today', s?.done_24h],
            ] as const
          ).map(([label, n]) => (
            <div key={label} className="rounded-lg border p-2.5">
              <dt className="text-xs text-muted-foreground">{label}</dt>
              <dd className="text-xl font-semibold tabular-nums">{n ?? '–'}</dd>
            </div>
          ))}
        </dl>
        <ToggleGroup
          type="single"
          variant="outline"
          spacing={0}
          value={status || 'all'}
          onValueChange={(v) => v && setStatus(v === 'all' ? '' : (v as typeof status))}
          aria-label="Job filter"
          className="h-8 pointer-coarse:h-11"
        >
          {(['all', 'failed', 'queued', 'running'] as const).map((v) => (
            <ToggleGroupItem
              key={v}
              value={v}
              className="h-8 px-2.5 text-sm capitalize pointer-coarse:h-11 pointer-coarse:px-3.5"
            >
              {v}
            </ToggleGroupItem>
          ))}
        </ToggleGroup>
        {jobs.isLoading ? (
          <SkeletonList count={3} className="h-12 rounded-lg" />
        ) : jobs.error && !jobs.data ? (
          <ErrorState title="Could not load the queue" error={jobs.error} onRetry={() => void jobs.refetch()} />
        ) : (jobs.data?.length ?? 0) === 0 ? (
          <p className="text-sm text-muted-foreground">
            {status ? `No ${status} jobs.` : 'Nothing in the queue. Jobs appear when a post is scheduled or published.'}
          </p>
        ) : (
          <ul className="divide-y rounded-xl border" data-testid="jobs-list">
            {jobs.data!.map((j) => (
              <JobRow key={j.id} job={j} />
            ))}
          </ul>
        )}
      </div>
    </SettingsCard>
  )
}
