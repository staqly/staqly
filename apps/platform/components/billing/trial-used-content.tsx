'use client'

import { useMutation, useQueryClient } from '@tanstack/react-query'
import { CreditCard, Mail } from 'lucide-react'
import Link from 'next/link'
import { toast } from 'sonner'

import { Button } from '@/components/ui/button'
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from '@/components/ui/dialog'
import { api } from '@/lib/api'
import { toastApiError } from '@/lib/notify'
import { qk } from '@/lib/queries'
import type { TrialUsedKind } from '@/lib/trial-used'

export const SUPPORT_EMAIL = 'support@staqly.ai'

const CONTACT_HREF = `mailto:${SUPPORT_EMAIL}?subject=${encodeURIComponent('My free trial was paused')}`

/** The dialog TrialUsedDialog opens (loaded only when it opens, so the shell stays light). */
export default function TrialUsedContent({
  kind,
  canPay,
  charged,
  onClose,
}: {
  kind: TrialUsedKind
  /** A workspace owner: may start the paid plan. */
  canPay: boolean
  /** The card was charged (the subscription is active). */
  charged: boolean
  onClose: () => void
}) {
  const qc = useQueryClient()
  const start = useMutation({
    mutationFn: () => api.startPaidPlan(),
    onSuccess: (b) => {
      qc.setQueryData(qk.billing, b)
      void qc.invalidateQueries({ queryKey: qk.usage })
      toast.success('Your paid plan has started', { description: 'Publishing and new channels work again.' })
      onClose()
    },
    onError: (e) => toastApiError(e),
  })

  if (kind === 'card') {
    return (
      <Dialog open onOpenChange={(o) => !o && onClose()}>
        <DialogContent className="sm:max-w-md" data-testid="trial-used" data-kind="card">
          <DialogHeader>
            <DialogTitle>No free trial for this card</DialogTitle>
            <DialogDescription>
              This card was already used for a free trial that was never paid for, so this workspace has no free trial.{' '}
              {charged
                ? 'Your plan was charged today and everything keeps working.'
                : "The plan is due now and the charge didn't go through: pay in Plan & usage to keep working."}
            </DialogDescription>
          </DialogHeader>
          <DialogFooter>
            <Button variant={charged ? 'default' : 'outline'} onClick={onClose}>
              {charged ? 'Got it' : 'Not now'}
            </Button>
            {!charged && (
              <Button asChild>
                <Link href="/settings/plan" onClick={onClose}>
                  <CreditCard /> Go to Plan & usage
                </Link>
              </Button>
            )}
          </DialogFooter>
        </DialogContent>
      </Dialog>
    )
  }

  return (
    <Dialog open onOpenChange={(o) => !o && onClose()}>
      <DialogContent className="sm:max-w-md" data-testid="trial-used" data-kind="paused">
        <DialogHeader>
          <DialogTitle>Your free trial is paused</DialogTitle>
          <DialogDescription>
            These accounts were already used in an earlier free trial that was never paid for. Until the paid plan
            starts, posts aren&apos;t published and new channels can&apos;t be connected; your drafts and settings stay.
            Nothing has been charged.
            {!canPay && ' An owner of this workspace can start the paid plan.'}
          </DialogDescription>
        </DialogHeader>
        <DialogFooter>
          <Button variant="outline" asChild>
            <a href={CONTACT_HREF}>
              <Mail /> This isn&apos;t right? Contact us
            </a>
          </Button>
          {canPay && (
            <Button onClick={() => start.mutate()} disabled={start.isPending} aria-busy={start.isPending}>
              <CreditCard /> {start.isPending ? 'Starting...' : 'Start paid plan now'}
            </Button>
          )}
        </DialogFooter>
      </DialogContent>
    </Dialog>
  )
}
