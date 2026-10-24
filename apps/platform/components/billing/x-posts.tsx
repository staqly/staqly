'use client'

import { useMutation } from '@tanstack/react-query'
import { useState } from 'react'

import { Callout } from '@/components/common/callout'
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
import { money } from '@/lib/plans'
import { useMode, useXPosts } from '@/lib/queries'
import { cn } from '@/lib/utils'
import { shortDay, xPostsWarning } from '@/lib/x-posts'

export { xPostsLine, xPostsWarning } from '@/lib/x-posts'

/**
 * Plan & usage, Add-ons: the X posts bought on top of the plan (what is left and when it expires), the warning when
 * this period's are used up and Buy more X posts (for the account owner). How many of the included posts are used
 * shows in the usage meters, so it is not repeated here.
 */
export function XPostsSection({ canBuy }: { canBuy: boolean }) {
  const { isLocal } = useMode()
  const x = useXPosts({ enabled: !isLocal })
  const [buying, setBuying] = useState(false)

  if (isLocal || !x.data || x.data.unlimited) {
    return null
  }

  const out = x.data.left <= 0

  return (
    <div className="grid content-start gap-2 rounded-xl border bg-background p-3.5" data-testid="x-posts">
      <div className="flex flex-wrap items-start justify-between gap-3">
        <div className="grid min-w-0 gap-0.5">
          <p className="text-sm font-medium">Extra X posts</p>
          <p className="text-sm text-muted-foreground tabular-nums" data-testid="x-posts-line">
            {x.data.credits_left > 0
              ? `${x.data.credits_left.toLocaleString('en-US')} left · expire ${shortDay(x.data.credits_expire_at)}`
              : 'None bought this period'}
          </p>
        </div>
        {canBuy && (
          <Button variant={out ? 'default' : 'outline'} onClick={() => setBuying(true)} data-testid="buy-x-posts">
            Buy more X posts
          </Button>
        )}
      </div>
      {out && (
        <Callout tone="danger" size="xs">
          {xPostsWarning(x.data)}
        </Callout>
      )}
      <p className="text-xs text-muted-foreground">
        Used after the {x.data.included.toLocaleString('en-US')} X posts your plan includes each month (they reset on{' '}
        {shortDay(x.data.period_end)}). Each post and thread reply counts once when it goes out, a post with a live link
        counts {x.data.link_post_weight}. Bought posts expire at the end of the billing period.
      </p>
      {buying && <BuyXPostsDialog open={buying} onOpenChange={setBuying} x={x.data} />}
    </div>
  )
}

/** Pick a bundle of extra X posts, then pay for it with Stripe Checkout. */
function BuyXPostsDialog({ open, onOpenChange, x }: { open: boolean; onOpenChange: (o: boolean) => void; x: XPosts }) {
  const [posts, setPosts] = useState(x.bundles[1]?.posts ?? x.bundles[0]?.posts ?? 0)
  const buy = useMutation({
    mutationFn: () => api.creditsCheckout(posts),
    onSuccess: ({ url }) => window.location.assign(url),
    onError: (e) => toastApiError(e),
  })
  // Busy until the browser has left for Stripe, not only until the API answered.
  const leaving = buy.isPending || buy.isSuccess
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
          <Button disabled={!posts || leaving} onClick={() => buy.mutate()} data-testid="buy-x-posts-continue">
            {leaving ? 'Opening checkout...' : 'Continue to payment'}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  )
}
