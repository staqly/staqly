'use client'

import { useInfiniteQuery } from '@tanstack/react-query'
import { useState, type ComponentProps } from 'react'

import { PaymentsTable } from '@/components/payments-table'
import { Shell } from '@/components/shell'
import { Stat } from '@/components/stat'
import { Button } from '@/components/ui/button'
import { Card, CardContent } from '@/components/ui/card'
import { Skeleton } from '@/components/ui/skeleton'
import { Tabs, TabsList, TabsTrigger } from '@/components/ui/tabs'
import { AdminApiError, adminApi } from '@/lib/api'
import { formatMoney } from '@/lib/format'

type Item = ComponentProps<typeof PaymentsTable>['items'][number]
type Page = { items: Item[]; next_cursor: string }

const FILTERS = [
  ['all', 'All'],
  ['paid', 'Paid'],
  ['open', 'Open'],
  ['failed', 'Failed'],
] as const

export default function PaymentsPage() {
  const [status, setStatus] = useState<string>('all')
  const q = useInfiniteQuery({
    queryKey: ['admin-payments', status],
    initialPageParam: '',
    queryFn: ({ pageParam }) => {
      const p = new URLSearchParams({ status: status === 'all' ? '' : status, cursor: pageParam })

      return adminApi.get<Page>(`/v1/admin/payments?${p}`)
    },
    getNextPageParam: (last) => last.next_cursor || undefined,
    retry: (n, e) => !(e instanceof AdminApiError && e.status === 409) && n < 2,
  })
  const items = q.data?.pages.flatMap((p) => p.items) ?? []
  const paid = items.filter((i) => i.status === 'paid')
  const failed = items.filter((i) => (i.status === 'open' && i.attempt_count > 0) || i.status === 'uncollectible')
  const totals = new Map<string, number>()

  for (const i of paid) {
    totals.set(i.currency, (totals.get(i.currency) ?? 0) + i.amount_paid)
  }

  const off = q.error instanceof AdminApiError && q.error.code === 'billing_disabled'

  return (
    <Shell title="Payments">
      {off ? (
        <Card>
          <CardContent>Stripe isn&apos;t configured on this server.</CardContent>
        </Card>
      ) : (
        <>
          <Tabs value={status} onValueChange={setStatus}>
            <TabsList>
              {FILTERS.map(([v, label]) => (
                <TabsTrigger key={v} value={v}>
                  {label}
                </TabsTrigger>
              ))}
            </TabsList>
          </Tabs>
          {q.isError && (
            <p role="alert" className="text-sm text-destructive">
              {q.error.message}
            </p>
          )}
          {q.isPending ? (
            <Skeleton className="h-48 w-full" />
          ) : (
            <>
              <section aria-label="Summary of loaded invoices" className="grid grid-cols-2 gap-3 lg:grid-cols-4">
                <Stat label="Paid" value={paid.length} />
                <Stat label="Failed" value={failed.length} hint="Retrying or uncollectible" />
                {[...totals].map(([cur, sum]) => (
                  <Stat key={cur} label={`Paid total ${cur.toUpperCase()}`} value={formatMoney(sum, cur)} />
                ))}
              </section>
              <PaymentsTable items={items} />
              {q.hasNextPage && (
                <Button variant="outline" onClick={() => q.fetchNextPage()} disabled={q.isFetchingNextPage}>
                  {q.isFetchingNextPage ? 'Loading…' : 'Load more'}
                </Button>
              )}
            </>
          )}
        </>
      )}
    </Shell>
  )
}
