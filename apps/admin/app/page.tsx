'use client'

import { useQuery } from '@tanstack/react-query'
import type { ReactNode } from 'react'

import { Bars } from '@/components/bars'
import { Shell } from '@/components/shell'
import { Stat } from '@/components/stat'
import { Card, CardContent, CardHeader, CardTitle } from '@/components/ui/card'
import { Skeleton } from '@/components/ui/skeleton'
import { adminApi } from '@/lib/api'
import { formatNumber } from '@/lib/format'

type Counts = Record<string, number>
type Day = { day: string; count: number }
type Overview = {
  workspaces: number
  users: number
  active_users_7d: number
  active_users_30d: number
  signups_7d: number
  signups_30d: number
  by_plan: Counts
  subscriptions: Counts
  trialing: number
  suspended: number
  channels: { total: number; by_provider: Counts; by_status: Counts }
  posts: { period: string; by_status: Counts }
  failed_targets_7d: number
  usage: Record<string, { total: number; workspaces: number }>
  signups_daily: Day[]
  published_daily: Day[]
}

function CountList({ title, counts, limit }: { title: string; counts: Counts; limit?: number }) {
  const rows = Object.entries(counts)
    .toSorted((a, b) => b[1] - a[1])
    .slice(0, limit)

  return (
    <Card>
      <CardHeader>
        <CardTitle className="text-sm">{title}</CardTitle>
      </CardHeader>
      <CardContent>
        {rows.length === 0 ? (
          <p className="text-sm text-muted-foreground">Nothing yet.</p>
        ) : (
          <dl className="space-y-1.5 text-sm">
            {rows.map(([k, n]) => (
              <div key={k} className="flex justify-between gap-4">
                <dt className="truncate capitalize">{k.replaceAll('_', ' ')}</dt>
                <dd className="tabular font-medium">{formatNumber(n)}</dd>
              </div>
            ))}
          </dl>
        )}
      </CardContent>
    </Card>
  )
}

function Chart({ title, children }: { title: string; children: ReactNode }) {
  return (
    <Card>
      <CardHeader>
        <CardTitle className="text-sm">{title}</CardTitle>
      </CardHeader>
      <CardContent>{children}</CardContent>
    </Card>
  )
}

export default function OverviewPage() {
  const q = useQuery({
    queryKey: ['admin-overview'],
    queryFn: () => adminApi.get<Overview>('/v1/admin/overview'),
  })
  const o = q.data

  return (
    <Shell title="Overview">
      {q.isError && (
        <p role="alert" className="text-sm text-destructive">
          {q.error.message}
        </p>
      )}
      {!o ? (
        <Skeleton className="h-64 w-full" />
      ) : (
        <>
          <section aria-label="Totals" className="grid grid-cols-2 gap-3 lg:grid-cols-4">
            <Stat label="Workspaces" value={o.workspaces} />
            <Stat label="Users" value={o.users} />
            <Stat label="Active users 7d" value={o.active_users_7d} />
            <Stat label="Active users 30d" value={o.active_users_30d} />
            <Stat label="Signups 7d" value={o.signups_7d} />
            <Stat label="Signups 30d" value={o.signups_30d} />
            <Stat label="Trialing" value={o.trialing} />
            <Stat label="Suspended" value={o.suspended} />
            <Stat label="Failed targets 7d" value={o.failed_targets_7d} hint="Publishing attempts that failed" />
            <Stat label="Channels" value={o.channels.total} />
          </section>
          <section aria-label="Charts" className="grid gap-3 lg:grid-cols-2">
            <Chart title="Signups, last 30 days">
              <Bars data={o.signups_daily} label="Signups per day" />
            </Chart>
            <Chart title="Published posts, last 30 days">
              <Bars data={o.published_daily} label="Published posts per day" />
            </Chart>
          </section>
          <section aria-label="Breakdowns" className="grid gap-3 sm:grid-cols-2 lg:grid-cols-3">
            <CountList title="Plans" counts={o.by_plan} />
            <CountList title="Subscription statuses" counts={o.subscriptions} />
            <CountList title="Channels by provider" counts={o.channels.by_provider} limit={8} />
            <CountList title="Channels by status" counts={o.channels.by_status} />
            <CountList title={`Posts by status (${o.posts.period || 'period'})`} counts={o.posts.by_status} />
            <CountList
              title="Usage totals"
              counts={Object.fromEntries(Object.entries(o.usage).map(([k, v]) => [k, v.total]))}
            />
          </section>
        </>
      )}
    </Shell>
  )
}
