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
import { status, UNPUBLISHED_META } from '@/lib/status'
import { cn } from '@/lib/utils'

const UNPUBLISHING = status('Unpublishing', 'warning', LoaderCircle, 'Being taken down right now')

// How one channel of a saved post went (the composer, the post's Channels tab): kept apart from the composer so
// the post list's statistics dialog doesn't load it.

/** Why a channel or a comment failed: the sentence first, the platform's own text folded under it. */
function DeliveryError({ error }: { error: string }) {
  const { summary, detail } = splitDeliveryError(error)

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
      <RotateCcw /> Retry
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
          {target.removal_error && <span className="text-amber-700 dark:text-amber-400">{target.removal_error}</span>}
        </span>
      )
    case 'failed':
      return (
        <span className="text-destructive" data-testid="removal-error">
          Unpublish failed{target.removal_error ? `: ${target.removal_error}` : ''}
        </span>
      )
    case 'manual':
      return <span className="text-amber-700 dark:text-amber-400">Remove by hand: this platform has no delete</span>
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
      {target.published_at && (
        <span className="text-muted-foreground" title={formatDateTime(target.published_at)}>
          {timeAgo(target.published_at)}
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
                      pending: 'Waiting',
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
                {r.error && <DeliveryError error={r.error} />}
              </li>
            )
          })}
        </ol>
      )}
    </div>
  )
}
