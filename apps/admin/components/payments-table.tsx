import Link from 'next/link'

import { Badge } from '@/components/ui/badge'
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from '@/components/ui/table'
import { formatDate, formatMoney } from '@/lib/format'

export type Payment = {
  id: string
  number: string
  workspace_id: string
  workspace_name: string
  customer_email: string
  status: 'paid' | 'open' | 'uncollectible' | 'void'
  amount_due: number
  amount_paid: number
  currency: string
  attempt_count: number
  created: string
  period_end: string
  next_payment_attempt: string | null
  hosted_invoice_url: string
}

function StatusBadge({ p }: { p: Payment }) {
  if (p.status === 'paid') {
    return <Badge className="bg-emerald-500/15 text-emerald-700 dark:text-emerald-400">paid</Badge>
  }

  if (p.status === 'open') {
    return p.attempt_count > 0 ? (
      <Badge variant="destructive">failed</Badge>
    ) : (
      <Badge className="bg-amber-500/15 text-amber-700 dark:text-amber-400">open</Badge>
    )
  }

  return <Badge variant={p.status === 'uncollectible' ? 'destructive' : 'secondary'}>{p.status}</Badge>
}

export function PaymentsTable({ items }: { items: Payment[] }) {
  if (items.length === 0) {
    return <p className="text-sm text-muted-foreground">No invoices.</p>
  }

  return (
    <Table>
      <TableHeader>
        <TableRow>
          <TableHead>Invoice</TableHead>
          <TableHead>Workspace</TableHead>
          <TableHead>Email</TableHead>
          <TableHead>Status</TableHead>
          <TableHead className="text-right">Amount</TableHead>
          <TableHead>Created</TableHead>
          <TableHead>Period end</TableHead>
          <TableHead>Next attempt</TableHead>
          <TableHead>
            <span className="sr-only">Link</span>
          </TableHead>
        </TableRow>
      </TableHeader>
      <TableBody>
        {items.map((p) => (
          <TableRow key={p.id}>
            <TableCell className="font-medium">{p.number || p.id}</TableCell>
            <TableCell>
              {p.workspace_id ? (
                <Link href={`/workspace?id=${encodeURIComponent(p.workspace_id)}`} className="text-primary underline">
                  {p.workspace_name || p.workspace_id}
                </Link>
              ) : (
                '—'
              )}
            </TableCell>
            <TableCell>{p.customer_email || '—'}</TableCell>
            <TableCell>
              <StatusBadge p={p} />
            </TableCell>
            <TableCell className="tabular text-right">
              {formatMoney(p.status === 'paid' ? p.amount_paid : p.amount_due, p.currency)}
            </TableCell>
            <TableCell>{formatDate(p.created)}</TableCell>
            <TableCell>{formatDate(p.period_end)}</TableCell>
            <TableCell>{formatDate(p.next_payment_attempt)}</TableCell>
            <TableCell>
              {p.hosted_invoice_url && (
                <a
                  href={p.hosted_invoice_url}
                  target="_blank"
                  rel="noopener noreferrer"
                  className="text-primary underline"
                >
                  Open<span className="sr-only"> invoice {p.number}</span>
                </a>
              )}
            </TableCell>
          </TableRow>
        ))}
      </TableBody>
    </Table>
  )
}
