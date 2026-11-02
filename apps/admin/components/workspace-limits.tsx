'use client'

import { useQuery, useQueryClient } from '@tanstack/react-query'
import { useState } from 'react'

import { LimitEditor } from '@/components/limit-editor'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import { Card } from '@/components/ui/card'
import { Skeleton } from '@/components/ui/skeleton'
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from '@/components/ui/table'
import { adminApi, errorText } from '@/lib/api'
import { formatLimit } from '@/lib/limits'

type Row = { key: string; description: string; value: number; source: string; plan: number; note: string }

/** A workspace's limits before Stripe add-ons: its plan's value, its own where an admin set one, and why. */
export function WorkspaceLimits({ id, plan }: { id: string; plan: string }) {
  const qc = useQueryClient()
  const key = ['admin-workspace', id, 'limits']
  const q = useQuery({
    queryKey: key,
    queryFn: () => adminApi.get<{ data: Row[] }>(`/v1/admin/workspaces/${encodeURIComponent(id)}/limits`),
  })
  const [editing, setEditing] = useState<Row | null>(null)
  const refresh = () =>
    Promise.all([qc.invalidateQueries({ queryKey: key }), qc.invalidateQueries({ queryKey: ['admin-workspace', id] })])
  const path = (r: Row) => `/v1/admin/workspaces/${encodeURIComponent(id)}/limits/${encodeURIComponent(r.key)}`

  if (q.isPending) {
    return <Skeleton className="h-64 w-full" />
  }

  if (q.isError) {
    return (
      <p role="alert" className="text-sm text-destructive">
        {errorText(q.error)}
      </p>
    )
  }

  return (
    <Card className="overflow-x-auto p-0">
      <Table>
        <TableHeader>
          <TableRow>
            <TableHead>Limit</TableHead>
            <TableHead className="capitalize">{plan} plan</TableHead>
            <TableHead>This workspace</TableHead>
            <TableHead>Why</TableHead>
            <TableHead />
          </TableRow>
        </TableHeader>
        <TableBody>
          {q.data.data.map((r) => {
            const own = r.source === 'workspace'

            return (
              <TableRow key={r.key}>
                <TableCell>
                  <div className="font-medium">{r.key}</div>
                  <div className="max-w-xs text-xs whitespace-normal text-muted-foreground">{r.description}</div>
                </TableCell>
                <TableCell className={own ? 'text-muted-foreground line-through' : undefined}>
                  {formatLimit(r.key, r.plan)}
                </TableCell>
                <TableCell>
                  {own ? (
                    <Badge variant="secondary">{formatLimit(r.key, r.value)}</Badge>
                  ) : (
                    <span className="text-muted-foreground">as the plan</span>
                  )}
                </TableCell>
                <TableCell className="max-w-xs text-sm whitespace-normal">{r.note || '—'}</TableCell>
                <TableCell className="text-right">
                  <Button size="sm" variant="outline" onClick={() => setEditing(r)}>
                    {own ? 'Change' : 'Set own value'}
                  </Button>
                </TableCell>
              </TableRow>
            )
          })}
        </TableBody>
      </Table>
      {editing && (
        <LimitEditor
          limitKey={editing.key}
          title={`${editing.key} for this workspace`}
          description="Replaces the plan's value for this workspace only; Stripe add-ons still add to it."
          value={editing.value}
          withNote
          note={editing.note}
          onSave={(value, note) => adminApi.send('PUT', path(editing), { value, note }).then(refresh)}
          onClear={
            editing.source === 'workspace' ? () => adminApi.send('DELETE', path(editing)).then(refresh) : undefined
          }
          clearLabel="Back to the plan's value"
          onClose={() => setEditing(null)}
        />
      )}
    </Card>
  )
}
