'use client'

import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { PlusIcon } from 'lucide-react'
import { useState, type FormEvent } from 'react'
import { toast } from 'sonner'

import { ConfirmDialog } from '@/components/confirm-dialog'
import { Shell } from '@/components/shell'
import { Button } from '@/components/ui/button'
import { Card } from '@/components/ui/card'
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogHeader,
  DialogTitle,
  DialogTrigger,
} from '@/components/ui/dialog'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import { Skeleton } from '@/components/ui/skeleton'
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from '@/components/ui/table'
import { adminApi, errorText } from '@/lib/api'
import { formatDate } from '@/lib/format'

type Admin = {
  id: string
  email: string
  name: string
  created_at: string
  created_by: string
  last_login_at: string | null
}

const KEY = ['admin-admins']

function AddAdmin() {
  const [open, setOpen] = useState(false)
  const [email, setEmail] = useState('')
  const [name, setName] = useState('')
  const qc = useQueryClient()
  const m = useMutation({
    mutationFn: () => adminApi.send<Admin>('POST', '/v1/admin/admins', { email: email.trim(), name: name.trim() }),
    onSuccess: (a) => {
      toast.success(`${a.email} added`)
      setOpen(false)

      return qc.invalidateQueries({ queryKey: KEY })
    },
    onError: (e) => toast.error(errorText(e)),
  })

  function submit(e: FormEvent) {
    e.preventDefault()
    m.mutate()
  }

  return (
    <Dialog
      open={open}
      onOpenChange={(o) => {
        setOpen(o)
        setEmail('')
        setName('')
        m.reset()
      }}
    >
      <DialogTrigger asChild>
        <Button size="sm">
          <PlusIcon /> Add admin
        </Button>
      </DialogTrigger>
      <DialogContent>
        <DialogHeader>
          <DialogTitle>Add admin</DialogTitle>
          <DialogDescription>They sign in with an email code sent to this address.</DialogDescription>
        </DialogHeader>
        <form onSubmit={submit} className="space-y-4">
          <div className="space-y-2">
            <Label htmlFor="admin-email">Email</Label>
            <Input id="admin-email" type="email" required value={email} onChange={(e) => setEmail(e.target.value)} />
          </div>
          <div className="space-y-2">
            <Label htmlFor="admin-name">Name</Label>
            <Input id="admin-name" required value={name} onChange={(e) => setName(e.target.value)} />
          </div>
          <Button type="submit" disabled={m.isPending}>
            Add admin
          </Button>
        </form>
      </DialogContent>
    </Dialog>
  )
}

function Remove({ a }: { a: Admin }) {
  const qc = useQueryClient()
  const m = useMutation({
    mutationFn: () => adminApi.send('DELETE', `/v1/admin/admins/${encodeURIComponent(a.email)}`),
    onSuccess: () => {
      toast.success(`${a.email} removed`)

      return qc.invalidateQueries({ queryKey: KEY })
    },
    onError: (e) => toast.error(errorText(e)),
  })

  return (
    <ConfirmDialog
      title={`Remove ${a.email}?`}
      description="They lose access to this admin panel immediately."
      expected={a.email}
      actionLabel="Remove"
      onConfirm={() => m.mutate()}
      trigger={
        <Button variant="destructive" size="sm" aria-label={`Remove ${a.email}`}>
          Remove
        </Button>
      }
    />
  )
}

export default function AdminsPage() {
  const me = useQuery({ queryKey: ['admin-me'], queryFn: () => adminApi.get<{ email: string }>('/v1/admin/me') })
  const q = useQuery({
    queryKey: KEY,
    queryFn: async () => {
      const r = await adminApi.get<Admin[] | { items: Admin[] }>('/v1/admin/admins')

      return Array.isArray(r) ? r : r.items
    },
  })

  return (
    <Shell title="Admins" actions={<AddAdmin />}>
      {q.isPending ? (
        <Skeleton className="h-48 w-full" />
      ) : q.isError ? (
        <p role="alert" className="text-sm text-destructive">
          {errorText(q.error)}
        </p>
      ) : (
        <Card className="p-0">
          <Table>
            <TableHeader>
              <TableRow>
                <TableHead>Email</TableHead>
                <TableHead>Name</TableHead>
                <TableHead>Added</TableHead>
                <TableHead>By</TableHead>
                <TableHead>Last sign-in</TableHead>
                <TableHead />
              </TableRow>
            </TableHeader>
            <TableBody>
              {q.data.map((a) => (
                <TableRow key={a.id}>
                  <TableCell className="font-medium">{a.email}</TableCell>
                  <TableCell>{a.name}</TableCell>
                  <TableCell>{formatDate(a.created_at)}</TableCell>
                  <TableCell>{a.created_by || '-'}</TableCell>
                  <TableCell>{a.last_login_at ? formatDate(a.last_login_at) : 'never'}</TableCell>
                  <TableCell className="text-right">{a.email !== me.data?.email && <Remove a={a} />}</TableCell>
                </TableRow>
              ))}
            </TableBody>
          </Table>
        </Card>
      )}
    </Shell>
  )
}
