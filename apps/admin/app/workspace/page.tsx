'use client'

import { useQuery } from '@tanstack/react-query'
import { CopyIcon } from 'lucide-react'
import { useSearchParams } from 'next/navigation'
import { Suspense, type ComponentProps } from 'react'
import { toast } from 'sonner'

import { PaymentsTable, type Payment } from '@/components/payments-table'
import { Shell } from '@/components/shell'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import { Card, CardContent } from '@/components/ui/card'
import { Skeleton } from '@/components/ui/skeleton'
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from '@/components/ui/table'
import { Tabs, TabsContent, TabsList, TabsTrigger } from '@/components/ui/tabs'
import { UsageMeters, type WorkspaceUsage } from '@/components/usage-meters'
import { WorkspaceActions } from '@/components/workspace-actions'
import { AdminApiError, adminApi } from '@/lib/api'
import { formatDate } from '@/lib/format'

type Detail = {
  workspace: {
    id: string
    name: string
    plan: string
    subscription_status: string
    owner_email: string
    suspended: boolean
  }
  usage: WorkspaceUsage
  members: { user_id: string; email: string; name: string; role: string; last_login_at: string | null }[]
  channels: {
    id: string
    provider: string
    name: string
    handle: string
    status: string
    last_error: string
    created_at: string
  }[]
  invitations_pending: number
  sessions_active: number
  trial: { ends_at: string | null; review: string }
}

// connected = ok, needs_reconnect = warning, disabled = muted
const CHANNEL_STATUS: Record<string, ComponentProps<typeof Badge>> = {
  connected: { variant: 'secondary' },
  needs_reconnect: { className: 'bg-amber-500/15 text-amber-700 dark:text-amber-400' },
  disabled: { variant: 'outline', className: 'text-muted-foreground' },
}

function Payments({ id }: { id: string }) {
  const q = useQuery({
    queryKey: ['admin-workspace', id, 'payments'],
    queryFn: () => adminApi.get<{ items: Payment[] }>(`/v1/admin/workspaces/${encodeURIComponent(id)}/payments`),
    retry: false,
  })

  if (q.error instanceof AdminApiError && q.error.code === 'billing_disabled') {
    return <p className="text-sm text-muted-foreground">Stripe isn&apos;t configured.</p>
  }

  if (q.isError) {
    return (
      <p role="alert" className="text-sm text-destructive">
        {q.error.message}
      </p>
    )
  }

  return q.data ? (
    <div className="overflow-x-auto">
      <PaymentsTable items={q.data.items} />
    </div>
  ) : (
    <Skeleton className="h-32 w-full" />
  )
}

function WorkspaceView() {
  const id = useSearchParams().get('id') ?? ''
  const q = useQuery({
    queryKey: ['admin-workspace', id],
    queryFn: () => adminApi.get<Detail>(`/v1/admin/workspaces/${encodeURIComponent(id)}`),
    enabled: id !== '',
  })
  const d = q.data

  return (
    <Shell title={d?.workspace.name ?? 'Workspace'}>
      {!id && <p className="text-sm text-muted-foreground">No workspace selected.</p>}
      {q.isError && (
        <p role="alert" className="text-sm text-destructive">
          {q.error.message}
        </p>
      )}
      {id && !d && !q.isError && <Skeleton className="h-64 w-full" />}
      {d && (
        <>
          <div className="flex flex-wrap items-center gap-2 text-sm">
            <code className="break-all">{d.workspace.id}</code>
            <Button
              variant="ghost"
              size="icon"
              aria-label="Copy workspace id"
              onClick={() =>
                navigator.clipboard.writeText(d.workspace.id).then(
                  () => toast.success('Copied'),
                  () => toast.error('Could not copy'),
                )
              }
            >
              <CopyIcon />
            </Button>
            <Badge variant="secondary">{d.workspace.plan}</Badge>
            <Badge variant={d.workspace.suspended ? 'destructive' : 'outline'}>
              {d.workspace.suspended ? 'suspended' : d.workspace.subscription_status || 'no subscription'}
            </Badge>
            <span className="text-muted-foreground">Owner: {d.workspace.owner_email || '—'}</span>
          </div>
          <Tabs defaultValue="usage">
            <TabsList className="h-auto max-w-full flex-wrap">
              <TabsTrigger value="usage">Usage &amp; limits</TabsTrigger>
              <TabsTrigger value="members">Members</TabsTrigger>
              <TabsTrigger value="channels">Channels</TabsTrigger>
              <TabsTrigger value="payments">Payments</TabsTrigger>
              <TabsTrigger value="actions">Actions</TabsTrigger>
            </TabsList>
            <TabsContent value="usage">
              <Card>
                <CardContent>
                  <UsageMeters usage={d.usage} trialEndsAt={d.trial?.ends_at} />
                  {d.trial?.review && (
                    <p className="mt-3 text-sm text-amber-600">Trial is flagged for review ({d.trial.review}).</p>
                  )}
                </CardContent>
              </Card>
            </TabsContent>
            <TabsContent value="members" className="space-y-3">
              <p className="text-sm text-muted-foreground">
                {d.invitations_pending} pending invitations · {d.sessions_active} active sessions
              </p>
              <Card className="overflow-x-auto p-0">
                <Table>
                  <TableHeader>
                    <TableRow>
                      <TableHead>Email</TableHead>
                      <TableHead>Name</TableHead>
                      <TableHead>Role</TableHead>
                      <TableHead>Last login</TableHead>
                    </TableRow>
                  </TableHeader>
                  <TableBody>
                    {d.members.map((m) => (
                      <TableRow key={m.user_id}>
                        <TableCell>{m.email}</TableCell>
                        <TableCell>{m.name || '—'}</TableCell>
                        <TableCell>{m.role}</TableCell>
                        <TableCell>{formatDate(m.last_login_at)}</TableCell>
                      </TableRow>
                    ))}
                  </TableBody>
                </Table>
              </Card>
            </TabsContent>
            <TabsContent value="channels">
              <Card className="overflow-x-auto p-0">
                <Table>
                  <TableHeader>
                    <TableRow>
                      <TableHead>Provider</TableHead>
                      <TableHead>Name</TableHead>
                      <TableHead>Status</TableHead>
                      <TableHead>Last error</TableHead>
                      <TableHead>Created</TableHead>
                    </TableRow>
                  </TableHeader>
                  <TableBody>
                    {d.channels.length === 0 && (
                      <TableRow>
                        <TableCell colSpan={5} className="text-muted-foreground">
                          No channels.
                        </TableCell>
                      </TableRow>
                    )}
                    {d.channels.map((c) => (
                      <TableRow key={c.id}>
                        <TableCell>{c.provider}</TableCell>
                        <TableCell>
                          {c.name || '—'}
                          {c.handle && <span className="text-muted-foreground"> @{c.handle.replace(/^@/, '')}</span>}
                        </TableCell>
                        <TableCell>
                          <Badge {...CHANNEL_STATUS[c.status]}>{c.status.replaceAll('_', ' ')}</Badge>
                        </TableCell>
                        <TableCell className="max-w-xs whitespace-normal break-words">{c.last_error || '—'}</TableCell>
                        <TableCell>{formatDate(c.created_at)}</TableCell>
                      </TableRow>
                    ))}
                  </TableBody>
                </Table>
              </Card>
            </TabsContent>
            <TabsContent value="payments">
              <Payments id={id} />
            </TabsContent>
            <TabsContent value="actions">
              <WorkspaceActions id={d.workspace.id} name={d.workspace.name} plan={d.workspace.plan} />
            </TabsContent>
          </Tabs>
        </>
      )}
    </Shell>
  )
}

export default function WorkspacePage() {
  return (
    <Suspense>
      <WorkspaceView />
    </Suspense>
  )
}
