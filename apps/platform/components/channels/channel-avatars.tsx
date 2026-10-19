'use client'

import { ProviderIcon, ProviderTile } from '@/lib/providers'
import { cn } from '@/lib/utils'

export interface ChannelLike {
  id: string
  provider: string
  name: string
  avatar_url?: string | null
}

/** One channel's picture: its profile picture from the platform, or the provider tile when there is none. */
export function ChannelAvatar({ channel, className }: { channel: ChannelLike; className?: string }) {
  if (channel.avatar_url) {
    return (
      <span className={cn('relative inline-flex size-9 shrink-0', className)}>
        {/* eslint-disable-next-line @next/next/no-img-element */}
        <img
          src={channel.avatar_url}
          alt=""
          className="size-full rounded-full object-cover"
          referrerPolicy="no-referrer"
        />
        <span className="absolute -right-0.5 -bottom-0.5 flex size-4 items-center justify-center rounded-full bg-background ring-1 ring-border">
          <ProviderIcon id={channel.provider} className="size-2.5" />
        </span>
      </span>
    )
  }

  return <ProviderTile id={channel.provider} name={channel.name} className={className} />
}

/**
 * Several channels as a stack of small avatars (calendar chips, post rows): the first few overlap, the rest
 * is a "+n" bubble. The accessible name lists every channel.
 */
export function ChannelStack({
  channels,
  max = 3,
  size = 'size-6',
  className,
}: {
  channels: ChannelLike[]
  max?: number
  size?: string
  className?: string
}) {
  const shown = channels.slice(0, max)
  const more = channels.length - shown.length

  return (
    <span
      className={cn('inline-flex items-center', className)}
      role="img"
      aria-label={channels.map((c) => c.name).join(', ') || 'No channels'}
    >
      {shown.map((c, i) => (
        <span key={c.id} aria-hidden className={cn('rounded-full ring-2 ring-background', i > 0 && '-ml-1.5')}>
          <ChannelAvatar channel={c} className={size} />
        </span>
      ))}
      {more > 0 && (
        <span
          className={cn(
            '-ml-1.5 inline-flex items-center justify-center rounded-full bg-muted text-[10px] font-medium text-muted-foreground ring-2 ring-background',
            size,
          )}
        >
          +{more}
        </span>
      )}
    </span>
  )
}
