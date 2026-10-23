import { cva, type VariantProps } from 'class-variance-authority'
import { cn } from 'cn'
import { Slot } from 'radix-ui'
import * as React from 'react'

import { Tooltip, TooltipContent, TooltipProvider, TooltipTrigger } from '@/components/ui/tooltip'

const buttonVariants = cva(
  "group/button inline-flex shrink-0 items-center justify-center rounded-lg border border-transparent bg-clip-padding text-sm font-medium whitespace-nowrap transition-all outline-none select-none focus-visible:ring-2 focus-visible:ring-ring focus-visible:ring-offset-2 focus-visible:ring-offset-background active:not-aria-[haspopup]:translate-y-px disabled:pointer-events-none disabled:opacity-50 aria-invalid:border-destructive aria-invalid:ring-3 aria-invalid:ring-destructive/20 dark:aria-invalid:border-destructive/50 dark:aria-invalid:ring-destructive/40 [&_svg]:pointer-events-none [&_svg]:shrink-0 [&_svg:not([class*='size-'])]:size-4",
  {
    variants: {
      variant: {
        default: 'bg-primary text-primary-foreground hover:bg-primary/80',
        outline:
          'border-border bg-background hover:bg-muted hover:text-foreground aria-expanded:bg-muted aria-expanded:text-foreground dark:border-input dark:bg-input/30 dark:hover:bg-input/50',
        secondary:
          'bg-secondary text-secondary-foreground hover:bg-[color-mix(in_oklch,var(--secondary),var(--foreground)_5%)] aria-expanded:bg-secondary aria-expanded:text-secondary-foreground',
        ghost:
          'hover:bg-muted hover:text-foreground aria-expanded:bg-muted aria-expanded:text-foreground dark:text-foreground/85 dark:hover:bg-foreground/10 dark:hover:text-foreground',
        // Text a shade darker than the token in light mode: red on its 10% tint stays above 4.5:1.
        destructive:
          'bg-destructive/10 text-red-700 hover:bg-destructive/20 dark:text-destructive dark:bg-destructive/20 dark:hover:bg-destructive/30',
        link: 'text-primary underline-offset-4 underline hover:no-underline',
        // Pausing or holding something back: amber, styled like `destructive`.
        warning:
          'bg-amber-500/15 text-amber-800 hover:bg-amber-500/25 dark:bg-amber-500/20 dark:text-amber-400 dark:hover:bg-amber-500/30',
        // Decisions look like on staqly.ai: a neutral border, the colour in the text and icon.
        reject:
          'border-border bg-transparent text-red-700 hover:border-red-500/40 hover:bg-red-500/10 dark:text-red-400 dark:hover:bg-red-500/15',
        // Archiving: grey like the archived status, styled like `destructive`.
        archive:
          'bg-orange-500/15 text-orange-700 hover:bg-orange-500/25 dark:bg-orange-500/20 dark:text-orange-300 dark:hover:bg-orange-500/30',
        approve: 'border-border bg-transparent text-brand hover:border-brand/40 hover:bg-brand/10',
      },
      size: {
        default:
          'h-8 gap-1.5 px-2.5 pointer-coarse:h-11 pointer-coarse:px-3.5 has-data-[icon=inline-end]:pr-2 has-data-[icon=inline-start]:pl-2',
        // On touch: 36px to keep tight rows (code headers, ID chips) intact, plus 2px of invisible hit area = 40px.
        xs: "h-6 pointer-coarse:h-9 pointer-coarse:px-2.5 relative pointer-coarse:after:absolute pointer-coarse:after:-inset-0.5 gap-1 rounded-[min(var(--radius-md),10px)] px-2 text-xs in-data-[slot=button-group]:rounded-lg has-data-[icon=inline-end]:pr-1.5 has-data-[icon=inline-start]:pl-1.5 [&_svg:not([class*='size-'])]:size-3",
        // One control height: `sm` matches `default` (32 px, text-sm), so a "small" button next to an input or a
        // select never sits lower or reads smaller. Only xs/icon-xs/icon-sm stay smaller, for tight non-form rows.
        sm: 'h-8 gap-1.5 px-2.5 pointer-coarse:h-11 pointer-coarse:px-3.5 has-data-[icon=inline-end]:pr-2 has-data-[icon=inline-start]:pl-2',
        lg: 'h-9 pointer-coarse:h-11 gap-1.5 px-2.5 has-data-[icon=inline-end]:pr-2 has-data-[icon=inline-start]:pl-2',
        icon: 'size-8 pointer-coarse:size-11',
        'icon-xs':
          "size-6 pointer-coarse:size-9 relative pointer-coarse:after:absolute pointer-coarse:after:-inset-0.5 rounded-[min(var(--radius-md),10px)] in-data-[slot=button-group]:rounded-lg [&_svg:not([class*='size-'])]:size-3",
        'icon-sm':
          'size-7 pointer-coarse:size-10 rounded-[min(var(--radius-md),12px)] in-data-[slot=button-group]:rounded-lg',
        'icon-lg': 'size-9 pointer-coarse:size-11',
      },
    },
    defaultVariants: {
      variant: 'default',
      size: 'default',
    },
  },
)

/**
 * `tooltip`: a label shown on hover and keyboard focus, only where an action needs explaining (destructive or
 * not obvious: remove a comment, move it). Ordinary icon buttons rely on their aria-label alone, no tooltip.
 */
function Button({
  className,
  variant = 'default',
  size = 'default',
  asChild = false,
  tooltip,
  ...props
}: React.ComponentProps<'button'> &
  VariantProps<typeof buttonVariants> & {
    asChild?: boolean
    tooltip?: React.ReactNode | false
  }) {
  const Comp = asChild ? Slot.Root : 'button'
  const label = tooltip || undefined
  const button = (
    <Comp
      data-slot="button"
      data-variant={variant}
      data-size={size}
      className={cn(buttonVariants({ variant, size, className }))}
      {...props}
    />
  )

  if (!label) {
    return button
  }

  // Its own provider: buttons also render outside the app's providers (tests, the editor's portals).
  return (
    <TooltipProvider delayDuration={300}>
      <Tooltip>
        <TooltipTrigger asChild>{button}</TooltipTrigger>
        <TooltipContent>{label}</TooltipContent>
      </Tooltip>
    </TooltipProvider>
  )
}

export { Button, buttonVariants }
