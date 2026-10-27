'use client'

import { ProviderIcon, ProviderTile } from '@/lib/providers'
import { safeImageSrc } from '@/lib/safe'
import { cn } from '@/lib/utils'

export interface ChannelLike {
  id: string
  provider: string
  name: string
  avatar_url?: string | null
}

/** One channel's picture: its profile picture from the platform, or the provider tile when there is none. */
export function ChannelAvatar({ channel, className }: { channel: ChannelLike; className?: string }) {
  // A link, or a small picture kept inline (a Telegram chat's: its file address would carry the bot's token).
  const avatar = safeImageSrc(channel.avatar_url)

  if (avatar) {
    return (
      <span className={cn('relative inline-flex size-9 shrink-0', className)}>
        {/* Many platforms hand out pictures already cut to a circle (white or dark corners, not always centred): a
            slight zoom inside our circle hides that edge instead of showing it as a lopsided ring. */}
        <span className="size-full overflow-hidden rounded-full">
          <img src={avatar} alt="" className="size-full scale-[1.12] object-cover" referrerPolicy="no-referrer" />
        </span>
        {/* The platform badge scales with the avatar (16 px on a 36 px one), its mark centred inside. */}
        <span className="absolute -right-[6%] -bottom-[6%] grid aspect-square w-[44%] place-items-center rounded-full bg-background ring-1 ring-background">
          <ProviderIcon id={channel.provider} className="size-[62%]" />
        </span>
      </span>
    )
  }

  // A Slack workspace without an icon of its own: its first letter, as Slack itself shows it.
  const letter = LETTER_AVATARS.has(channel.provider) ? channel.name.match(/[\p{L}\p{N}]/u)?.[0] : undefined

  if (letter) {
    return (
      <span className={cn('relative inline-flex size-9 shrink-0', className)}>
        <span className="grid size-full place-items-center rounded-full bg-muted text-[0.9em] font-semibold text-foreground uppercase [container-type:size]">
          <span className="text-[45cqh] leading-none">{letter}</span>
        </span>
        <span className="absolute -right-[6%] -bottom-[6%] grid aspect-square w-[44%] place-items-center rounded-full bg-background ring-1 ring-background">
          <ProviderIcon id={channel.provider} className="size-[62%]" />
        </span>
      </span>
    )
  }

  return <ProviderTile id={channel.provider} name={channel.name} className={className} />
}

/** Platforms whose channel is a named place (a workspace), shown by its first letter until it has an icon. */
const LETTER_AVATARS = new Set(['slack', 'telegram'])

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
        // The ring sits on a square box of the avatar's size (an inline span is taller than wide: an oval ring).
        <span
          key={c.id}
          aria-hidden
          className={cn('inline-flex shrink-0 rounded-full ring-2 ring-background', size, i > 0 && '-ml-1.5')}
        >
          <ChannelAvatar channel={c} className="size-full" />
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
