'use client'

import { cn } from 'cn'
import { Label as LabelPrimitive } from 'radix-ui'
import * as React from 'react'

/** Red asterisk after a required field's label; screen readers get `aria-required` on the control instead. */
function RequiredMark() {
  return (
    <span aria-hidden="true" data-slot="required-mark" className="-ml-1 text-destructive">
      *
    </span>
  )
}

function Label({
  className,
  children,
  required = false,
  ...props
}: React.ComponentProps<typeof LabelPrimitive.Root> & { required?: boolean }) {
  return (
    <LabelPrimitive.Root
      data-slot="label"
      className={cn(
        'flex items-center gap-2 text-sm leading-none font-medium select-none group-data-[disabled=true]:pointer-events-none group-data-[disabled=true]:opacity-50 peer-disabled:cursor-not-allowed peer-disabled:opacity-50',
        className,
      )}
      {...props}
    >
      {children}
      {required && <RequiredMark />}
    </LabelPrimitive.Root>
  )
}

export { Label, RequiredMark }
