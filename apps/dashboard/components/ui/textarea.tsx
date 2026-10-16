import { cn } from 'cn'
import * as React from 'react'

/** Grows with its text up to a cap, then scrolls. With maxLength, a counter sits in the bottom-right corner. */
function Textarea({ className, ...props }: React.ComponentProps<'textarea'>) {
  const area = (
    <textarea
      data-slot="textarea"
      className={cn(
        'flex field-sizing-content max-h-64 min-h-16 w-full overflow-y-auto rounded-lg border border-input bg-transparent px-2.5 py-2 text-base transition-colors outline-none placeholder:text-muted-foreground focus-visible:border-ring focus-visible:ring-3 focus-visible:ring-ring/50 disabled:cursor-not-allowed disabled:bg-input/50 disabled:opacity-50 aria-invalid:border-destructive aria-invalid:ring-3 aria-invalid:ring-destructive/20 md:text-sm dark:bg-input/30 dark:disabled:bg-input/80 dark:aria-invalid:border-destructive/50 dark:aria-invalid:ring-destructive/40',
        props.maxLength && 'pb-6',
        className,
      )}
      {...props}
    />
  )

  if (!props.maxLength) {
    return area
  }

  return (
    <div className="relative w-full">
      {area}
      <span className="pointer-events-none absolute right-3 bottom-1.5 text-xs text-muted-foreground tabular-nums">
        {String(props.value ?? '').length}/{props.maxLength}
      </span>
    </div>
  )
}

export { Textarea }
