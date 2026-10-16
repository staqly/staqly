import { CircleAlert, Info, TriangleAlert, type LucideIcon } from 'lucide-react'

import { TONES } from '@/lib/tones'
import { cn } from '@/lib/utils'

type CalloutTone = 'info' | 'warning' | 'danger'

const ICONS: Record<CalloutTone, LucideIcon> = {
  info: Info,
  warning: TriangleAlert,
  danger: CircleAlert,
}

/**
 * A tinted box with an icon for a warning, an error or a note inside a form, dialog or card. The tone comes
 * from lib/tones.ts; the text keeps the foreground colour. `size="xs"` for small print in dialogs and cards.
 * Pass `role` ("alert", "status", "note") where the message should be announced.
 */
export function Callout({
  tone = 'warning',
  icon: Icon = ICONS[tone],
  size = 'sm',
  action,
  className,
  children,
  ...props
}: Omit<React.ComponentProps<'div'>, 'children'> & {
  tone?: CalloutTone
  icon?: LucideIcon
  size?: 'sm' | 'xs'
  /** A button on the right of the message. */
  action?: React.ReactNode
  children: React.ReactNode
}) {
  return (
    <div
      data-slot="callout"
      className={cn(
        'flex items-start rounded-lg border',
        size === 'xs' ? 'gap-2 p-2.5 text-xs' : 'gap-3 p-3 text-sm',
        TONES[tone].callout,
        className,
      )}
      {...props}
    >
      <Icon className={cn('size-4 shrink-0', size === 'sm' && 'mt-0.5', TONES[tone].text)} aria-hidden />
      <div className="min-w-0 flex-1 break-words">{children}</div>
      {action}
    </div>
  )
}
