import { AlertTriangle } from 'lucide-react'

import { Button } from '@/components/ui/button'
import { errorMessage } from '@/lib/api'
import { cn } from '@/lib/utils'

export function ErrorState({
  error,
  onRetry,
  className,
  title = 'Could not load data',
  description,
  action,
}: {
  error: unknown
  onRetry?: () => void
  className?: string
  title?: string
  /** Replaces the error's own message (e.g. a 404's "item not found", which only repeats the title). */
  description?: React.ReactNode
  /** A way on, e.g. a link back to the list. */
  action?: React.ReactNode
}) {
  return (
    <div
      role="alert"
      className={cn(
        'flex flex-col items-center gap-2 rounded-xl border border-dashed p-8 text-center text-sm',
        className,
      )}
    >
      <AlertTriangle className="size-5 text-destructive" />
      <div className="font-medium">{title}</div>
      <div className="max-w-md text-muted-foreground">{description ?? errorMessage(error)}</div>
      {onRetry && (
        <Button variant="outline" size="sm" onClick={onRetry} className="mt-1">
          Try again
        </Button>
      )}
      {action && <div className="mt-1">{action}</div>}
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
