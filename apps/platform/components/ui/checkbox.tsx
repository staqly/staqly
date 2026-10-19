'use client'

import { cn } from 'cn'
import { CheckIcon, MinusIcon } from 'lucide-react'
import { Checkbox as CheckboxPrimitive } from 'radix-ui'
import * as React from 'react'

const CHECKBOX_CLASS =
  'peer relative flex size-4 shrink-0 items-center justify-center rounded-[4px] border border-input transition-colors outline-none group-has-disabled/field:opacity-50 group-has-[:focus-visible]/field-label:ring-0 group-has-[:focus-visible]/field-label:not-data-checked:border-input after:absolute after:-inset-x-3 after:-inset-y-2 pointer-coarse:after:-inset-[15px] focus-visible:ring-2 focus-visible:ring-ring focus-visible:ring-offset-2 focus-visible:ring-offset-background disabled:cursor-not-allowed disabled:opacity-50 aria-invalid:border-destructive aria-invalid:ring-3 aria-invalid:ring-destructive/20 aria-invalid:aria-checked:border-primary dark:bg-input/30 dark:aria-invalid:border-destructive/50 dark:aria-invalid:ring-destructive/40 data-checked:border-primary data-checked:bg-primary data-checked:text-primary-foreground data-[state=indeterminate]:border-primary data-[state=indeterminate]:bg-primary data-[state=indeterminate]:text-primary-foreground group-has-[:focus-visible]/field-label:data-checked:border-primary dark:data-checked:bg-primary'

function Checkbox({ className, ...props }: React.ComponentProps<typeof CheckboxPrimitive.Root>) {
  return (
    <CheckboxPrimitive.Root data-slot="checkbox" className={cn(CHECKBOX_CLASS, className)} {...props}>
      <CheckboxPrimitive.Indicator
        data-slot="checkbox-indicator"
        className="grid place-content-center text-current transition-none [&>svg]:size-3.5"
      >
        {props.checked === 'indeterminate' ? <MinusIcon /> : <CheckIcon />}
      </CheckboxPrimitive.Indicator>
    </CheckboxPrimitive.Root>
  )
}

/**
 * The same box for long lists (inbox rows): a plain button with checkbox semantics instead of Radix's
 * Root/Provider/Trigger/Presence/Indicator (about a dozen components each). Selecting or clearing 500 rows
 * at once rendered ~7500 components and blocked the main thread for ~120 ms; now each row is two. Toggles
 * on click and Space (a button does), not on Enter (as Radix and the WAI-ARIA checkbox pattern).
 */
function RowCheckbox({
  checked,
  className,
  onKeyDown,
  ...props
}: Omit<React.ComponentProps<'button'>, 'role' | 'type'> & { checked: boolean }) {
  return (
    <button
      type="button"
      role="checkbox"
      aria-checked={checked}
      data-slot="checkbox"
      data-state={checked ? 'checked' : 'unchecked'}
      className={cn(CHECKBOX_CLASS, className)}
      onKeyDown={(e) => {
        onKeyDown?.(e)

        if (e.key === 'Enter') {
          e.preventDefault()
        }
      }}
      {...props}
    >
      {checked && (
        <span
          data-slot="checkbox-indicator"
          className="grid place-content-center text-current transition-none [&>svg]:size-3.5"
        >
          <CheckIcon />
        </span>
      )}
    </button>
  )
}

export { Checkbox, RowCheckbox }
