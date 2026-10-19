'use client'

import { useMutation, useQueryClient } from '@tanstack/react-query'
import { CreditCard, ExternalLink, FileText, Receipt, Sparkles } from 'lucide-react'
import Link from 'next/link'
import { useEffect, useState } from 'react'
import { toast } from 'sonner'

import { AddonsSection, bonusHint } from '@/components/billing/addons'
import { XPostsSection } from '@/components/billing/x-posts'
import { Callout } from '@/components/common/callout'
import { EmptyState, ErrorState } from '@/components/common/error-state'
import { PageContainer } from '@/components/common/page-header'
import { PlanBadge } from '@/components/plans/plan-badge'
import { usePlanActions } from '@/components/plans/plans-page'
import { SettingsHeader } from '@/components/settings/settings-header'
import { Button } from '@/components/ui/button'
import { Skeleton } from '@/components/ui/skeleton'
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from '@/components/ui/table'
import { api, UNLIMITED, type BillingDetails } from '@/lib/api'
import { toastApiError } from '@/lib/notify'
import { LIVE_SUBSCRIPTION } from '@/lib/plans'
import { qk, useBilling, useBillingDetails, useRole, useUsage } from '@/lib/queries'
import { TONES, type Tone } from '@/lib/tones'
import { cn } from '@/lib/utils'

import { METERS, periodLabel, UsageMeter } from './plan-usage-page'
import { SettingsNav } from './settings-nav'

type Invoice = BillingDetails['invoices'][number]
type Subscription = NonNullable<BillingDetails['subscription']>

function day(value?: string | null) {
  return value ? new Date(value).toLocaleDateString('en-US', { day: 'numeric', month: 'short', year: 'numeric' }) : ''
}

// ponytail: amounts are in cents; zero-decimal currencies (JPY, KRW) would show 100x too small. Staqly's plans are USD.
function money(cents: number, currency: string) {
  return new Intl.NumberFormat('en-US', { style: 'currency', currency: (currency || 'usd').toUpperCase() }).format(
    cents / 100,
  )
}

const SUBSCRIPTION_STATUS: Record<string, [string, Tone]> = {
  active: ['Active', 'success'],
  trialing: ['Trial', 'info'],
  past_due: ['Payment failed', 'danger'],
  unpaid: ['Unpaid', 'danger'],
  incomplete: ['Waiting for payment', 'warning'],
  incomplete_expired: ['Payment expired', 'neutral'],
  canceled: ['Canceled', 'neutral'],
  paused: ['Paused', 'neutral'],
}

const INVOICE_STATUS: Record<string, [string, Tone]> = {
  paid: ['Paid', 'success'],
  open: ['Due', 'warning'],
  void: ['Void', 'neutral'],
  uncollectible: ['Not paid', 'danger'],
}

function Pill({ label, tone }: { label: string; tone: Tone }) {
  return (
    <span
      className={cn(
        'inline-flex h-5 items-center rounded-full px-2 text-xs font-medium ring-1 ring-inset',
        TONES[tone].badge,
      )}
    >
      {label}
    </span>
  )
}

/** What happens next: the renewal, the end of a canceled subscription or of the trial. */
function renewalLine(sub: Subscription) {
  if (sub.status === 'canceled') {
    return sub.canceled_at ? `Ended on ${day(sub.canceled_at)}` : 'Ended'
  }

  if (sub.cancel_at_period_end || sub.cancel_at) {
    return `Ends on ${day(sub.cancel_at ?? sub.current_period_end)}; it will not renew`
  }

  if (sub.status === 'trialing' && sub.trial_end) {
    // The card on file is charged when the trial ends, per user (cancel in Manage billing before then).
    const then =
      sub.amount > 0 ? ` · then ${money(sub.amount, sub.currency)} per user / ${sub.interval || 'month'}` : ''

    return `Trial ends ${day(sub.trial_end)}${then}`
  }

  return sub.current_period_end ? `Renews on ${day(sub.current_period_end)}` : ''
}

function Section({
  title,
  description,
  action,
  children,
}: {
  title: string
  description?: string
  action?: React.ReactNode
  children: React.ReactNode
}) {
  return (
    <section className="grid gap-3 rounded-2xl border bg-card p-4 sm:p-5">
      <div className="flex flex-wrap items-start justify-between gap-3">
        <div>
          <h2 className="text-base font-semibold tracking-tight">{title}</h2>
          {description && <p className="mt-0.5 text-sm text-muted-foreground">{description}</p>}
        </div>
        {action}
      </div>
      {children}
    </section>
  )
}

function InvoiceTable({ invoices }: { invoices: Invoice[] }) {
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

            return (
              <TableRow key={inv.id} data-testid="invoice-row">
                <TableCell className="whitespace-nowrap">{day(inv.created)}</TableCell>
                <TableCell className="font-mono text-xs">{inv.number || '-'}</TableCell>
                <TableCell className="text-right tabular-nums">{money(inv.total, inv.currency)}</TableCell>
                <TableCell>
                  <Pill label={label} tone={tone} />
                </TableCell>
                <TableCell className="text-right whitespace-nowrap">
                  {inv.hosted_invoice_url && (
                    <Button asChild variant="ghost" size="sm">
                      <a href={inv.hosted_invoice_url} target="_blank" rel="noopener noreferrer">
                        {inv.status === 'open' ? 'Pay' : 'View'} <ExternalLink />
                      </a>
                    </Button>
                  )}
                  {inv.invoice_pdf && (
                    <Button asChild variant="ghost" size="sm">
                      <a
                        href={inv.invoice_pdf}
                        target="_blank"
                        rel="noopener noreferrer"
                        aria-label={`PDF of ${inv.number}`}
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
 * Billing (cloud): the plan and its subscription, this period's usage, the cards on file and the invoices. Stripe
 * holds the cards and invoices; the page reads them on open and sends changes to Stripe's billing portal.
 */
export function BillingPage() {
  const { canManagePeople, accountOwner } = useRole()
  const billing = useBilling({ enabled: canManagePeople })
  const details = useBillingDetails({ enabled: canManagePeople })
  const usage = useUsage()
  const { portal: openPortal } = usePlanActions()
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

  const b = billing.data
  const d = details.data
  const plan = usage.data?.plan
  const sub = d?.subscription ?? null
  const invoices = [...(d?.invoices ?? []), ...older]
  const moreLeft = hasMore ?? d?.has_more_invoices ?? false
  const portal = accountOwner && !!b?.has_customer && !!b?.enabled
  const [statusLabel, statusTone] = sub ? (SUBSCRIPTION_STATUS[sub.status] ?? [sub.status, 'neutral' as Tone]) : []
  const subscribed = LIVE_SUBSCRIPTION.includes(b?.subscription_status ?? '')
  const qc = useQueryClient()

  // Back from Stripe: the plan changes when Stripe's event arrives, so look again in a moment.
  useEffect(() => {
    const params = new URLSearchParams(window.location.search)
    const result = params.get('checkout')

    // Back from buying X posts: they are added when Stripe's event arrives.
    if (params.get('credits') === 'success') {
      toast.success('Payment received', { description: 'Your extra X posts show here in a moment.' })
      setTimeout(() => void qc.invalidateQueries({ queryKey: qk.xPosts }), 3000)
      window.history.replaceState(null, '', window.location.pathname)
    }

    if (!result) {
      return
    }

    if (result === 'success' || result === 'switched') {
      toast.success(result === 'success' ? 'Payment received' : 'Plan changed', {
        description: 'The new plan shows here in a moment.',
      })
      setTimeout(() => {
        void qc.invalidateQueries({ queryKey: qk.usage })
        void qc.invalidateQueries({ queryKey: qk.billing })
        void qc.invalidateQueries({ queryKey: qk.billingDetails })
      }, 3000)
    }

    window.history.replaceState(null, '', window.location.pathname)
  }, [qc])

  if (!canManagePeople) {
    return (
      <PageContainer>
        <SettingsHeader />
        <SettingsNav className="mt-4" />
        <Callout tone="neutral" className="mt-6">
          Only owners and admins of this workspace see its billing.
        </Callout>
      </PageContainer>
    )
  }

  return (
    <PageContainer>
      <SettingsHeader />
      <SettingsNav className="mt-4" />

      <div className="mt-6 grid gap-4" data-testid="billing">
        {b && !b.enabled && (
          <Callout tone="neutral" role="note" data-testid="billing-off">
            Online payments are not enabled on this instance, so there are no cards or invoices here. Plan changes are
            applied by the server operator.
          </Callout>
        )}

        <Section
          title="Plan"
          action={
            <div className="flex flex-wrap gap-2">
              {portal && (
                <Button variant="outline" disabled={openPortal.isPending} onClick={() => openPortal.mutate()}>
                  Manage subscription <ExternalLink />
                </Button>
              )}
              <Button asChild variant={subscribed ? 'outline' : 'default'}>
                <Link href="/plans">
                  {subscribed ? (
                    'Change plan'
                  ) : (
                    <>
                      <Sparkles /> Choose a plan
                    </>
                  )}
                </Link>
              </Button>
            </div>
          }
        >
          {usage.isLoading || details.isLoading ? (
            <Skeleton className="h-12" />
          ) : (
            <div className="flex flex-wrap items-center gap-x-3 gap-y-1.5" data-testid="billing-plan">
              <span className="text-xl font-semibold tracking-tight">{plan?.name ?? 'Plan'}</span>
              <span aria-hidden>
                <PlanBadge plan={plan?.id} />
              </span>
              {statusLabel && <Pill label={statusLabel} tone={statusTone!} />}
              {sub && sub.amount > 0 && (
                // Per user: the seat price, then what the team pays (3 users × $29 = $87 / month).
                <span className="text-sm text-muted-foreground tabular-nums" data-testid="billing-price">
                  {money(sub.amount, sub.currency)} per user / {sub.interval || 'month'}
                  {(sub.quantity ?? 1) > 1 &&
                    ` · ${sub.quantity} users = ${money(sub.amount * (sub.quantity ?? 1), sub.currency)} / ${sub.interval || 'month'}`}
                  {sub.interval === 'year' && ' (billed yearly)'}
                </span>
              )}
              <p className="basis-full text-sm text-muted-foreground" data-testid="billing-renewal">
                {sub
                  ? renewalLine(sub)
                  : b?.plan_expires_at
                    ? b.suspended
                      ? `Ended on ${day(b.plan_expires_at)}`
                      : `Trial ends on ${day(b.plan_expires_at)}`
                    : ''}
              </p>
            </div>
          )}
        </Section>

        <Section
          title="This billing period"
          description={
            sub?.current_period_start && sub.current_period_end
              ? `${day(sub.current_period_start)} to ${day(sub.current_period_end)}. Usage counts per calendar month: ${periodLabel(usage.data?.period)}.`
              : `Usage for ${periodLabel(usage.data?.period)}; posts reset on the 1st.`
          }
        >
          {usage.error ? (
            <ErrorState title="Could not load usage" error={usage.error} onRetry={() => usage.refetch()} />
          ) : (
            <div className="grid grid-cols-2 gap-3 lg:grid-cols-3">
              {METERS.map((m) =>
                usage.data ? (
                  <UsageMeter
                    key={m.key}
                    label={m.label}
                    Icon={m.Icon}
                    bytes={m.bytes}
                    used={usage.data.usage?.[m.key] ?? 0}
                    max={usage.data.plan?.limits?.[m.key] ?? UNLIMITED}
                    hint={bonusHint(usage.data.plan, m.key) || undefined}
                  />
                ) : (
                  <Skeleton key={m.key} className="h-28 rounded-xl" />
                ),
              )}
            </div>
          )}
          <XPostsSection canBuy={accountOwner && !!b?.enabled} />
          <AddonsSection canBuy={accountOwner && !!b?.enabled} yearly={sub?.interval === 'year'} />
        </Section>

        {details.error ? (
          <ErrorState title="Could not load billing details" error={details.error} onRetry={() => details.refetch()} />
        ) : (
          d?.enabled && (
            <>
              <Section
                title="Payment method"
                description="Cards are kept by Stripe; Staqly sees only the brand and the last four digits."
                action={
                  portal && (
                    <Button variant="outline" disabled={openPortal.isPending} onClick={() => openPortal.mutate()}>
                      <CreditCard /> Update payment method
                    </Button>
                  )
                }
              >
                {d.payment_methods.length === 0 ? (
                  <p className="text-sm text-muted-foreground">No card on file yet. It is added when you upgrade.</p>
                ) : (
                  <ul className="grid gap-2" data-testid="payment-methods">
                    {d.payment_methods.map((pm) => (
                      <li
                        key={`${pm.brand}-${pm.last4}-${pm.exp_year}`}
                        className="flex flex-wrap items-center gap-3 rounded-xl border bg-background px-3 py-2.5 text-sm"
                      >
                        <CreditCard className="size-4 text-muted-foreground" />
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
              </Section>

              <Section title="Invoices" description="Every invoice Stripe issued for this workspace, newest first.">
                {invoices.length === 0 ? (
                  <EmptyState
                    icon={<Receipt />}
                    title="No invoices yet"
                    description="Your first invoice shows up here after you upgrade."
                  />
                ) : (
                  <>
                    <InvoiceTable invoices={invoices} />
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
                  </>
                )}
              </Section>
            </>
          )
        )}
      </div>
    </PageContainer>
  )
}
