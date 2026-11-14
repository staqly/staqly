import { BellRing, Gamepad2, Hash, Mail, Send, type LucideIcon } from 'lucide-react'

import { CHANNEL_TYPE_INFO } from '@/lib/alerts'
import type { AlertType } from '@/lib/api'
import { cn } from '@/lib/utils'

const ICONS: Record<AlertType, { Icon: LucideIcon; cls: string }> = {
  telegram: { Icon: Send, cls: 'bg-sky-500/12 text-sky-600 dark:text-sky-400' },
  slack: { Icon: Hash, cls: 'bg-fuchsia-500/12 text-fuchsia-700 dark:text-fuchsia-300' },
  discord: { Icon: Gamepad2, cls: 'bg-indigo-500/12 text-indigo-600 dark:text-indigo-300' },
  ntfy: { Icon: BellRing, cls: 'bg-teal-500/12 text-teal-700 dark:text-teal-300' },
  email: { Icon: Mail, cls: 'bg-zinc-500/12 text-zinc-700 dark:text-zinc-300' },
}

/** Rounded tile with the channel type's glyph. `decorative` when the type's name is written next to it. */
export function ChannelTypeIcon({
  type,
  className,
  decorative,
}: {
  type: AlertType
  className?: string
  decorative?: boolean
}) {
  const { Icon, cls } = ICONS[type] ?? ICONS.email

  return (
    <span
      className={cn('flex size-9 shrink-0 items-center justify-center rounded-lg [&_svg]:size-4', cls, className)}
      {...(decorative
        ? { 'aria-hidden': true }
        : { role: 'img', 'aria-label': CHANNEL_TYPE_INFO[type]?.label ?? type })}
    >
      <Icon aria-hidden />
    </span>
  )
}
