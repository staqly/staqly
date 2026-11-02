'use client'

import { ProviderIcon } from '@staqly/brand/providers'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import {
  Bookmark,
  Clock,
  Eye,
  Heart,
  type LucideIcon,
  MessageCircle,
  MousePointerClick,
  Quote,
  RefreshCw,
  Repeat2,
  Share2,
  Timer,
  UserPlus,
} from 'lucide-react'
import dynamic from 'next/dynamic'
import { Suspense, useState } from 'react'

import { Callout } from '@/components/common/callout'
import { ErrorState } from '@/components/common/error-state'
import { Button } from '@/components/ui/button'
import { Skeleton } from '@/components/ui/skeleton'
import { api, type Post, type TargetMetrics } from '@/lib/api'
import { formatDateTime, timeAgo } from '@/lib/format'
import { toastApiError } from '@/lib/notify'
import { liveTargets } from '@/lib/posts'
import { qk } from '@/lib/queries'

import type { SentimentFilter } from './post-responses'
import { RetryTarget, TargetStatus } from './target-status'

// Only the post's Insights tab and the list's Insights dialog show them: their own chunk, loaded when they open.
const CommentsOverview = dynamic(() => import('@/components/posts/post-responses').then((m) => m.CommentsOverview))
const ChannelComments = dynamic(() => import('@/components/posts/post-responses').then((m) => m.ChannelComments))

const METRIC_LABELS: [Exclude<keyof NonNullable<TargetMetrics['metrics']>, 'reactions'>, string, LucideIcon][] = [
  ['impressions', 'Views', Eye],
  ['likes', 'Likes', Heart],
  ['replies', 'Replies', MessageCircle],
  ['reposts', 'Reposts', Repeat2],
  ['shares', 'Shares', Share2],
  ['quotes', 'Quotes', Quote],
  ['bookmarks', 'Bookmarks', Bookmark],
  ['link_clicks', 'Link clicks', MousePointerClick],
  ['watch_minutes', 'Watch time', Clock],
  ['avg_view_seconds', 'Average view', Timer],
  ['subscribers_gained', 'Subscribers gained', UserPlus],
]

/** Durations read as time: 1,284 min watched becomes 21 h 24 min, a 72 s average view 1:12. */
function metricValue(key: string, n: number) {
  if (key === 'watch_minutes') {
    return n >= 60 ? `${Math.floor(n / 60).toLocaleString('en-US')} h ${n % 60} min` : `${n} min`
  }

  if (key === 'avg_view_seconds') {
    return `${Math.floor(n / 60)}:${String(n % 60).padStart(2, '0')}`
  }

  return n.toLocaleString('en-US')
}

/** What a platform calls a number when it isn't the usual word (Slack has reactions, not likes). */
function metricLabel(provider: string, key: string, label: string) {
  return provider === 'slack' && key === 'likes' ? 'Reactions' : label
}

// Slack names its emoji; the common ones as themselves, the rest (custom ones too) by name.
const EMOJI: Record<string, string> = {
  '+1': '👍',
  thumbsup: '👍',
  '-1': '👎',
  thumbsdown: '👎',
  heart: '❤️',
  tada: '🎉',
  joy: '😂',
  fire: '🔥',
  eyes: '👀',
  rocket: '🚀',
  clap: '👏',
  raised_hands: '🙌',
  pray: '🙏',
  white_check_mark: '✅',
  heavy_check_mark: '✔️',
  '100': '💯',
  smile: '😄',
  slightly_smiling_face: '🙂',
  thinking_face: '🤔',
  star: '⭐',
}

const hasNumbers = (t: TargetMetrics) => !!t.metrics && METRIC_LABELS.some(([k]) => t.metrics?.[k] != null)

/** A channel's numbers in one line where they fit, a tile each. */
function MetricTiles({ t }: { t: TargetMetrics & { metrics: NonNullable<TargetMetrics['metrics']> } }) {
  return (
    <dl className="grid grid-cols-[repeat(auto-fill,minmax(6.5rem,1fr))] gap-2">
      {METRIC_LABELS.filter(([k]) => t.metrics?.[k] != null).map(([k, label, Icon]) => (
        // The label with its icon on top, the number under it: each one reads at a glance.
        <div key={k} className="grid gap-0.5 rounded-lg border bg-muted/30 px-3 py-2">
          <dt className="flex items-center gap-1.5 text-xs text-muted-foreground">
            <Icon aria-hidden className="size-3.5" />
            {metricLabel(t.provider, k, label)}
          </dt>
          <dd className="text-xl font-semibold tracking-tight tabular-nums">{metricValue(k, t.metrics![k]!)}</dd>
        </div>
      ))}
      {(t.metrics.reactions?.length ?? 0) > 0 && (
        <div className="col-span-full flex flex-wrap gap-1.5" aria-label="Reactions">
          {t.metrics.reactions!.map((r) => (
            <span
              key={r.emoji}
              className="inline-flex items-center gap-1 rounded-full border bg-muted/40 px-2 py-0.5 text-sm"
              title={r.people?.length ? r.people.join(', ') : undefined}
            >
              <span aria-label={r.emoji}>{EMOJI[r.emoji] ?? `:${r.emoji}:`}</span>
              <span className="tabular-nums">{r.count}</span>
              {r.people && r.people.length > 0 && (
                <span className="max-w-48 truncate text-xs text-muted-foreground">{r.people.join(', ')}</span>
              )}
            </span>
          ))}
        </div>
      )}
    </dl>
  )
}

/**
 * The post on its channels (the sheet's Insights tab, the list's Insights dialog): per channel how it went out
 * (status, link, errors, Retry, unpublished), its numbers in one line and its audience comments, with the AI's
 * reading of all comments on top. Numbers and comments are read only once something went out, and only while
 * `active`; the numbers are cached for ten minutes.
 */
export function PostStats({ post, active }: { post: Post; active: boolean }) {
  const qc = useQueryClient()
  const key = ['post-metrics', post.id] as const
  const targets = post.targets ?? []
  const out = targets.some((t) => t.status === 'published')
  const metrics = useQuery({ queryKey: key, queryFn: () => api.postMetrics(post.id), enabled: active && out })
  const [filter, setFilter] = useState<SentimentFilter>('all')
  const refresh = useMutation({
    mutationFn: () => Promise.all([api.postMetrics(post.id, true), api.refreshPostResponses(post.id)]),
    onSuccess: ([res]) => {
      qc.setQueryData(key, res)
      void qc.invalidateQueries({ queryKey: qk.postResponses(post.id) })
    },
    onError: (e) => toastApiError(e),
  })
  const rows = metrics.data?.data
  // Taken down everywhere it went out: the numbers and comments read last stay, nothing reads them again.
  const frozen = !!post.unpublished_at && liveTargets(post).length === 0
  const failed = (rows ?? []).filter((t) => t.error)

  return (
    <div className="grid gap-4" data-testid="post-stats">
      {post.unpublished_at && (
        <Callout tone="warning" size="xs" data-testid="stats-unpublished">
          <p className="font-medium">Unpublished {formatDateTime(post.unpublished_at)}</p>
          <p className="mt-0.5 text-muted-foreground">
            The numbers and comments here are the last ones read before it was taken down; they don&apos;t change any
            more.
          </p>
        </Callout>
      )}
      {failed.map((t) => (
        <Callout key={t.channel_id} tone="danger" size="xs" data-testid="metrics-error">
          <p className="font-medium">Couldn&apos;t read the latest numbers from {t.channel_name}</p>
          <p className="mt-0.5 text-muted-foreground">
            {t.error}
            {t.metrics ? ' The numbers below are the last ones read.' : ''}
          </p>
        </Callout>
      ))}
      {metrics.error && <ErrorState error={metrics.error} onRetry={() => metrics.refetch()} />}
      {out && (
        <Suspense fallback={null}>
          <CommentsOverview postId={post.id} active={active} filter={filter} onFilter={setFilter} />
        </Suspense>
      )}
      {targets.length === 0 ? (
        <p className="text-sm text-muted-foreground">This post has no channels yet.</p>
      ) : (
        <ul className="grid gap-3" aria-label="Channels">
          {targets.map((t) => {
            const m = rows?.find((r) => r.channel_id === t.channel_id)
            const published = t.status === 'published'

            return (
              <li key={t.channel_id} className="grid gap-2 rounded-xl border p-3" data-testid="post-channel">
                {/* Retry in the top right corner, beside the channel's name. */}
                <div className="flex items-center gap-2 text-sm">
                  <ProviderIcon id={t.provider} className="size-4 shrink-0" />
                  <span className="min-w-0 font-medium wrap-anywhere">{t.channel_name}</span>
                  {m?.updated_at && (
                    <span
                      className="ml-auto shrink-0 text-xs text-muted-foreground"
                      title={formatDateTime(m.updated_at)}
                    >
                      Updated {timeAgo(m.updated_at)}
                    </span>
                  )}
                  {t.status === 'failed' && <RetryTarget post={post} target={t} className="ml-auto shrink-0" />}
                </div>
                <TargetStatus post={post} target={t} retry={false} />
                {published &&
                  (metrics.isPending && active ? (
                    // Where the numbers come: nothing moves when they arrive.
                    <Skeleton className="h-16 w-full" />
                  ) : m ? (
                    m.available && m.metrics && hasNumbers(m) ? (
                      <MetricTiles t={{ ...m, metrics: m.metrics }} />
                    ) : (
                      <p className="text-xs text-muted-foreground">
                        {!m.available
                          ? 'Numbers are not available for this platform.'
                          : m.provider === 'custom'
                            ? 'Your automation has not sent numbers yet.'
                            : 'No numbers yet.'}
                      </p>
                    )
                  ) : null)}
                {published && (
                  <Suspense fallback={<Skeleton className="h-4 w-28" />}>
                    <ChannelComments postId={post.id} channelId={t.channel_id} active={active} filter={filter} />
                  </Suspense>
                )}
              </li>
            )
          })}
        </ul>
      )}
      {out && !frozen && (
        <div>
          <Button type="button" variant="outline" disabled={refresh.isPending} onClick={() => refresh.mutate()}>
            <RefreshCw className={refresh.isPending ? 'animate-spin' : undefined} /> Refresh numbers and comments
          </Button>
        </div>
      )}
    </div>
  )
}
