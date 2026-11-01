'use client'

import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import Link from 'next/link'
import { useRouter, useSearchParams } from 'next/navigation'
import { Suspense } from 'react'
import { toast } from 'sonner'

import { ConfirmDialog } from '@/components/confirm-dialog'
import { Shell } from '@/components/shell'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import { Card, CardContent, CardHeader, CardTitle } from '@/components/ui/card'
import { Skeleton } from '@/components/ui/skeleton'
import { adminApi } from '@/lib/api'
import { formatDate } from '@/lib/format'

type Detail = {
  user: { id: string; email: string; name: string; created_at: string; last_login_at: string | null }
  workspaces: { id: string; name: string; plan: string; role: string }[]
  identities: { provider: string }[]
  sessions_active: number
}

function UserView() {
  const id = useSearchParams().get('id') ?? ''
  const qc = useQueryClient()
  const router = useRouter()
  const base = `/v1/admin/users/${encodeURIComponent(id)}`
  const q = useQuery({
    queryKey: ['admin-user', id],
    queryFn: () => adminApi.get<Detail>(base),
    enabled: id !== '',
  })
  const revoke = useMutation({
    mutationFn: () => adminApi.send<{ revoked: number }>('POST', `${base}/sessions/revoke`),
    onSuccess: (r) => {
      toast.success(`Revoked ${r.revoked} sessions`)
      void qc.invalidateQueries({ queryKey: ['admin-user', id] })
    },
    onError: (e) => toast.error(e.message),
  })
  const del = useMutation({
    mutationFn: (email: string) => adminApi.send('DELETE', base, { confirm: email }),
    onSuccess: () => {
      toast.success('User deleted')
      router.push('/users')
    },
    onError: (e) => toast.error(e.message),
  })
  const d = q.data

  return (
    <Shell title={d?.user.email ?? 'User'}>
      {!id && <p className="text-sm text-muted-foreground">No user selected.</p>}
      {q.isError && (
        <p role="alert" className="text-sm text-destructive">
          {q.error.message}
        </p>
      )}
      {id && !d && !q.isError && <Skeleton className="h-64 w-full" />}
      {d && (
        <div className="grid gap-3 lg:grid-cols-2">
          <Card>
            <CardHeader>
              <CardTitle className="text-sm">Details</CardTitle>
            </CardHeader>
            <CardContent>
              <dl className="space-y-1.5 text-sm">
                <div className="flex justify-between gap-4">
                  <dt className="text-muted-foreground">Email</dt>
                  <dd className="break-all">{d.user.email}</dd>
                </div>
                <div className="flex justify-between gap-4">
                  <dt className="text-muted-foreground">Name</dt>
                  <dd>{d.user.name || '—'}</dd>
                </div>
                <div className="flex justify-between gap-4">
                  <dt className="text-muted-foreground">Created</dt>
                  <dd>{formatDate(d.user.created_at)}</dd>
                </div>
                <div className="flex justify-between gap-4">
                  <dt className="text-muted-foreground">Last login</dt>
                  <dd>{formatDate(d.user.last_login_at)}</dd>
                </div>
                <div className="flex justify-between gap-4">
                  <dt className="text-muted-foreground">Active sessions</dt>
                  <dd>{d.sessions_active}</dd>
                </div>
                <div className="flex justify-between gap-4">
                  <dt className="text-muted-foreground">Sign-in methods</dt>
                  <dd className="flex gap-1">
                    {d.identities.length === 0 && '—'}
                    {d.identities.map((i) => (
                      <Badge key={i.provider} variant="secondary">
                        {i.provider}
                      </Badge>
                    ))}
                  </dd>
                </div>
              </dl>
            </CardContent>
          </Card>
          <Card>
            <CardHeader>
              <CardTitle className="text-sm">Workspaces</CardTitle>
            </CardHeader>
            <CardContent>
              {d.workspaces.length === 0 && <p className="text-sm text-muted-foreground">None.</p>}
              <ul className="space-y-1.5 text-sm">
                {d.workspaces.map((w) => (
                  <li key={w.id} className="flex items-center justify-between gap-4">
                    <Link
                      href={`/workspace?id=${encodeURIComponent(w.id)}`}
                      className="truncate text-primary underline"
                    >
                      {w.name}
                    </Link>
                    <span className="flex gap-1">
                      <Badge variant="secondary">{w.plan}</Badge>
                      <Badge variant="outline">{w.role}</Badge>
                    </span>
                  </li>
                ))}
              </ul>
            </CardContent>
          </Card>
          <Card className="lg:col-span-2">
            <CardHeader>
              <CardTitle className="text-sm">Actions</CardTitle>
            </CardHeader>
            <CardContent className="flex flex-wrap gap-2">
              <Button variant="outline" disabled={revoke.isPending} onClick={() => revoke.mutate()}>
                Revoke sessions
              </Button>
              <ConfirmDialog
                title="Delete user?"
                description="The account and its identities are removed. Workspaces where this person is the only member are deleted with them."
                expected={d.user.email}
                actionLabel="Delete user"
                trigger={<Button variant="destructive">Delete user</Button>}
                onConfirm={() => del.mutate(d.user.email)}
              />
            </CardContent>
          </Card>
        </div>
      )}
    </Shell>
  )
}

export default function UserPage() {
  return (
    <Suspense>
      <UserView />
    </Suspense>
  )
}
