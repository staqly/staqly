'use client'

import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { PlusIcon } from 'lucide-react'
import { useState } from 'react'
import { toast } from 'sonner'

import { PromoFormView } from '@/components/promo-form'
import { Shell } from '@/components/shell'
import {
  AlertDialog,
  AlertDialogAction,
  AlertDialogCancel,
  AlertDialogContent,
  AlertDialogDescription,
  AlertDialogFooter,
  AlertDialogHeader,
  AlertDialogTitle,
  AlertDialogTrigger,
} from '@/components/ui/alert-dialog'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import { Card, CardContent } from '@/components/ui/card'
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogHeader,
  DialogTitle,
  DialogTrigger,
} from '@/components/ui/dialog'
import { Skeleton } from '@/components/ui/skeleton'
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from '@/components/ui/table'
import { AdminApiError, adminApi, errorText } from '@/lib/api'
import { formatDate, formatMoney } from '@/lib/format'

type Promo = {
  id: string
  code: string
  active: boolean
  coupon: {
    percent_off: number // 0 when the coupon is an amount
    amount_off: number // minor units, 0 when a percentage
    currency: string
    duration: 'once' | 'repeating' | 'forever'
    duration_in_months: number
  }
  times_redeemed: number
  max_redemptions: number // 0 = unlimited
  expires_at: string // '' = never
}

const KEY = ['admin-promo-codes']

function discount(c: Promo['coupon']) {
  return c.percent_off ? `${c.percent_off}% off` : `${formatMoney(c.amount_off, c.currency || 'usd')} off`
}

function duration(c: Promo['coupon']) {
  return c.duration === 'repeating' ? `${c.duration_in_months} months` : c.duration
}

function Deactivate({ p }: { p: Promo }) {
  const qc = useQueryClient()
  const m = useMutation({
    mutationFn: () => adminApi.send('POST', `/v1/admin/promotion-codes/${encodeURIComponent(p.id)}/deactivate`),
    onSuccess: () => {
      toast.success(`${p.code} deactivated`)

      return qc.invalidateQueries({ queryKey: KEY })
    },
    onError: (e) => toast.error(e.message),
  })

  return (
    <AlertDialog>
      <AlertDialogTrigger asChild>
        <Button variant="destructive" size="sm" aria-label={`Deactivate ${p.code}`}>
          Deactivate
        </Button>
      </AlertDialogTrigger>
      <AlertDialogContent>
        <AlertDialogHeader>
          <AlertDialogTitle>Deactivate {p.code}?</AlertDialogTitle>
          <AlertDialogDescription>Nobody can redeem it any more. Existing discounts stay.</AlertDialogDescription>
        </AlertDialogHeader>
        <AlertDialogFooter>
          <AlertDialogCancel>Cancel</AlertDialogCancel>
          <AlertDialogAction variant="destructive" onClick={() => m.mutate()}>
            Deactivate
          </AlertDialogAction>
        </AlertDialogFooter>
      </AlertDialogContent>
    </AlertDialog>
  )
}

function NewCode() {
  const [open, setOpen] = useState(false)
  const qc = useQueryClient()
  const m = useMutation({
    mutationFn: (body: Record<string, unknown>) => adminApi.send<Promo>('POST', '/v1/admin/promotion-codes', body),
    onSuccess: (p) => {
      toast.success(`${p.code} created`)
      setOpen(false)

      return qc.invalidateQueries({ queryKey: KEY })
    },
  })

  return (
    <Dialog
      open={open}
      onOpenChange={(o) => {
        setOpen(o)
        m.reset()
      }}
    >
      <DialogTrigger asChild>
        <Button size="sm">
          <PlusIcon /> New code
        </Button>
      </DialogTrigger>
      <DialogContent>
        <DialogHeader>
          <DialogTitle>New promo code</DialogTitle>
          <DialogDescription>Creates a Stripe coupon and a code customers enter at checkout.</DialogDescription>
        </DialogHeader>
        <PromoFormView
          busy={m.isPending}
          serverError={m.error ? errorText(m.error) : undefined}
          serverFields={m.error instanceof AdminApiError ? m.error.fields : undefined}
          onSubmit={(b) => m.mutate(b)}
        />
      </DialogContent>
    </Dialog>
  )
}

export default function PromoCodesPage() {
  const q = useQuery({
    queryKey: KEY,
    queryFn: () => adminApi.get<{ items: Promo[] }>('/v1/admin/promotion-codes'),
  })
  const off = q.error instanceof AdminApiError && q.error.code === 'billing_disabled'

  return (
    <Shell title="Promo codes" actions={off ? undefined : <NewCode />}>
      {off ? (
        <Card>
          <CardContent>Stripe isn&apos;t configured on this server.</CardContent>
        </Card>
      ) : q.isError ? (
        <p role="alert" className="text-sm text-destructive">
          {q.error.message}
        </p>
      ) : !q.data ? (
        <Skeleton className="h-48 w-full" />
      ) : (
        <Card className="p-0">
          <Table>
            <TableHeader>
              <TableRow>
                <TableHead>Code</TableHead>
                <TableHead>Discount</TableHead>
                <TableHead>Duration</TableHead>
                <TableHead>Redeemed</TableHead>
                <TableHead>Expires</TableHead>
                <TableHead>Status</TableHead>
                <TableHead>
                  <span className="sr-only">Actions</span>
                </TableHead>
              </TableRow>
            </TableHeader>
            <TableBody>
              {q.data.items.length === 0 && (
                <TableRow>
                  <TableCell colSpan={7} className="text-muted-foreground">
                    No promo codes yet.
                  </TableCell>
                </TableRow>
              )}
              {q.data.items.map((p) => (
                <TableRow key={p.id}>
                  <TableCell className="font-mono">{p.code}</TableCell>
                  <TableCell>{discount(p.coupon)}</TableCell>
                  <TableCell>{duration(p.coupon)}</TableCell>
                  <TableCell className="tabular">
                    {p.times_redeemed}
                    {p.max_redemptions ? ` / ${p.max_redemptions}` : ''}
                  </TableCell>
                  <TableCell>{p.expires_at ? formatDate(p.expires_at) : '—'}</TableCell>
                  <TableCell>
                    <Badge variant={p.active ? 'default' : 'secondary'}>{p.active ? 'Active' : 'Inactive'}</Badge>
                  </TableCell>
                  <TableCell className="text-right">{p.active && <Deactivate p={p} />}</TableCell>
                </TableRow>
              ))}
            </TableBody>
          </Table>
        </Card>
      )}
    </Shell>
  )
}
