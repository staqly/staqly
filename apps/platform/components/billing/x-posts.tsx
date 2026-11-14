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
import { cn } from '@/lib/utils'
import { shortDay, xPostsWarning } from '@/lib/x-posts'

export { xPostsLine, xPostsWarning } from '@/lib/x-posts'

/**
 * Plan & usage, Posts per platform, under X's bar: how posts count, when the included ones reset and the warning when
 * this period's are used up. Bought posts show on the bar's line ("+ 200 bought").
 */
export function XPostsNote({ x }: { x: XPosts }) {
  return (
    <div className="grid gap-1.5" data-testid="x-posts">
      {x.left <= 0 && (
        <Callout tone="danger" size="xs">
          {xPostsWarning(x)}
        </Callout>
      )}
      <p className="text-xs text-muted-foreground">
        Each post and thread reply counts once when it goes out, a post with a live link counts {x.link_post_weight}.
        Included posts reset on {shortDay(x.period_end)}.
      </p>
    </div>
  )
}

/**
 * Buy more, next to X's bar (for the account owner): opens the bundles. `blocked`: why nothing can be bought now (a
 * locked workspace); the button is disabled and says it.
 */
export function BuyXPosts({ x, blocked }: { x: XPosts; blocked?: string }) {
  const [buying, setBuying] = useState(false)

  return (
    <>
      <Button
        size="sm"
        variant="info"
        onClick={blocked ? undefined : () => setBuying(true)}
        aria-disabled={!!blocked || undefined}
        tooltip={blocked}
        aria-label="Buy more X posts"
        data-testid="buy-x-posts"
      >
        Buy more
      </Button>
      {buying && <BuyXPostsDialog open={buying} onOpenChange={setBuying} x={x} />}
    </>
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
