'use client'

import { ArrowDown, ArrowUp, ArrowUpDown, ExternalLink } from 'lucide-react'
import Link from 'next/link'
import { useState } from 'react'

import { ChannelAvatar } from '@/components/channels/channel-avatars'
import { ChannelStatusBadge } from '@/components/common/status-badge'
import { formatDateTime, timeAgo } from '@/lib/format'
import { ProviderIcon } from '@/lib/providers'
import { safeHttpUrl } from '@/lib/safe'
import { cn } from '@/lib/utils'

import {
  formatCount,
  formatRate,
  SOURCE_KINDS,
  type Analytics,
  type AnalyticsMetrics,
  type ChannelAnalytics,
  VIZ_COLORS,
} from './analytics-data'

const card = 'grid gap-3 rounded-xl border bg-card p-4 shadow-xs'
const th = 'px-2 py-2 text-left text-xs font-medium whitespace-nowrap text-muted-foreground'
const td = 'px-2 py-2 align-top'
const num = 'px-2 py-2 text-right align-top tabular-nums whitespace-nowrap'

/** Why a channel shows no platform numbers: three different situations, three different messages. */
export function numbersNote(c: ChannelAnalytics): string | null {
  const cov = c.metrics_coverage

  if (cov.support === 'none') {
    return "This platform doesn't report numbers to staqly yet"
  }

  if (cov.published === 0) {
    return 'No data yet in this range'
  }

  if (cov.with_data === 0) {
    return cov.support === 'automation'
      ? "Your automation hasn't reported numbers for these posts"
      : "No numbers read yet: a published post's Statistics tab reads them"
  }

  return null
}

type SortKey = 'name' | 'published' | 'failed' | 'success_rate' | 'last_published_at' | 'views' | 'engagements'

const SORTS: { key: SortKey; label: string; numeric: boolean }[] = [
  { key: 'name', label: 'Channel', numeric: false },
  { key: 'published', label: 'Published', numeric: true },
  { key: 'failed', label: 'Failed', numeric: true },
  { key: 'success_rate', label: 'Success', numeric: true },
  { key: 'last_published_at', label: 'Last post', numeric: false },
  { key: 'views', label: 'Views', numeric: true },
  { key: 'engagements', label: 'Engagements', numeric: true },
]

function sortValue(c: ChannelAnalytics, key: SortKey): number | string {
  switch (key) {
    case 'name':
      return c.name.toLowerCase()
    case 'success_rate':
      return c.success_rate ?? -1
    case 'last_published_at':
      return c.last_published_at ?? ''
    case 'views':
      return c.metrics?.impressions ?? -1
    case 'engagements':
      return c.metrics?.engagements ?? -1
    default:
      return c[key]
  }
}

/** Each channel: outcomes in the range and the numbers its platform reports, sortable by any column. */
export function ChannelsTable({ data }: { data: Analytics }) {
  const [sort, setSort] = useState<{ key: SortKey; desc: boolean }>({ key: 'published', desc: true })
  const rows = data.channels.toSorted((a, b) => {
    const va = sortValue(a, sort.key)
    const vb = sortValue(b, sort.key)
    const cmp = va < vb ? -1 : va > vb ? 1 : 0

    return sort.desc ? -cmp : cmp
  })

  return (
    <section aria-labelledby="channels-heading" className={card}>
      <div>
        <h2 id="channels-heading" className="text-base font-semibold">
          Channels
        </h2>
        <p className="text-xs text-muted-foreground">
          Period totals per channel. Views and engagements are the latest numbers staqly has for the posts published in
          this range.
        </p>
      </div>
      {rows.length === 0 ? (
        <p className="text-sm text-muted-foreground">No channels match these filters.</p>
      ) : (
        <div className="relative overflow-x-auto" tabIndex={0} role="region" aria-labelledby="channels-heading">
          <table className="w-full min-w-[46rem] text-sm" data-testid="channels-table">
            <caption className="sr-only">Channels, sortable by each column</caption>
            <thead>
              <tr className="border-b">
                {SORTS.map((s) => {
                  const active = sort.key === s.key
                  const Icon = !active ? ArrowUpDown : sort.desc ? ArrowDown : ArrowUp

                  return (
                    <th
                      key={s.key}
                      scope="col"
                      aria-sort={active ? (sort.desc ? 'descending' : 'ascending') : 'none'}
                      className={cn(th, s.numeric && 'text-right')}
                    >
                      <button
                        type="button"
                        onClick={() => setSort({ key: s.key, desc: active ? !sort.desc : s.numeric })}
                        className={cn(
                          'inline-flex items-center gap-1 rounded-sm hover:text-foreground focus-visible:outline-2 focus-visible:outline-ring pointer-coarse:-my-3 pointer-coarse:py-3',
                          active && 'text-foreground',
                        )}
                      >
                        {s.label}
                        <Icon className="size-3" aria-hidden />
                      </button>
                    </th>
                  )
                })}
                <th scope="col" className={th}>
                  Most common failure
                </th>
              </tr>
            </thead>
            <tbody>
              {rows.map((c) => {
                const note = numbersNote(c)

                return (
                  <tr key={c.channel_id} className="border-b last:border-0" data-testid="channel-row">
                    <th scope="row" className={cn(td, 'text-left font-normal')}>
                      <div className="flex min-w-0 items-center gap-2">
                        <ChannelAvatar
                          channel={{ id: c.channel_id, provider: c.provider, name: c.name, avatar_url: c.avatar_url }}
                          className="size-7"
                        />
                        <div className="min-w-0">
                          <div className="truncate font-medium" title={c.name}>
                            {c.name}
                          </div>
                          {c.status !== 'connected' && <ChannelStatusBadge status={c.status} className="mt-0.5" />}
                        </div>
                      </div>
                    </th>
                    <td className={num}>{formatCount(c.published)}</td>
                    <td className={num}>{formatCount(c.failed)}</td>
                    <td className={num}>{formatRate(c.success_rate)}</td>
                    <td className={cn(td, 'whitespace-nowrap text-muted-foreground')}>
                      {c.last_published_at ? (
                        <time dateTime={c.last_published_at} title={formatDateTime(c.last_published_at)}>
                          {timeAgo(c.last_published_at)}
                        </time>
                      ) : (
                        '—'
                      )}
                    </td>
                    {note ? (
                      <td colSpan={2} className={cn(td, 'max-w-56 text-right text-xs text-muted-foreground')}>
                        {note}
                      </td>
                    ) : (
                      <>
                        <td className={num}>
                          {c.metrics?.impressions !== null && c.metrics?.impressions !== undefined
                            ? formatCount(c.metrics.impressions)
                            : '—'}
                        </td>
                        <td className={num} title={coverageTitle(c)}>
                          {formatCount(c.metrics?.engagements ?? 0)}
                        </td>
                      </>
                    )}
                    <td className={cn(td, 'max-w-64 text-xs text-muted-foreground')}>
                      {c.top_failure ? <span className="line-clamp-2 wrap-anywhere">{c.top_failure}</span> : '—'}
                    </td>
                  </tr>
                )
              })}
            </tbody>
          </table>
        </div>
      )}
    </section>
  )
}

function coverageTitle(c: ChannelAnalytics) {
  const cov = c.metrics_coverage
  const read = cov.updated_at ? `, last read ${formatDateTime(cov.updated_at)}` : ''

  return `${cov.with_data} of ${cov.published} published posts have numbers${read}`
}

/** Where posts come from: the dashboard, each agent over MCP or the API, schedules and repeats. */
export function SourcesTable({ data }: { data: Analytics }) {
  const max = Math.max(1, ...data.sources.map((s) => s.targets_published + s.targets_failed))

  return (
    <section aria-labelledby="sources-heading" className={cn(card, VIZ_COLORS)}>
      <div>
        <h2 id="sources-heading" className="text-base font-semibold">
          Automation
        </h2>
        <p className="text-xs text-muted-foreground">
          Where posts come from (who created them), period totals. The success rate counts the channels that finished.
        </p>
      </div>
      {data.sources.length === 0 ? (
        <p className="text-sm text-muted-foreground">No posts were created or published in this range.</p>
      ) : (
        <div className="relative overflow-x-auto" tabIndex={0} role="region" aria-labelledby="sources-heading">
          <table className="w-full min-w-[36rem] text-sm" data-testid="sources-table">
            <caption className="sr-only">Posts and channels by source</caption>
            <thead>
              <tr className="border-b">
                <th scope="col" className={th}>
                  Source
                </th>
                <th scope="col" className={cn(th, 'text-right')}>
                  Created
                </th>
                <th scope="col" className={cn(th, 'text-right')}>
                  Published
                </th>
                <th scope="col" className={th}>
                  Channels published and failed
                </th>
                <th scope="col" className={cn(th, 'text-right')}>
                  Success
                </th>
              </tr>
            </thead>
            <tbody>
              {data.sources.map((s) => {
                const done = s.targets_published + s.targets_failed

                return (
                  <tr key={s.source} className="border-b last:border-0" data-testid="source-row">
                    <th scope="row" className={cn(td, 'max-w-56 text-left font-normal')}>
                      <span className="mr-2 font-medium wrap-anywhere">{s.label}</span>
                      <span className="rounded-md border px-1.5 text-xs whitespace-nowrap text-muted-foreground">
                        {SOURCE_KINDS[s.kind]}
                      </span>
                    </th>
                    <td className={num}>{formatCount(s.posts_created)}</td>
                    <td className={num}>{formatCount(s.posts_published)}</td>
                    <td className={cn(td, 'whitespace-nowrap')}>
                      <span className="mr-2 inline-flex h-2.5 w-32 gap-0.5 align-middle" aria-hidden>
                        {s.targets_published > 0 && (
                          <span
                            className="h-full rounded-[2px] bg-(--viz-published)"
                            style={{ width: `${(s.targets_published / max) * 100}%` }}
                          />
                        )}
                        {s.targets_failed > 0 && (
                          <span
                            className="h-full rounded-[2px] bg-(--viz-failed)"
                            style={{ width: `${(s.targets_failed / max) * 100}%` }}
                          />
                        )}
                      </span>
                      <span className="text-xs text-muted-foreground tabular-nums">
                        {formatCount(s.targets_published)} published · {formatCount(s.targets_failed)} failed
                      </span>
                    </td>
                    <td className={num}>{done ? formatRate(s.success_rate) : '—'}</td>
                  </tr>
                )
              })}
            </tbody>
          </table>
        </div>
      )}
    </section>
  )
}

const METRIC_ROWS: [keyof AnalyticsMetrics, string][] = [
  ['impressions', 'Views'],
  ['engagements', 'Engagements'],
  ['likes', 'Likes'],
  ['replies', 'Replies'],
  ['reposts', 'Reposts'],
  ['link_clicks', 'Link clicks'],
]

/**
 * The platform numbers staqly already has for posts published in the range (summed). Says how many posts they
 * cover and when they were read, so a partial total is never mistaken for the whole.
 */
export function Performance({ data }: { data: Analytics }) {
  const m = data.totals.metrics
  const covered = data.channels.reduce((n, c) => n + c.metrics_coverage.with_data, 0)
  const published = data.channels.reduce((n, c) => n + c.metrics_coverage.published, 0)
  const lastRead = data.channels.reduce<string | null>(
    (last, c) =>
      c.metrics_coverage.updated_at && (!last || c.metrics_coverage.updated_at > last)
        ? c.metrics_coverage.updated_at
        : last,
    null,
  )
  const silent = data.channels.filter((c) => c.metrics_coverage.support === 'none' && c.published > 0)

  return (
    <section aria-labelledby="performance-heading" className={card} data-testid="performance">
      <div>
        <h2 id="performance-heading" className="text-base font-semibold">
          Performance
        </h2>
        <p className="text-xs text-muted-foreground">
          Platform numbers summed over the posts published in this range, as last read (staqly doesn&apos;t fetch them
          here).
        </p>
      </div>
      {m ? (
        <>
          <dl className="grid grid-cols-2 gap-3 sm:grid-cols-3 lg:grid-cols-6">
            {METRIC_ROWS.filter(([k]) => m[k] !== null).map(([k, label]) => (
              <div key={k} className="rounded-lg border px-3 py-2">
                <dt className="text-xs text-muted-foreground">{label}</dt>
                <dd className="text-lg font-semibold">{formatCount(m[k] ?? 0)}</dd>
              </div>
            ))}
          </dl>
          <p className="text-xs text-muted-foreground">
            From {formatCount(covered)} of {formatCount(published)} published channel posts
            {lastRead ? `, newest read ${timeAgo(lastRead)}` : ''}. Engagements are likes, replies, reposts, quotes and
            bookmarks.
          </p>
        </>
      ) : (
        <p className="text-sm text-muted-foreground">
          {published === 0
            ? 'No data yet in this range.'
            : "No numbers yet for the posts in this range. A published post's Statistics tab reads them from platforms that report numbers (X, YouTube, Slack); your own automation can report its own."}
        </p>
      )}
      {silent.length > 0 && (
        <p className="text-xs text-muted-foreground">
          {silent.map((c) => c.name).join(', ')}: this platform doesn&apos;t report numbers to staqly yet.
        </p>
      )}
    </section>
  )
}

/** The most common failure reasons, each with the channels it hit and a way to the posts. */
export function FailuresList({ data }: { data: Analytics }) {
  const names = new Map(data.channels.map((c) => [c.channel_id, c]))

  return (
    <section aria-labelledby="failures-heading" className={card}>
      <div>
        <h2 id="failures-heading" className="text-base font-semibold">
          Failure reasons
        </h2>
        <p className="text-xs text-muted-foreground">
          Period totals; the same error with other numbers or links counts once.
        </p>
      </div>
      {data.failures.length === 0 ? (
        <p className="text-sm text-muted-foreground">Nothing failed in this range.</p>
      ) : (
        <ol className="grid gap-2" data-testid="failures-list">
          {data.failures.map((f) => (
            <li key={f.reason + f.last_at} className="grid gap-1 rounded-lg border px-3 py-2 text-sm">
              <div className="flex items-start justify-between gap-3">
                <span className="min-w-0 wrap-anywhere">{f.reason}</span>
                <span className="shrink-0 font-semibold tabular-nums">{formatCount(f.count)}×</span>
              </div>
              <div className="flex flex-wrap items-center gap-x-3 gap-y-1 text-xs text-muted-foreground">
                {f.channels.map((id) => {
                  const c = names.get(id)

                  return (
                    <Link
                      key={id}
                      href={`/posts?status=failed&channel=${encodeURIComponent(id)}`}
                      className="inline-flex items-center gap-1 underline-offset-2 hover:text-foreground hover:underline pointer-coarse:-my-3 pointer-coarse:py-3"
                    >
                      {c && <ProviderIcon id={c.provider} className="size-3" />}
                      {c?.name ?? id}
                    </Link>
                  )
                })}
                <Link
                  href={`/posts/${encodeURIComponent(f.sample_post_id)}`}
                  prefetch={false}
                  className="underline-offset-2 hover:text-foreground hover:underline pointer-coarse:-my-3 pointer-coarse:py-3"
                >
                  Open an example post
                </Link>
                <span>last {timeAgo(f.last_at)}</span>
              </div>
            </li>
          ))}
        </ol>
      )}
    </section>
  )
}

/** The best posts by engagement where numbers exist, otherwise the most recent ones. */
export function TopPosts({ data }: { data: Analytics }) {
  const { by, posts } = data.top_posts
  const names = new Map(data.channels.map((c) => [c.channel_id, c.name]))

  return (
    <section aria-labelledby="top-heading" className={card}>
      <div>
        <h2 id="top-heading" className="text-base font-semibold">
          {by === 'engagements' ? 'Top posts' : 'Recent posts'}
        </h2>
        <p className="text-xs text-muted-foreground">
          {by === 'engagements'
            ? 'Posts published in this range with the most engagements (likes, replies, reposts, quotes, bookmarks).'
            : "Posts published in this range, newest first. They're ranked by engagement once platforms report numbers."}
        </p>
      </div>
      {posts.length === 0 ? (
        <p className="text-sm text-muted-foreground">No posts were published in this range.</p>
      ) : (
        <ol className="grid gap-2" data-testid="top-posts">
          {posts.map((p) => (
            <li key={p.post_id} className="grid gap-1 rounded-lg border px-3 py-2 text-sm">
              <div className="flex items-start justify-between gap-3">
                <Link
                  href={`/posts/${encodeURIComponent(p.post_id)}`}
                  prefetch={false}
                  className="min-w-0 font-medium wrap-anywhere underline-offset-2 hover:underline"
                >
                  {p.excerpt || 'A post without text'}
                </Link>
                {p.metrics && (
                  <span className="shrink-0 text-right text-xs text-muted-foreground">
                    <strong className="text-sm text-foreground">{formatCount(p.metrics.engagements)}</strong>{' '}
                    engagements
                    {p.metrics.impressions !== null && <> · {formatCount(p.metrics.impressions)} views</>}
                  </span>
                )}
              </div>
              <div className="flex flex-wrap items-center gap-x-3 gap-y-1 text-xs text-muted-foreground">
                <time dateTime={p.published_at}>{formatDateTime(p.published_at)}</time>
                {p.channels.map((c) => {
                  const href = safeHttpUrl(c.external_url)

                  return href ? (
                    <a
                      key={c.channel_id}
                      href={href}
                      target="_blank"
                      rel="noopener noreferrer"
                      className="inline-flex items-center gap-1 underline-offset-2 hover:text-foreground hover:underline pointer-coarse:-my-3 pointer-coarse:py-3"
                    >
                      <ProviderIcon id={c.provider} className="size-3" />
                      View on {names.get(c.channel_id) ?? 'the platform'}
                      <ExternalLink className="size-3" aria-hidden />
                    </a>
                  ) : (
                    <span key={c.channel_id} className="inline-flex items-center gap-1">
                      <ProviderIcon id={c.provider} className="size-3" />
                    </span>
                  )
                })}
              </div>
            </li>
          ))}
        </ol>
      )}
    </section>
  )
}
