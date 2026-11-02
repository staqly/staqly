'use client'

import { useQuery, useQueryClient } from '@tanstack/react-query'
import { useState } from 'react'

import { LimitEditor } from '@/components/limit-editor'
import { Shell } from '@/components/shell'
import { Card } from '@/components/ui/card'
import { Skeleton } from '@/components/ui/skeleton'
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from '@/components/ui/table'
import { adminApi, errorText } from '@/lib/api'
import { formatLimit } from '@/lib/limits'

type Limit = { key: string; description: string; default: number; plans: Record<string, number> }
type Limits = { plans: string[]; limits: Limit[] }

const KEY = ['admin-limits']

// One cell being edited: a limit's default (plan ''), or a plan's value.
type Editing = { limit: Limit; plan: string }

export default function LimitsPage() {
  const qc = useQueryClient()
  const q = useQuery({ queryKey: KEY, queryFn: () => adminApi.get<Limits>('/v1/admin/limits') })
  const [editing, setEditing] = useState<Editing | null>(null)
  const refresh = () => qc.invalidateQueries({ queryKey: KEY })
  const path = (e: Editing) =>
    `/v1/admin/limits/${encodeURIComponent(e.limit.key)}${e.plan ? `/plans/${encodeURIComponent(e.plan)}` : ''}`

  return (
    <Shell title="Limits">
      <p className="mb-4 max-w-3xl text-sm text-muted-foreground">
        What each plan allows. A plan without its own value takes the default (shown faded). A workspace can get its own
        value on its page (Limits tab); Stripe add-ons add to whichever applies. Changes apply at once and go to the
        audit log.
      </p>
      {q.isPending ? (
        <Skeleton className="h-96 w-full" />
      ) : q.isError ? (
        <p role="alert" className="text-sm text-destructive">
          {errorText(q.error)}
        </p>
      ) : (
        <Card className="overflow-x-auto p-0">
          <Table>
            <TableHeader>
              <TableRow>
                <TableHead>Limit</TableHead>
                <TableHead>Default</TableHead>
                {q.data.plans.map((p) => (
                  <TableHead key={p} className="capitalize">
                    {p}
                  </TableHead>
                ))}
              </TableRow>
            </TableHeader>
            <TableBody>
              {q.data.limits.map((l) => (
                <TableRow key={l.key}>
                  <TableCell>
                    <div className="font-medium">{l.key}</div>
                    <div className="max-w-xs text-xs whitespace-normal text-muted-foreground">{l.description}</div>
                  </TableCell>
                  <TableCell>
                    <button
                      type="button"
                      className="cursor-pointer rounded px-1 font-medium underline-offset-2 hover:underline"
                      onClick={() => setEditing({ limit: l, plan: '' })}
                    >
                      {formatLimit(l.key, l.default)}
                    </button>
                  </TableCell>
                  {q.data.plans.map((p) => {
                    const own = p in l.plans

                    return (
                      <TableCell key={p}>
                        <button
                          type="button"
                          aria-label={`${l.key} for ${p}`}
                          className={`cursor-pointer rounded px-1 underline-offset-2 hover:underline ${own ? 'font-medium' : 'text-muted-foreground'}`}
                          onClick={() => setEditing({ limit: l, plan: p })}
                        >
                          {formatLimit(l.key, own ? l.plans[p] : l.default)}
                        </button>
                      </TableCell>
                    )
                  })}
                </TableRow>
              ))}
            </TableBody>
          </Table>
        </Card>
      )}
      {editing && (
        <LimitEditor
          limitKey={editing.limit.key}
          title={`${editing.limit.key}: ${editing.plan || 'default'}`}
          description={
            editing.plan
              ? `The ${editing.plan} plan's own value. Without one it takes the default.`
              : 'Every plan without its own value takes this one.'
          }
          value={
            editing.plan && editing.plan in editing.limit.plans
              ? editing.limit.plans[editing.plan]
              : editing.limit.default
          }
          onSave={(value) => adminApi.send('PUT', path(editing), { value }).then(refresh)}
          onClear={
            editing.plan && editing.plan in editing.limit.plans
              ? () => adminApi.send('DELETE', path(editing)).then(refresh)
              : undefined
          }
          clearLabel="Use the default"
          onClose={() => setEditing(null)}
        />
      )}
    </Shell>
  )
}
