import { cn } from 'cn'

/** A loading placeholder: shown only when loading takes a while (globals.css, [data-slot='skeleton']). */
function Skeleton({ className, ...props }: React.ComponentProps<'div'>) {
  return <div data-slot="skeleton" className={cn('rounded-md bg-muted', className)} {...props} />
}

export { Skeleton }
