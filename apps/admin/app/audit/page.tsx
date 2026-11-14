'use client'

import { useInfiniteQuery } from '@tanstack/react-query'
import Link from 'next/link'
import { useEffect, useState } from 'react'

import { Shell } from '@/components/shell'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import { Card } from '@/components/ui/card'
import { Input } from '@/components/ui/input'
import { Skeleton } from '@/components/ui/skeleton'
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from '@/components/ui/table'
import { adminApi } from '@/lib/api'
import { formatDate } from '@/lib/format'

type Row = {
  id: string
  email: string
  action: string
  workspace_id: string
  target: string
  details: Record<string, unknown> | null
  created_at: string
}
type Page = { items: Row[]; next_cursor: string }

export default function AuditPage() {
  const [text, setText] = useState('')
  const [term, setTerm] = useState('')

  useEffect(() => {
    const t = setTimeout(() => setTerm(text.trim()), 300)

    return () => clearTimeout(t)
  }, [text])

  const q = useInfiniteQuery({
    queryKey: ['admin-audit', term],
    initialPageParam: '',
    queryFn: ({ pageParam }) =>
      adminApi.get<Page>(`/v1/admin/audit?${new URLSearchParams({ q: term, cursor: pageParam })}`),
    getNextPageParam: (last) => last.next_cursor || undefined,
  })
  const rows = q.data?.pages.flatMap((p) => p.items) ?? []

  return (
    <Shell title="Audit log">
      <Input
        type="search"
        aria-label="Search audit log"
        placeholder="Search by admin, action, target…"
        value={text}
        onChange={(e) => setText(e.target.value)}
        className="max-w-sm"
      />
      {q.isError && (
        <p role="alert" className="text-sm text-destructive">
          {q.error.message}
        </p>
      )}
      {q.isPending ? (
        <Skeleton className="h-48 w-full" />
      ) : (
        <Card className="p-0">
          <Table>
            <TableHeader>
              <TableRow>
                <TableHead>Time</TableHead>
                <TableHead>Admin</TableHead>
                <TableHead>Action</TableHead>
                <TableHead>Workspace</TableHead>
                <TableHead>Target</TableHead>
                <TableHead>Details</TableHead>
              </TableRow>
            </TableHeader>
            <TableBody>
              {rows.length === 0 && (
                <TableRow>
                  <TableCell colSpan={6} className="text-muted-foreground">
                    Nothing logged.
                  </TableCell>
                </TableRow>
              )}
              {rows.map((r) => {
                const d = r.details && Object.keys(r.details).length > 0 ? JSON.stringify(r.details) : ''

                return (
                  <TableRow key={r.id}>
                    <TableCell className="whitespace-nowrap">{formatDate(r.created_at)}</TableCell>
                    <TableCell>{r.email}</TableCell>
                    <TableCell>
                      <Badge variant="secondary">{r.action}</Badge>
                    </TableCell>
                    <TableCell>
                      {r.workspace_id ? (
                        <Link
                          href={`/workspace?id=${encodeURIComponent(r.workspace_id)}`}
                          className="font-mono text-xs underline"
                        >
                          {r.workspace_id}
                        </Link>
                      ) : (
                        '—'
                      )}
                    </TableCell>
                    <TableCell className="max-w-40 truncate" title={r.target ?? undefined}>
                      {r.target || '—'}
                    </TableCell>
                    <TableCell className="max-w-56">
                      {d ? (
                        <code className="block truncate text-xs" title={d}>
                          {d}
                        </code>
                      ) : (
                        '—'
                      )}
                    </TableCell>
                  </TableRow>
                )
              })}
            </TableBody>
          </Table>
        </Card>
      )}
      {q.hasNextPage && (
        <Button variant="outline" onClick={() => q.fetchNextPage()} disabled={q.isFetchingNextPage}>
          {q.isFetchingNextPage ? 'Loading…' : 'Load more'}
        </Button>
      )}
    </Shell>
  )
}
