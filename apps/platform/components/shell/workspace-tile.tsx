'use client'

import { cn } from '@/lib/utils'

/** The workspace's first letter on a tile, so the list reads as workspaces to pick from. */
export function WorkspaceTile({ name, className }: { name: string; className?: string }) {
  return (
    <span
      aria-hidden
      className={cn(
        'flex size-6 shrink-0 items-center justify-center rounded-md border bg-muted text-xs font-semibold text-foreground uppercase',
        className,
      )}
    >
      {[...name.trim()][0] ?? '?'}
    </span>
  )
}
