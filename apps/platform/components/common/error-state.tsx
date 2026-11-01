import { AlertTriangle, RotateCw } from 'lucide-react'

import { Button } from '@/components/ui/button'
import { ApiError, errorMessage } from '@/lib/api'
import { cn } from '@/lib/utils'

export function ErrorState({
  error,
  onRetry,
  className,
  title = 'Could not load data',
  description,
  action,
  bare,
}: {
  error: unknown
  onRetry?: () => void
  className?: string
  title?: string
  /** Replaces the error's own message (e.g. a 404's "item not found", which only repeats the title). */
  description?: React.ReactNode
  /** A way on, e.g. a link back to the list. */
  action?: React.ReactNode
  /** Inside a card: no frame of its own. */
  bare?: boolean
}) {
  // A server-side failure says so in plain words instead of "Internal server error".
  const serverSide = error instanceof ApiError && error.status >= 500
  const text =
    description ?? (serverSide ? 'Something went wrong on our side. Try again in a moment.' : errorMessage(error))

  return (
    <div
      role="alert"
      className={cn(
        'mx-auto flex w-full max-w-lg flex-col items-center gap-3 text-center text-sm',
        bare ? 'py-6' : 'rounded-2xl border bg-card px-6 py-10 shadow-xs',
        className,
      )}
    >
      <span className="flex size-11 items-center justify-center rounded-xl bg-destructive/10 text-destructive">
        <AlertTriangle className="size-5" />
      </span>
      <div className="grid gap-1">
        <div className="text-base font-semibold tracking-tight">{title}</div>
        <div className="max-w-md text-muted-foreground">{text}</div>
      </div>
      {(onRetry || action) && (
        <div className="mt-1 flex flex-wrap items-center justify-center gap-2">
          {onRetry && (
            <Button variant="outline" onClick={onRetry}>
              <RotateCw /> Try again
            </Button>
          )}
          {action}
        </div>
      )}
    </div>
  )
}

export function EmptyState({
  icon,
  title,
  description,
  action,
  className,
}: {
  icon?: React.ReactNode
  title: string
  description?: React.ReactNode
  action?: React.ReactNode
  className?: string
}) {
  return (
    <div className={cn('flex flex-col items-center justify-center gap-2 px-6 py-12 text-center', className)}>
      {icon && (
        <div className="mb-1 flex size-10 items-center justify-center rounded-xl border bg-card text-muted-foreground shadow-xs [&_svg]:size-5">
          {icon}
        </div>
      )}
      <div className="text-sm font-medium">{title}</div>
      {description && <div className="max-w-sm text-sm text-muted-foreground">{description}</div>}
      {action && <div className="mt-1">{action}</div>}
    </div>
  )
}

/**
 * "Load more" under a paged list. A page that fails to load says so above the button, which then tries it again;
 * what is already on screen stays (an ErrorState in place of the list would hide every loaded page).
 */
export function LoadMore({
  onLoad,
  loading,
  failed,
  label = 'Load more',
  size,
  className,
}: {
  onLoad: () => void
  loading: boolean
  failed: boolean
  label?: string
  size?: 'sm'
  className?: string
}) {
  return (
    <div className={cn('flex flex-col items-center gap-2', className)}>
      {failed && !loading && (
        <p role="alert" className="text-sm text-destructive">
          Could not load more. Check the connection and try again.
        </p>
      )}
      <Button variant="outline" size={size} onClick={onLoad} disabled={loading}>
        {loading ? 'Loading...' : failed ? 'Try again' : label}
      </Button>
    </div>
  )
}
