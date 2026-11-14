'use client'

import { Globe } from 'lucide-react'
import { memo } from 'react'

import { ChannelAvatar } from '@/components/channels/channel-avatars'
import type { Channel, PostTarget } from '@/lib/api'
import { cn } from '@/lib/utils'

/**
 * A picked channel in a row of tabs (the composer's text row, the preview's channel row): its avatar with the
 * platform badge, a dot when it has its own text, its delivery state. `label` names what the tab shows for it.
 */
export const ChannelTab = memo(function ChannelTab({
  channel,
  selected,
  own,
  status,
  label = 'Text for',
  testId = 'tab-channel',
  onSelect,
}: {
  channel: Channel
  selected: boolean
  own?: boolean
  status?: PostTarget['status']
  /** "Text for" in the composer, "Preview for" in the preview. */
  label?: string
  testId?: string
  onSelect: (id: string) => void
}) {
  return (
    <button
      type="button"
      role="tab"
      aria-selected={selected}
      aria-label={`${label} ${channel.name}`}
      title={`${label} ${channel.name}`}
      onClick={() => onSelect(channel.id)}
      data-testid={`${testId}-${channel.id}`}
      className="group/tab inline-grid place-items-center rounded-full p-0.5 outline-none pointer-coarse:p-1.5"
    >
      <span className="relative inline-flex size-7">
        {/* The ring circles the avatar alone (the platform badge sits on top of it): drawn first, under the avatar. */}
        <span
          aria-hidden
          data-testid="tab-channel-ring"
          className={cn(
            // Solid colours: the theme's border and focus colours are see-through and blur against the avatar.
            'pointer-events-none absolute -inset-0.5 rounded-full ring-2 group-focus-visible/tab:ring-brand',
            selected ? 'ring-brand' : 'ring-transparent group-hover/tab:ring-muted-foreground',
          )}
        />
        <ChannelAvatar channel={channel} className="size-7" />
        {own && (
          <span className="absolute -top-0.5 -right-0.5 size-2.5 rounded-full bg-primary ring-2 ring-background" />
        )}
        {/* Delivery state at the top left, clear of the platform badge at the bottom right. */}
        {status && status !== 'pending' && (
          <span
            aria-hidden
            data-testid="tab-channel-status"
            className={cn(
              'absolute -top-0.5 -left-0.5 size-2.5 rounded-full ring-2 ring-background',
              status === 'published' && 'bg-emerald-500',
              status === 'failed' && 'bg-destructive',
              status === 'publishing' && 'bg-sky-500',
            )}
          />
        )}
      </span>
    </button>
  )
})

/** The first tab of such a row: the shared text (the composer) or the common preview. */
export function AllChannelsTab({ selected, onSelect }: { selected: boolean; onSelect: (id: 'all') => void }) {
  return (
    <button
      type="button"
      role="tab"
      aria-selected={selected}
      onClick={() => onSelect('all')}
      className={cn(
        'inline-flex h-8 items-center gap-1.5 rounded-full border px-3 text-sm outline-none focus-visible:ring-2 focus-visible:ring-ring pointer-coarse:h-10',
        selected ? 'border-primary bg-primary/10 font-medium' : 'hover:bg-muted',
      )}
    >
      <Globe className="size-4" /> All channels
    </button>
  )
}
