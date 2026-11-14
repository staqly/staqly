'use client'

import { LogoMark } from '@staqly/brand/logo'

import { safeImageSrc } from '@/lib/safe'
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

/** The workspace's own logo, or staqly's when it has none. Decorative: the workspace's name is next to it. */
export function WorkspaceLogo({ src, className }: { src?: string; className?: string }) {
  const logo = safeImageSrc(src)

  return logo ? (
    <img src={logo} alt="" className={cn('shrink-0 rounded-md object-cover', className)} />
  ) : (
    <LogoMark className={className} />
  )
}
