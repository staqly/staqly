'use client'

import { useMutation } from '@tanstack/react-query'
import { CreditCard, ExternalLink, FileText, Receipt } from 'lucide-react'
import { useState } from 'react'

import { EmptyState } from '@/components/common/error-state'
import { Button } from '@/components/ui/button'
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from '@/components/ui/table'
import { api, type BillingDetails } from '@/lib/api'
import { toastApiError } from '@/lib/notify'
import { safeHttpUrl } from '@/lib/safe'
import { TONES, type Tone } from '@/lib/tones'
import { cn } from '@/lib/utils'

import { cents, shortDate } from './billing-format'

type Invoice = BillingDetails['invoices'][number]

const INVOICE_STATUS: Record<string, [string, Tone]> = {
  paid: ['Paid', 'success'],
  open: ['Due', 'warning'],
  void: ['Void', 'neutral'],
  uncollectible: ['Not paid', 'danger'],
}

/** A card inside a Plan & usage group: a small heading, a line under it, an action on the right. */
function Panel({
  title,
  description,
  icon,
  action,
  children,
  testId,
}: {
  title: string
  description?: React.ReactNode
  icon?: React.ReactNode
  action?: React.ReactNode
  children: React.ReactNode
  testId?: string
}) {
  return (
    <div className="grid content-start gap-3 rounded-xl border bg-card p-4" data-testid={testId}>
      <div className="flex flex-wrap items-start justify-between gap-3">
        <div className="flex min-w-0 flex-1 basis-48 items-start gap-2.5">
          {icon && <span className="mt-0.5 text-muted-foreground [&_svg]:size-4">{icon}</span>}
          <div className="min-w-0">
            <h3 className="text-sm font-semibold">{title}</h3>
            {description && <p className="text-sm text-muted-foreground">{description}</p>}
          </div>
        </div>
        {action && <div className="shrink-0">{action}</div>}
      </div>
      {children}
    </div>
  )
}

export function Pill({ label, tone }: { label: string; tone: Tone }) {
  return (
    <span
      className={cn(
        'inline-flex h-5 items-center rounded-full px-2 text-xs font-medium whitespace-nowrap ring-1 ring-inset',
        TONES[tone].badge,
      )}
    >
      {label}
    </span>
  )
}

/** The cards on file (brand and last four digits only; Stripe keeps the cards). */
export function PaymentMethods({
  details,
  onManage,
  managing,
}: {
  details: BillingDetails
  /** Opens Stripe's billing portal; absent for people who may not change it. */
  onManage?: () => void
  managing?: boolean
}) {
  return (
    <Panel
      title="Payment method"
      icon={<CreditCard />}
      description="Kept by Stripe. Staqly sees only the brand, the last four digits and the expiry."
      action={
        onManage && (
          <Button variant="outline" disabled={managing} onClick={onManage}>
            Update <ExternalLink />
          </Button>
        )
      }
    >
      {details.payment_methods.length === 0 ? (
        <p className="text-sm text-muted-foreground" data-testid="no-payment-method">
          {details.managed_payments
            ? 'No card on file here. Payments made through Link are managed in your Link account.'
            : 'No card on file yet. It is added when you start a plan.'}
        </p>
      ) : (
        <ul className="grid gap-2" data-testid="payment-methods">
          {details.payment_methods.map((pm) => (
            <li
              key={`${pm.brand}-${pm.last4}-${pm.exp_year}`}
              className="flex flex-wrap items-center gap-x-3 gap-y-1 rounded-lg border bg-background px-3 py-2.5 text-sm"
            >
              <CreditCard className="size-4 text-muted-foreground" aria-hidden />
              <span className="font-medium capitalize">{pm.brand}</span>
              <span className="font-mono tabular-nums">•••• {pm.last4}</span>
              <span className="text-muted-foreground tabular-nums">
                Expires {String(pm.exp_month).padStart(2, '0')}/{pm.exp_year}
              </span>
              {pm.default && <Pill label="Default" tone="success" />}
            </li>
          ))}
        </ul>
      )}
    </Panel>
  )
}

function InvoiceRows({ invoices }: { invoices: Invoice[] }) {
  return (
    <div className="overflow-x-auto">
      <Table>
        <TableHeader>
          <TableRow>
            <TableHead>Date</TableHead>
            <TableHead>Number</TableHead>
            <TableHead className="text-right">Amount</TableHead>
            <TableHead>Status</TableHead>
            <TableHead>
              <span className="sr-only">Links</span>
            </TableHead>
          </TableRow>
        </TableHeader>
        <TableBody>
          {invoices.map((inv) => {
            const [label, tone] = INVOICE_STATUS[inv.status] ?? [inv.status, 'neutral' as Tone]
            const hosted = safeHttpUrl(inv.hosted_invoice_url)
            const pdf = safeHttpUrl(inv.invoice_pdf)

            return (
              <TableRow key={inv.id} data-testid="invoice-row">
                <TableCell className="whitespace-nowrap">{shortDate(inv.created)}</TableCell>
                <TableCell className="font-mono text-xs">{inv.number || '-'}</TableCell>
                <TableCell className="text-right tabular-nums">{cents(inv.total, inv.currency)}</TableCell>
                <TableCell>
                  <Pill label={label} tone={tone} />
                </TableCell>
                <TableCell className="text-right whitespace-nowrap">
                  {hosted && (
                    <Button asChild variant="ghost" size="sm">
                      <a href={hosted} target="_blank" rel="noopener noreferrer">
                        {inv.status === 'open' ? 'Pay' : 'View'} <ExternalLink />
                      </a>
                    </Button>
                  )}
                  {pdf && (
                    <Button asChild variant="ghost" size="sm">
                      <a
                        href={pdf}
                        target="_blank"
                        rel="noopener noreferrer"
                        aria-label={`PDF of ${inv.number || 'the invoice'}`}
                      >
                        <FileText /> PDF
                      </a>
                    </Button>
                  )}
                </TableCell>
              </TableRow>
            )
          })}
        </TableBody>
      </Table>
    </div>
  )
}

/**
 * The invoices Stripe has for this workspace, newest first, 24 a page. With Managed Payments Stripe sells through
 * Link, which sends the receipts and invoices itself: the list may then stay empty, and says where they are.
 */
export function Invoices({ details }: { details: BillingDetails }) {
  const [older, setOlder] = useState<Invoice[]>([])
  const [hasMore, setHasMore] = useState<boolean | null>(null)
  const more = useMutation({
    mutationFn: (after: string) => api.billingDetails(after),
    onSuccess: (d) => {
      setOlder((o) => [...o, ...d.invoices])
      setHasMore(d.has_more_invoices)
    },
    onError: (e) => toastApiError(e),
  })
  const invoices = [...details.invoices, ...older]
  const moreLeft = hasMore ?? details.has_more_invoices
  const link = details.managed_payments

  return (
    <Panel
      title="Invoices"
      icon={<Receipt />}
      description={
        link
          ? 'Payments go through Link, which emails a receipt and invoice for each one. Copies Stripe has show here.'
          : 'Issued by Stripe for this workspace, newest first.'
      }
    >
      {invoices.length === 0 ? (
        <EmptyState
          className="py-6"
          icon={<Receipt />}
          title="No invoices yet"
          description={
            link
              ? 'Find your receipts and invoices in the emails from Link or in your Link account.'
              : 'The first one shows up here after the first payment. A trial is not charged until it ends.'
          }
        />
      ) : (
        <div className="grid gap-3">
          <InvoiceRows invoices={invoices} />
          {moreLeft && (
            <div>
              <Button
                variant="outline"
                disabled={more.isPending}
                onClick={() => more.mutate(invoices[invoices.length - 1]!.id)}
              >
                {more.isPending ? 'Loading...' : 'Show older invoices'}
              </Button>
            </div>
          )}
        </div>
      )}
    </Panel>
  )
}
