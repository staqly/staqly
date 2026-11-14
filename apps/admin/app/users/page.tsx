'use client'

import { useInfiniteQuery } from '@tanstack/react-query'
import Link from 'next/link'
import { useEffect, useState } from 'react'

import { Shell } from '@/components/shell'
import { Button } from '@/components/ui/button'
import { Card } from '@/components/ui/card'
import { Input } from '@/components/ui/input'
import { Skeleton } from '@/components/ui/skeleton'
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from '@/components/ui/table'
import { adminApi } from '@/lib/api'
import { formatDate, formatNumber } from '@/lib/format'

type Item = {
  id: string
  email: string
  name: string
  created_at: string
  last_login_at: string | null
  workspaces: number
}
type Page = { items: Item[]; next_cursor: string }

export default function UsersPage() {
  const [input, setInput] = useState('')
  const [q, setQ] = useState('')

  useEffect(() => {
    const t = setTimeout(() => setQ(input.trim()), 300)

    return () => clearTimeout(t)
  }, [input])

  const list = useInfiniteQuery({
    queryKey: ['admin-users', q],
    initialPageParam: '',
    queryFn: ({ pageParam }) => adminApi.get<Page>(`/v1/admin/users?${new URLSearchParams({ q, cursor: pageParam })}`),
    getNextPageParam: (last) => last.next_cursor || undefined,
  })
  const items = list.data?.pages.flatMap((p) => p.items) ?? []

  return (
    <Shell title="Users">
      <Input
        type="search"
        aria-label="Search users"
        placeholder="Email or name"
        value={input}
        onChange={(e) => setInput(e.target.value)}
      />
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
                <TableHead>Email</TableHead>
                <TableHead>Name</TableHead>
                <TableHead className="text-right">Workspaces</TableHead>
                <TableHead>Created</TableHead>
                <TableHead>Last login</TableHead>
              </TableRow>
            </TableHeader>
            <TableBody>
              {items.length === 0 && (
                <TableRow>
                  <TableCell colSpan={5} className="text-muted-foreground">
                    No users.
                  </TableCell>
                </TableRow>
              )}
              {items.map((u) => (
                <TableRow key={u.id}>
                  <TableCell className="font-medium">
                    <Link href={`/user?id=${encodeURIComponent(u.id)}`} className="text-primary underline">
                      {u.email}
                    </Link>
                  </TableCell>
                  <TableCell>{u.name || '—'}</TableCell>
                  <TableCell className="tabular text-right">{formatNumber(u.workspaces)}</TableCell>
                  <TableCell>{formatDate(u.created_at)}</TableCell>
                  <TableCell>{formatDate(u.last_login_at)}</TableCell>
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
