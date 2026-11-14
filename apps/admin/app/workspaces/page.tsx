'use client'

import { useInfiniteQuery } from '@tanstack/react-query'
import Link from 'next/link'
import { useEffect, useState } from 'react'

import { Shell } from '@/components/shell'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import { Card } from '@/components/ui/card'
import { Input } from '@/components/ui/input'
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from '@/components/ui/select'
import { Skeleton } from '@/components/ui/skeleton'
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from '@/components/ui/table'
import { adminApi } from '@/lib/api'
import { formatDate, formatNumber } from '@/lib/format'

type Item = {
  id: string
  name: string
  plan: string
  subscription_status: string
  owner_email: string
  members: number
  channels: number
  posts_this_period: number
  created_at: string
  suspended: boolean
}
type Page = { items: Item[]; next_cursor: string }

const PLANS = ['starter', 'plus', 'pro', 'business']
const STATUSES = [
  ['trialing', 'Trialing'],
  ['suspended', 'Suspended (plan ended)'],
  ['active', 'Active subscription'],
  ['canceled', 'Canceled'],
] as const

export default function WorkspacesPage() {
  const [input, setInput] = useState('')
  const [q, setQ] = useState('')
  const [plan, setPlan] = useState('all')
  const [status, setStatus] = useState('all')

  useEffect(() => {
    const t = setTimeout(() => setQ(input.trim()), 300)

    return () => clearTimeout(t)
  }, [input])

  const list = useInfiniteQuery({
    queryKey: ['admin-workspaces', q, plan, status],
    initialPageParam: '',
    queryFn: ({ pageParam }) => {
      const p = new URLSearchParams({ q, cursor: pageParam })

      if (plan !== 'all') {
        p.set('plan', plan)
      }

      if (status !== 'all') {
        p.set('status', status)
      }

      return adminApi.get<Page>(`/v1/admin/workspaces?${p}`)
    },
    getNextPageParam: (last) => last.next_cursor || undefined,
  })
  const items = list.data?.pages.flatMap((p) => p.items) ?? []

  return (
    <Shell title="Workspaces">
      <div className="flex flex-wrap gap-2">
        <Input
          type="search"
          aria-label="Search workspaces"
          placeholder="Name, id or owner email"
          className="min-w-0 flex-1 basis-56"
          value={input}
          onChange={(e) => setInput(e.target.value)}
        />
        <Select value={plan} onValueChange={setPlan}>
          <SelectTrigger aria-label="Plan">
            <SelectValue />
          </SelectTrigger>
          <SelectContent>
            <SelectItem value="all">All plans</SelectItem>
            {PLANS.map((p) => (
              <SelectItem key={p} value={p}>
                {p}
              </SelectItem>
            ))}
          </SelectContent>
        </Select>
        <Select value={status} onValueChange={setStatus}>
          <SelectTrigger aria-label="Subscription status">
            <SelectValue />
          </SelectTrigger>
          <SelectContent>
            <SelectItem value="all">All statuses</SelectItem>
            {STATUSES.map(([v, label]) => (
              <SelectItem key={v} value={v}>
                {label}
              </SelectItem>
            ))}
          </SelectContent>
        </Select>
      </div>
      {list.isError && (
        <p role="alert" className="text-sm text-destructive">
          {list.error.message}
        </p>
      )}
      {list.isPending ? (
        <Skeleton className="h-64 w-full" />
      ) : (
        <Card className="overflow-x-auto p-0">
          <Table>
            <TableHeader>
              <TableRow>
                <TableHead>Name</TableHead>
                <TableHead>Owner</TableHead>
                <TableHead>Plan</TableHead>
                <TableHead>Subscription</TableHead>
                <TableHead className="text-right">Members</TableHead>
                <TableHead className="text-right">Channels</TableHead>
                <TableHead className="text-right">Posts</TableHead>
                <TableHead>Created</TableHead>
              </TableRow>
            </TableHeader>
            <TableBody>
              {items.length === 0 && (
                <TableRow>
                  <TableCell colSpan={8} className="text-muted-foreground">
                    No workspaces.
                  </TableCell>
                </TableRow>
              )}
              {items.map((w) => (
                <TableRow key={w.id}>
                  <TableCell className="font-medium">
                    <Link href={`/workspace?id=${encodeURIComponent(w.id)}`} className="text-primary underline">
                      {w.name}
                    </Link>
                  </TableCell>
                  <TableCell>{w.owner_email || '—'}</TableCell>
                  <TableCell>
                    <Badge variant={w.suspended ? 'destructive' : 'secondary'}>{w.plan}</Badge>
                  </TableCell>
                  <TableCell>{w.subscription_status || '—'}</TableCell>
                  <TableCell className="tabular text-right">{formatNumber(w.members)}</TableCell>
                  <TableCell className="tabular text-right">{formatNumber(w.channels)}</TableCell>
                  <TableCell className="tabular text-right">{formatNumber(w.posts_this_period)}</TableCell>
                  <TableCell>{formatDate(w.created_at)}</TableCell>
                </TableRow>
              ))}
            </TableBody>
          </Table>
        </Card>
      )}
      {list.hasNextPage && (
        <Button variant="outline" disabled={list.isFetchingNextPage} onClick={() => list.fetchNextPage()}>
          Load more
        </Button>
      )}
    </Shell>
  )
}
