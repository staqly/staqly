'use client'

import { keepPreviousData, useQuery } from '@tanstack/react-query'
import { BarChart3, PenSquare, Share2 } from 'lucide-react'
import Link from 'next/link'
import { useRouter, useSearchParams } from 'next/navigation'
import { useMemo } from 'react'

import { DateRangePicker, formatDayRange } from '@/components/common/date-range-picker'
import { EmptyState, ErrorState } from '@/components/common/error-state'
import { NativeSelect } from '@/components/common/native-select'
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

const DAY = /^\d{4}-\d{2}-\d{2}$/

/** The page's filters as the URL keeps them: ?days=7|30|90 or ?from=&to=, plus ?channel= and ?source=. */
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

  const channel = sp.get('channel')

  if (channel) {
    out.channel = channel
  }

  // An empty source is a real one (posts with none recorded), so it is kept when present.
  const source = sp.get('source')

  if (source !== null) {
    out.source = source
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
      <NativeSelect
        aria-label="Channel"
        className="w-full sm:w-48"
        value={params.channel ?? ''}
        onChange={(e) => set({ channel: e.target.value || undefined })}
      >
        <option value="">All channels</option>
        {(channels.data ?? []).map((c) => (
          <option key={c.id} value={c.id}>
            {c.name}
          </option>
        ))}
      </NativeSelect>
      <SourceSelect data={data} value={params.source} onChange={(v) => set({ source: v })} />
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
      <p className="-mt-3 text-sm text-muted-foreground" data-testid="analytics-range">
        {formatDayRange({ from: data.range.from, to: data.range.to })} · {data.range.days} days · times in{' '}
        {data.range.timezone}
      </p>
      {empty ? (
        <EmptyAnalytics hasChannels={hasChannels} filtered={!!params.channel || params.source !== undefined} />
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
  value: string | undefined
  onChange: (v: string | undefined) => void
}) {
  const sources = data?.sources ?? []
  // The chosen source stays in the list even when it has nothing in the range.
  const missing = value !== undefined && !sources.some((s) => s.source === value)

  return (
    <NativeSelect
      aria-label="Source"
      className="w-full sm:w-48"
      value={value === undefined ? '*' : `=${value}`}
      onChange={(e) => onChange(e.target.value === '*' ? undefined : e.target.value.slice(1))}
    >
      <option value="*">All sources</option>
      {sources.map((s) => (
        <option key={s.source} value={`=${s.source}`}>
          {s.label}
        </option>
      ))}
      {missing && <option value={`=${value}`}>{value || 'Not recorded'}</option>}
    </NativeSelect>
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
