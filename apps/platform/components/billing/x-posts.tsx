'use client'

import { useMutation } from '@tanstack/react-query'
import { useEffect, useState } from 'react'
import { toast } from 'sonner'

import { Callout } from '@/components/common/callout'
import { money } from '@/components/plans/plan-cards'
import { Button } from '@/components/ui/button'
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from '@/components/ui/dialog'
import { api, type XPosts } from '@/lib/api'
import { toastApiError } from '@/lib/notify'
import { useMode, useXPosts } from '@/lib/queries'
import { cn } from '@/lib/utils'

/** Nov 30: a short day for "expire Nov 30" and "wait until Nov 1". */
export function shortDay(iso?: string | null) {
  return iso ? new Date(iso).toLocaleDateString('en-US', { month: 'short', day: 'numeric', timeZone: 'UTC' }) : ''
}

/** "37 of 250 included · 200 extra left (expire Nov 30)". */
export function xPostsLine(x: XPosts) {
  if (x.unlimited) {
    return `${x.used.toLocaleString('en-US')} this month, no limit`
  }

  const used = Math.min(x.included - x.included_left, x.included)
  let line = `${used.toLocaleString('en-US')} of ${x.included.toLocaleString('en-US')} included`

  if (x.credits_left > 0) {
    line += ` · ${x.credits_left.toLocaleString('en-US')} extra left (expire ${shortDay(x.credits_expire_at)})`
  }

  return line
}

/** How full the allowance is: 0 to 1 (included and bought posts together). */
export function xPostsShare(x: XPosts) {
  if (x.unlimited) {
    return 0
  }

  const total = x.used + x.left

  return total > 0 ? Math.min(x.used / total, 1) : 1
}

/** The warning at 80 % and 100 %, or nothing. */
export function xPostsWarning(x: XPosts) {
  if (x.unlimited) {
    return ''
  }

  if (x.left <= 0) {
    return `Your X posts for this period are used up. Buy more or wait until ${shortDay(x.period_end)}.`
  }

  if (xPostsShare(x) >= 0.8) {
    return `Only ${x.left.toLocaleString('en-US')} X posts left this period.`
  }

  return ''
}

/** Billing page: the X posts this period, the warning and Buy more X posts (for the account owner). */
export function XPostsSection({ canBuy }: { canBuy: boolean }) {
  const { isLocal } = useMode()
  const x = useXPosts({ enabled: !isLocal })
  const [buying, setBuying] = useState(false)

  if (isLocal || !x.data) {
    return null
  }

  const warning = xPostsWarning(x.data)
  const share = xPostsShare(x.data)

  return (
    <div className="grid gap-3" data-testid="x-posts">
      <div className="flex flex-wrap items-center justify-between gap-3">
        <div className="grid gap-1">
          <p className="text-sm font-medium">X posts this period</p>
          <p className="text-sm text-muted-foreground tabular-nums" data-testid="x-posts-line">
            {xPostsLine(x.data)}
          </p>
        </div>
        {canBuy && !x.data.unlimited && (
          <Button variant="outline" onClick={() => setBuying(true)} data-testid="buy-x-posts">
            Buy more X posts
          </Button>
        )}
      </div>
      {!x.data.unlimited && (
        <div
          className="h-1.5 overflow-hidden rounded-full bg-muted"
          role="progressbar"
          aria-label="X posts used"
          aria-valuemin={0}
          aria-valuemax={100}
          aria-valuenow={Math.round(share * 100)}
        >
          <div
            className={cn(
              'h-full rounded-full',
              share >= 1 ? 'bg-destructive' : share >= 0.8 ? 'bg-amber-500' : 'bg-brand',
            )}
            style={{ width: `${Math.round(share * 100)}%` }}
          />
        </div>
      )}
      {warning && (
        <Callout tone={x.data.left <= 0 ? 'danger' : 'warning'} size="xs">
          {warning}
        </Callout>
      )}
      <p className="text-xs text-muted-foreground">
        Each post and thread reply to X counts once when it goes out; a post with a live link counts{' '}
        {x.data.link_post_weight}. Included posts reset on {shortDay(x.data.period_end)}; bought ones are used after
        them and expire at the end of the billing period.
      </p>
      {buying && <BuyXPostsDialog open={buying} onOpenChange={setBuying} x={x.data} />}
    </div>
  )
}

/** Pick a bundle of extra X posts, then pay for it with Stripe Checkout. */
export function BuyXPostsDialog({
  open,
  onOpenChange,
  x,
}: {
  open: boolean
  onOpenChange: (o: boolean) => void
  x: XPosts
}) {
  const [posts, setPosts] = useState(x.bundles[1]?.posts ?? x.bundles[0]?.posts ?? 0)
  const buy = useMutation({
    mutationFn: () => api.creditsCheckout(posts),
    onSuccess: ({ url }) => window.location.assign(url),
    onError: (e) => toastApiError(e),
  })
  const cheapest = Math.max(...x.bundles.map((b) => b.price / b.posts))

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent className="sm:max-w-md">
        <DialogHeader>
          <DialogTitle>Buy more X posts</DialogTitle>
          <DialogDescription>
            Used after the posts your plan includes. Bought posts expire at the end of the billing period.
          </DialogDescription>
        </DialogHeader>
        <div role="radiogroup" aria-label="Bundle" className="grid gap-2">
          {x.bundles.map((b) => {
            const perPost = b.price / b.posts
            const saving = Math.round((1 - perPost / cheapest) * 100)

            return (
              <button
                key={b.posts}
                type="button"
                role="radio"
                aria-checked={posts === b.posts}
                onClick={() => setPosts(b.posts)}
                data-testid={`bundle-${b.posts}`}
                className={cn(
                  'flex items-center justify-between gap-3 rounded-xl border p-3 text-left outline-none focus-visible:ring-2 focus-visible:ring-ring',
                  posts === b.posts ? 'border-brand bg-brand-soft/40' : 'hover:bg-muted/40',
                )}
              >
                <span className="grid">
                  <span className="text-sm font-medium">{b.posts.toLocaleString('en-US')} X posts</span>
                  <span className="text-xs text-muted-foreground">
                    {money(perPost, x.currency)} a post
                    {saving > 0 && ` · save ${saving}%`}
                  </span>
                </span>
                <span className="text-base font-semibold tabular-nums">{money(b.price, x.currency)}</span>
              </button>
            )
          })}
        </div>
        <DialogFooter>
          <Button variant="ghost" onClick={() => onOpenChange(false)}>
            Cancel
          </Button>
          <Button disabled={!posts || buy.isPending} onClick={() => buy.mutate()} data-testid="buy-x-posts-continue">
            {buy.isPending ? 'Opening checkout...' : 'Continue to payment'}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  )
}

/** App-wide: a toast once a period when the X posts reach 80 % and again when they run out. */
export function useXPostsWarnings() {
  const { isLocal } = useMode()
  const x = useXPosts({ enabled: !isLocal })

  useEffect(() => {
    if (!x.data || x.data.unlimited) {
      return
    }

    const level = x.data.left <= 0 ? '100' : xPostsShare(x.data) >= 0.8 ? '80' : ''

    if (!level) {
      return
    }

    const key = `staqly.x-posts-warned.${x.data.period}.${level}`

    try {
      if (sessionStorage.getItem(key)) {
        return
      }

      sessionStorage.setItem(key, '1')
    } catch {
      // No storage (private window): warn anyway, once per page load.
    }

    const message = xPostsWarning(x.data)

    if (level === '100') {
      toast.error(message, {
        action: { label: 'Buy more', onClick: () => window.location.assign('/settings/billing') },
      })
    } else {
      toast.warning(message)
    }
  }, [x.data])
}
