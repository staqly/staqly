'use client'

import { keepPreviousData, useQuery } from '@tanstack/react-query'
import { BarChart3, PenSquare, Share2 } from 'lucide-react'
import Link from 'next/link'
import { useRouter, useSearchParams } from 'next/navigation'
import { useMemo } from 'react'

import { ChannelAvatar } from '@/components/channels/channel-avatars'
import { DateRangePicker, formatDayRange } from '@/components/common/date-range-picker'
import { EmptyState, ErrorState } from '@/components/common/error-state'
import { MultiSelect } from '@/components/common/multi-select'
import { PageContainer, PageHeader } from '@/components/common/page-header'
import { Button } from '@/components/ui/button'
import { Skeleton } from '@/components/ui/skeleton'
import { ToggleGroup, ToggleGroupItem } from '@/components/ui/toggle-group'
import { useChannels } from '@/lib/queries'
import { cn, hrefWithParams } from '@/lib/utils'

import { DailyChart, PostingTimes } from './analytics-charts'
import { fetchAnalytics, PRESETS, type Analytics, type AnalyticsParams } from './analytics-data'
import { ChannelsTable, FailuresList, Performance, SourcesTable, TopPosts } from './analytics-sections'
import { KpiTiles } from './kpi-tiles'

const NONE: string[] = []

const DAY = /^\d{4}-\d{2}-\d{2}$/

/** The page's filters as the URL keeps them: ?days=7|30|90 or ?from=&to=, plus ?channel= and ?source= (repeated). */
export function readParams(sp: URLSearchParams): AnalyticsParams {
  const from = sp.get('from') ?? ''
  const to = sp.get('to') ?? ''
  const days = Number(sp.get('days'))
  const out: AnalyticsParams = {}

  if (DAY.test(from) && DAY.test(to)) {
    out.from = from
    out.to = to
  } else {
    out.days = (PRESETS as readonly number[]).includes(days) ? days : 30
  }

  const channels = sp.getAll('channel').filter(Boolean)

  if (channels.length) {
    out.channels = channels
  }

  // An empty source is a real one (posts with none recorded), so it is kept when present.
  const sources = sp.getAll('source')

  if (sources.length) {
    out.sources = sources
  }

  return out
}

export function AnalyticsPage() {
  const router = useRouter()
  const sp = useSearchParams()
  const params = useMemo(() => readParams(new URLSearchParams(sp.toString())), [sp])
  const { data, error, isPending, isPlaceholderData, refetch } = useQuery({
    queryKey: ['analytics', params],
    queryFn: ({ signal }) => fetchAnalytics(params, signal),
    // A new range or filter keeps the last numbers on screen (dimmed) until the new ones arrive.
    placeholderData: keepPreviousData,
    staleTime: 60_000,
  })
  const channels = useChannels()

  const set = (patch: Record<string, string | undefined>) =>
    router.replace(hrefWithParams('/analytics', sp, patch), { scroll: false })

  // Repeated parameters (channel=a&channel=b), which hrefWithParams doesn't write.
  const setList = (key: 'channel' | 'source', values: string[]) => {
    const next = new URLSearchParams(sp.toString())

    next.delete(key)

    for (const v of values) {
      next.append(key, v)
    }

    const s = next.toString()

    router.replace(s ? `/analytics?${s}` : '/analytics', { scroll: false })
  }

  const rangeValue = params.from && params.to ? 'custom' : String(params.days)

  const header = (
    <PageHeader
      icon={<BarChart3 />}
      title="Analytics"
      description="How your publishing goes: what went out, what failed, where posts come from and when they do best."
    />
  )

  const filters = (
    <div
      role="group"
      aria-label="Filters"
      className="flex flex-wrap items-center gap-2"
      data-testid="analytics-filters"
    >
      <ToggleGroup
        type="single"
        variant="outline"
        size="sm"
        value={rangeValue}
        aria-label="Date range"
        onValueChange={(v) => {
          if (v && v !== 'custom') {
            set({ days: v === '30' ? undefined : v, from: undefined, to: undefined })
          }
        }}
      >
        {PRESETS.map((d) => (
          <ToggleGroupItem key={d} value={String(d)} aria-label={`Last ${d} days`}>
            {d} days
          </ToggleGroupItem>
        ))}
      </ToggleGroup>
      <DateRangePicker
        label="Custom range"
        placeholder="Custom range"
        value={params.from && params.to ? { from: params.from, to: params.to } : null}
        onChange={(r) => set(r ? { from: r.from, to: r.to, days: undefined } : { from: undefined, to: undefined })}
      />
      <MultiSelect
        label="Channels"
        anyLabel="All channels"
        size="sm"
        className="w-full sm:w-52"
        value={params.channels ?? NONE}
        onChange={(v) => setList('channel', v)}
        options={(channels.data ?? []).map((c) => ({
          value: c.id,
          label: c.name,
          icon: <ChannelAvatar channel={c} className="size-5" />,
        }))}
      />
      <SourceSelect data={data} value={params.sources} onChange={(v) => setList('source', v)} />
    </div>
  )

  if (isPending) {
    return (
      <PageContainer className="grid gap-6">
        {header}
        {filters}
        <div className="grid grid-cols-2 gap-3 md:grid-cols-5" aria-busy="true" aria-label="Loading analytics">
          {[0, 1, 2, 3, 4].map((i) => (
            <Skeleton key={i} className="h-24 rounded-xl" />
          ))}
        </div>
        <Skeleton className="h-64 rounded-xl" />
      </PageContainer>
    )
  }

  if (error || !data) {
    return (
      <PageContainer className="grid gap-6">
        {header}
        {filters}
        <ErrorState error={error} title="Could not load analytics" onRetry={() => void refetch()} />
      </PageContainer>
    )
  }

  const hasChannels = (channels.data?.length ?? data.channels.length) > 0
  const empty =
    data.totals.targets_published + data.totals.targets_failed + data.totals.posts_created === 0 &&
    data.queue.scheduled + data.queue.drafts === 0

  return (
    <PageContainer className="grid gap-6">
      {header}
      {filters}
      <p className="-mt-3 border-b pb-4 text-xs text-muted-foreground" data-testid="analytics-range">
        {formatDayRange({ from: data.range.from, to: data.range.to })} · {data.range.days} days · times in{' '}
        {data.range.timezone}
      </p>
      {empty ? (
        <EmptyAnalytics hasChannels={hasChannels} filtered={!!params.channels || !!params.sources} />
      ) : (
        <div
          className={cn('grid gap-6 transition-opacity', isPlaceholderData && 'opacity-60')}
          aria-busy={isPlaceholderData}
        >
          <KpiTiles data={data} />
          <DailyChart data={data} />
          <ChannelsTable data={data} />
          <SourcesTable data={data} />
          <Performance data={data} />
          <PostingTimes data={data} />
          <FailuresList data={data} />
          <TopPosts data={data} />
        </div>
      )}
    </PageContainer>
  )
}

function SourceSelect({
  data,
  value,
  onChange,
}: {
  data: Analytics | undefined
  value: string[] | undefined
  onChange: (v: string[]) => void
}) {
  const sources = data?.sources ?? []
  // Chosen sources stay in the list even when they have nothing in the range.
  const missing = (value ?? []).filter((v) => !sources.some((s) => s.source === v))

  return (
    <MultiSelect
      label="Sources"
      anyLabel="All sources"
      size="sm"
      className="w-full sm:w-52"
      value={value ?? NONE}
      onChange={onChange}
      options={[
        ...sources.map((s) => ({ value: s.source, label: s.label })),
        ...missing.map((v) => ({ value: v, label: v || 'Not recorded' })),
      ]}
    />
  )
}

function EmptyAnalytics({ hasChannels, filtered }: { hasChannels: boolean; filtered: boolean }) {
  if (!hasChannels) {
    return (
      <EmptyState
        icon={<Share2 />}
        title="No channels yet"
        description="Connect a channel and publish a few posts: this page then shows what went out, what failed and when your posts do best."
        action={
          <Button asChild size="sm">
            <Link href="/channels">Connect a channel</Link>
          </Button>
        }
      />
    )
  }

  return (
    <EmptyState
      icon={<BarChart3 />}
      title="No data yet in this range"
      description={
        filtered
          ? 'Nothing was published, failed or created with these filters. Try a longer range or clear the filters.'
          : 'Nothing was published, failed or created in this range. Write a post or pick a longer range.'
      }
      action={
        filtered ? undefined : (
          <Button asChild size="sm">
            <Link href="/posts?compose=1">
              <PenSquare /> Write a post
            </Link>
          </Button>
        )
      }
    />
  )
}
