'use client'

import { ChevronRight } from 'lucide-react'
import { useId, useState } from 'react'

import { CopyButton } from '@/components/common/copy-button'
import { cn } from '@/lib/utils'

/**
 * A card whose header expands it, animated, to the full content: event data, feedback. One style for both.
 * Copy appears in the header once it is open. The body renders after the first open, so long lists stay
 * cheap.
 */
export function DisclosureCard({
  label,
  header,
  preview,
  meta,
  copy,
  copyLabel,
  defaultOpen = false,
  bodyTestId,
  testId,
  className,
  children,
}: {
  /** Accessible name of the toggle, e.g. "Data" or "Feedback". */
  label: string
  /** What the header shows next to the chevron. */
  header: React.ReactNode
  /** Shown in the header only while collapsed, e.g. the start of the text. */
  preview?: React.ReactNode
  /** Right side of the header, e.g. a time. */
  meta?: React.ReactNode
  /** Text the Copy button copies. */
  copy?: string
  copyLabel?: string
  defaultOpen?: boolean
  bodyTestId?: string
  testId?: string
  className?: string
  children: React.ReactNode
}) {
  const [open, setOpen] = useState(defaultOpen)
  const [mounted, setMounted] = useState(defaultOpen)
  const id = useId()

  return (
    <div className={cn('overflow-hidden rounded-lg border bg-muted/30', className)} data-testid={testId}>
      <div className="flex items-center gap-2 pr-2.5 text-xs text-muted-foreground">
        <button
          type="button"
          className="flex min-w-0 flex-1 items-center gap-1.5 py-1.5 pl-2 text-left hover:text-foreground focus-visible:outline-2 focus-visible:-outline-offset-2 focus-visible:outline-ring pointer-coarse:min-h-11"
          aria-expanded={open}
          aria-controls={id}
          aria-label={label}
          onClick={() => {
            setMounted(true)
            setOpen((v) => !v)
          }}
        >
          <ChevronRight className={cn('size-3.5 shrink-0 transition-transform duration-200', open && 'rotate-90')} />
          {header}
          {!open && preview}
          {meta && <span className="ml-auto shrink-0 pl-2">{meta}</span>}
        </button>
        {open && copy !== undefined && (
          <CopyButton value={copy} label={copyLabel ?? `Copy ${label.toLowerCase()}`} size="xs" showLabel />
        )}
      </div>
      {/* grid-rows 0fr -> 1fr animates to the content's own height. */}
      <div
        id={id}
        className={cn(
          'grid transition-[grid-template-rows] duration-200 ease-out',
          open ? 'grid-rows-[1fr]' : 'grid-rows-[0fr]',
        )}
      >
        <div className="min-h-0 overflow-hidden">
          {mounted && (
            <div className="border-t" data-testid={open ? bodyTestId : undefined} inert={!open}>
              {children}
            </div>
          )}
        </div>
      </div>
    </div>
  )
}
