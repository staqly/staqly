'use client'

import { useMutation, useQueryClient } from '@tanstack/react-query'
import { ArrowRight, ExternalLink, LoaderCircle, RotateCcw } from 'lucide-react'

import { Callout } from '@/components/common/callout'
import { Badge, TargetStatusBadge } from '@/components/common/status-badge'
import { Button } from '@/components/ui/button'
import { api, type Post, type PostTarget } from '@/lib/api'
import { formatDateTime, timeAgo } from '@/lib/format'
import { namedToast, toastApiError } from '@/lib/notify'
import { splitDeliveryError } from '@/lib/posts'
import { invalidateAfterPostChange, syncPostInCache } from '@/lib/queries'
import { safeHttpUrl } from '@/lib/safe'
import { DELETED_META, status, UNPUBLISHED_META } from '@/lib/status'
import { cn } from '@/lib/utils'

const UNPUBLISHING = status('Unpublishing', 'warning', LoaderCircle, 'Being taken down right now')

// How one channel of a saved post went (the composer, the post's Channels tab): kept apart from the composer so
// the post list's statistics dialog doesn't load it.

/** Why a channel or a comment failed: the sentence first, the platform's own text folded under it. */
function DeliveryError({ error, compact }: { error: string; compact?: boolean }) {
  const { summary, detail } = splitDeliveryError(error)

  // A comment under a post that went out: one quiet line, not a box louder than the post's own state.
  if (compact) {
    return (
      <p className="basis-full text-xs text-muted-foreground" data-testid="delivery-error" title={detail || undefined}>
        {summary}
      </p>
    )
  }

  return (
    <Callout tone="danger" size="xs" className="basis-full" data-testid="delivery-error">
      <p>{summary}</p>
      {detail && (
        <details className="mt-1 text-muted-foreground">
          <summary className="cursor-pointer select-none">Technical details</summary>
          <code className="mt-1 block font-mono wrap-anywhere">{detail}</code>
        </details>
      )}
    </Callout>
  )
}

/** Retry for a channel that failed: queues it again. */
export function RetryTarget({ post, target, className }: { post: Post; target: PostTarget; className?: string }) {
  const qc = useQueryClient()
  const retry = useMutation({
    mutationFn: () => api.retryTarget(post.id, target.channel_id),
    onSuccess: (p) => {
      syncPostInCache(qc, p)
      invalidateAfterPostChange(qc, p.id)
      namedToast(target.channel_name, 'is being retried')
    },
    onError: (e) => toastApiError(e),
  })

  return (
    <Button
      type="button"
      size="sm"
      variant="outline"
      className={className}
      onClick={() => retry.mutate()}
      disabled={retry.isPending}
    >
      <RotateCcw /> Retry now
    </Button>
  )
}

/** Retry now for every failed channel of the post at once (one request each), for a post sent to many. */
export function RetryFailedTargets({ post, targets }: { post: Post; targets: PostTarget[] }) {
  const qc = useQueryClient()
  const retry = useMutation({
    mutationFn: async () => {
      let last: Post | undefined

      for (const t of targets) {
        last = await api.retryTarget(post.id, t.channel_id)
      }

      return last
    },
    onSuccess: (p) => {
      if (p) {
        syncPostInCache(qc, p)
        invalidateAfterPostChange(qc, p.id)
      }

      namedToast(post.title, `is being retried on ${targets.length} channels`)
    },
    onError: (e) => {
      // Some may have gone out again before the error: show what the server has now.
      invalidateAfterPostChange(qc, post.id)
      toastApiError(e)
    },
  })

  return (
    <Button
      type="button"
      size="sm"
      variant="outline"
      onClick={() => retry.mutate()}
      disabled={retry.isPending}
      data-testid="retry-failed"
    >
      <RotateCcw /> Retry {targets.length} failed now
    </Button>
  )
}

/** Taking a channel down after Published: unpublishing, Unpublished and when, failed with the platform's error (as
 * text), or remove by hand. */
function TargetRemoval({ target }: { target: PostTarget }) {
  switch (target.removal) {
    case 'removing':
      return (
        <span className="inline-flex items-center gap-x-2" data-testid="target-unpublishing">
          <ArrowRight aria-hidden className="size-3 text-muted-foreground" />
          <Badge meta={UNPUBLISHING} status="unpublishing" />
        </span>
      )
    case 'removed':
      return (
        <span className="inline-flex items-center gap-x-2" data-testid="target-unpublished">
          <ArrowRight aria-hidden className="size-3 text-muted-foreground" />
          <Badge
            meta={UNPUBLISHED_META}
            status="unpublished"
            title={target.removed_at ? formatDateTime(target.removed_at) : undefined}
          />
          {target.removed_at && (
            <span className="text-muted-foreground" title={formatDateTime(target.removed_at)}>
              {timeAgo(target.removed_at)}
            </span>
          )}
        </span>
      )
    case 'deleted':
      return (
        <span className="inline-flex items-center gap-x-2" data-testid="target-deleted">
          <ArrowRight aria-hidden className="size-3 text-muted-foreground" />
          <Badge meta={DELETED_META} status="deleted" />
          <span className="text-muted-foreground">
            on the platform, outside staqly
            {target.removed_at && (
              <span title={formatDateTime(target.removed_at)}> · found {timeAgo(target.removed_at)}</span>
            )}
          </span>
        </span>
      )
    case 'failed':
      return (
        <span className="text-destructive" data-testid="removal-error">
          Unpublish failed{target.removal_error ? `: ${target.removal_error}` : ''}
        </span>
      )
    case 'manual':
      return (
        <span className="text-amber-700 dark:text-amber-400">
          Remove by hand: {target.removal_error || 'this platform has no delete'}
        </span>
      )
    default:
      return null
  }
}

/**
 * How one channel of a saved post went: state, the link once published, the error, and Retry when failed (unless
 * the card shows Retry in its own header: `retry={false}`).
 */
export function TargetStatus({ post, target, retry = true }: { post: Post; target: PostTarget; retry?: boolean }) {
  const url = target.external_url ? safeHttpUrl(target.external_url) : undefined
  const tried =
    target.status === 'published'
      ? (target.published_at ?? target.attempted_at)
      : target.status === 'failed'
        ? target.attempted_at
        : null

  return (
    <div className="flex flex-wrap items-center gap-x-2 gap-y-1 text-xs" data-testid="post-target">
      <TargetStatusBadge status={target.status} />
      {url && (
        <a
          href={url}
          target="_blank"
          rel="noopener noreferrer nofollow"
          className="inline-flex max-w-full min-w-0 items-center gap-1 text-brand underline underline-offset-2 pointer-coarse:-my-3 pointer-coarse:py-3"
        >
          <span className="truncate">{url}</span>
          <ExternalLink className="size-3 shrink-0" aria-hidden />
        </a>
      )}
      {tried && (
        // When it went out or failed: history, the post's own time can't say it for each channel.
        <span className="text-muted-foreground tabular-nums" title={timeAgo(tried)} data-testid="target-time">
          {formatDateTime(tried)}
          {target.status === 'failed' && target.attempts > 1 && ` · ${target.attempts} attempts`}
        </span>
      )}
      <TargetRemoval target={target} />
      {retry && target.status === 'failed' && <RetryTarget post={post} target={target} className="ml-auto" />}
      {target.error && <DeliveryError error={target.error} />}
      {(target.replies?.length ?? 0) > 0 && (
        <ol className="grid basis-full gap-1 border-l pl-3" aria-label={`Comments on ${target.channel_name}`}>
          {target.replies!.map((r) => {
            const href = r.external_url ? safeHttpUrl(r.external_url) : undefined

            return (
              <li key={r.index} className="flex flex-wrap items-center gap-x-2 gap-y-0.5" data-testid="post-reply">
                <span className="text-muted-foreground">Comment {r.index + 1}</span>
                <span
                  className={cn(
                    r.status === 'published' && 'text-emerald-600 dark:text-emerald-400',
                    r.status === 'failed' && 'text-destructive',
                    (r.status === 'pending' || r.status === 'skipped') && 'text-muted-foreground',
                  )}
                >
                  {
                    {
                      // Comments go under the post: with the post failed they wait for its retry.
                      pending: target.status === 'failed' ? 'Not sent (goes with a retry)' : 'Waiting',
                      publishing: 'Publishing',
                      published: 'Published',
                      failed: 'Failed',
                      skipped: 'Not sent (no comments on this channel)',
                    }[r.status]
                  }
                </span>
                {href && (
                  <a
                    href={href}
                    target="_blank"
                    rel="noopener noreferrer nofollow"
                    className="inline-flex items-center gap-1 text-brand underline underline-offset-2 pointer-coarse:-my-3 pointer-coarse:py-3"
                  >
                    Open <ExternalLink className="size-3" aria-hidden />
                  </a>
                )}
                {r.error && <DeliveryError error={r.error} compact />}
              </li>
            )
          })}
        </ol>
      )}
    </div>
  )
}
