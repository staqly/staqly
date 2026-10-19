'use client'

import { useMutation, useQueryClient } from '@tanstack/react-query'
import { Check, Plus } from 'lucide-react'
import { useState } from 'react'
import { toast } from 'sonner'

import { ConfirmDialog } from '@/components/common/confirm-dialog'
import { money } from '@/components/plans/plan-cards'
import { Button } from '@/components/ui/button'
import { api, type Addon, type Plan } from '@/lib/api'
import { formatBytes } from '@/lib/format'
import { toastApiError } from '@/lib/notify'
import { qk, useAddons, useMode } from '@/lib/queries'

const BYTES = new Set(['storage_bytes', 'max_upload_bytes'])

/** "15 + 10 from Channels pack" under a meter whose limit an add-on or a deal raised; "" when none did. */
export function bonusHint(plan: Plan | undefined, limit: string) {
  const bonuses = (plan?.limit_bonuses ?? []).filter((b) => b.limit === limit)
  const base = plan?.base_limits?.[limit as keyof NonNullable<Plan['base_limits']>]

  if (!bonuses.length || typeof base !== 'number') {
    return ''
  }

  const fmt = (n: number) => (BYTES.has(limit) ? formatBytes(n) : n.toLocaleString('en-US'))
  const lifted = bonuses.find((b) => b.unlimited)

  if (lifted) {
    return `Unlimited from ${lifted.from}`
  }

  return [fmt(base), ...bonuses.map((b) => `${fmt(b.add)} from ${b.from}`)].join(' + ')
}

/**
 * Add-ons on top of the plan (Plan & usage, for those who manage the workspace): each with its price in the subscription's interval, Add (charged now
 * for the rest of the period) or Remove (the rest credited). Hidden when none are on sale.
 */
export function AddonsSection({ canBuy, yearly }: { canBuy: boolean; yearly: boolean }) {
  const { isLocal } = useMode()
  const addons = useAddons({ enabled: !isLocal })
  const qc = useQueryClient()
  const [adding, setAdding] = useState<Addon | null>(null)
  const [removing, setRemoving] = useState<Addon | null>(null)
  const done = (msg: string) => {
    toast.success(msg, { description: 'Your limits update in a few seconds.' })
    void qc.invalidateQueries({ queryKey: qk.addons })
    // The features arrive with Stripe's next event: read the plan again shortly.
    setTimeout(() => void qc.invalidateQueries({ queryKey: qk.usage }), 4000)
  }
  const add = useMutation({
    mutationFn: (a: Addon) => api.addAddon(a.id),
    onSuccess: (_, a) => done(`${a.name} added`),
    onError: (e) => toastApiError(e),
    onSettled: () => setAdding(null),
  })
  const remove = useMutation({
    mutationFn: (a: Addon) => api.removeAddon(a.id),
    onSuccess: (_, a) => done(`${a.name} removed`),
    onError: (e) => toastApiError(e),
    onSettled: () => setRemoving(null),
  })

  if (isLocal || !addons.data?.length) {
    return null
  }

  const price = (a: Addon) => {
    const amount = yearly ? a.price_year : a.price_month

    return amount > 0
      ? `${money(amount, a.currency)} per ${yearly ? 'year' : 'month'}`
      : 'Not sold for this billing period'
  }

  return (
    <>
      <ul className="grid gap-2 sm:grid-cols-2" aria-label="Add-ons" data-testid="addons">
        {addons.data.map((a) => (
          <li key={a.id} className="flex items-start justify-between gap-3 rounded-xl border bg-background p-3.5">
            <div className="grid min-w-0 gap-0.5">
              <span className="flex items-center gap-1.5 text-sm font-medium">
                {a.name}
                {a.active && (
                  <span className="inline-flex items-center gap-0.5 text-xs font-normal text-brand">
                    <Check className="size-3.5" /> Active
                  </span>
                )}
              </span>
              <span className="text-xs text-muted-foreground">{a.description || a.features.join(' · ')}</span>
              <span className="text-xs text-muted-foreground tabular-nums">{price(a)} · excl. VAT</span>
            </div>
            {canBuy &&
              (a.active ? (
                <Button variant="ghost" onClick={() => setRemoving(a)} disabled={remove.isPending}>
                  Remove
                </Button>
              ) : (
                <Button
                  variant="outline"
                  onClick={() => setAdding(a)}
                  disabled={add.isPending || !(yearly ? a.price_year : a.price_month)}
                >
                  <Plus /> Add
                </Button>
              ))}
          </li>
        ))}
      </ul>
      <ConfirmDialog
        open={!!adding}
        onOpenChange={(o) => !o && setAdding(null)}
        title={`Add ${adding?.name ?? ''}?`}
        description={
          adding
            ? `${price(adding)}, excl. VAT. The rest of this billing period is charged now to the card on file.`
            : ''
        }
        confirmLabel="Add"
        onConfirm={() => adding && add.mutate(adding)}
      />
      <ConfirmDialog
        open={!!removing}
        onOpenChange={(o) => !o && setRemoving(null)}
        title={`Remove ${removing?.name ?? ''}?`}
        description="It stops now; the unused rest of this period is credited on your next invoice."
        confirmLabel="Remove"
        variant="destructive"
        onConfirm={() => removing && remove.mutate(removing)}
      />
    </>
  )
}
