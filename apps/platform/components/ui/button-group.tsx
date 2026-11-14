import { cva, type VariantProps } from 'class-variance-authority'
import { cn } from 'cn'
import * as React from 'react'

// shadcn ButtonGroup: joined buttons that share one outline. The child selectors win over each button's
// own radius (Button's size classes re-round themselves inside a group, so a plain rounded-r-none on the
// button is not enough).
const buttonGroupVariants = cva(
  'flex w-fit items-stretch [&>*]:focus-visible:relative [&>*]:focus-visible:z-10 has-[>[data-slot=button-group]]:gap-2',
  {
    variants: {
      orientation: {
        horizontal:
          '[&>*:not(:first-child)]:rounded-l-none! [&>*:not(:first-child)]:border-l-0 [&>*:not(:last-child)]:rounded-r-none!',
        vertical:
          'flex-col [&>*:not(:first-child)]:rounded-t-none! [&>*:not(:first-child)]:border-t-0 [&>*:not(:last-child)]:rounded-b-none!',
      },
    },
    defaultVariants: {
      orientation: 'horizontal',
    },
  },
)

function ButtonGroup({
  className,
  orientation,
  ...props
}: React.ComponentProps<'div'> & VariantProps<typeof buttonGroupVariants>) {
  return (
    <div
      role="group"
      data-slot="button-group"
      data-orientation={orientation ?? 'horizontal'}
      className={cn(buttonGroupVariants({ orientation }), className)}
      {...props}
    />
  )
}

export { ButtonGroup, buttonGroupVariants }
