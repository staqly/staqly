'use client'

import { ChevronDown, ExternalLink, XCircle } from 'lucide-react'
import { useId, useState } from 'react'

import { ChannelAvatar, ChannelStack, type ChannelLike } from '@/components/channels/channel-avatars'
import { channelLabel, type NotificationChannel } from '@/lib/notifications'
import { safeHttpUrl } from '@/lib/safe'
import { TONES } from '@/lib/tones'
import { cn } from '@/lib/utils'

const like = (c: NotificationChannel): ChannelLike => ({
  id: c.id,
  provider: c.provider,
  name: channelLabel(c),
  avatar_url: c.avatar_url,
})

/**
 * The channels of one publish to several of them: their avatars stacked, and on demand each channel with its
 * outcome and its link on the platform. Failed channels are named in red even while the list is folded.
 */
export function PublishChannels({ channels }: { channels: NotificationChannel[] }) {
  const [open, setOpen] = useState(false)
  const listId = useId()
  const failed = channels.filter((c) => c.status === 'failed')

  return (
    <div className="mt-1 grid min-w-0 gap-1">
      <button
        type="button"
        aria-expanded={open}
        aria-controls={listId}
        onClick={() => setOpen((o) => !o)}
        className="inline-flex w-fit max-w-full items-center gap-2 rounded-sm text-xs text-muted-foreground hover:text-foreground pointer-coarse:min-h-10"
      >
        <ChannelStack channels={channels.map(like)} max={4} size="size-5" />
        <span>{open ? 'Hide channels' : `Show ${channels.length} channels`}</span>
        <ChevronDown className={cn('size-3 shrink-0 transition-transform', open && 'rotate-180')} aria-hidden />
      </button>
      {!open && failed.length > 0 && (
        <p className={cn('text-xs wrap-anywhere', TONES.danger.text)}>
          Failed on {failed.map((c) => channelLabel(c)).join(', ')}
        </p>
      )}
      {open && (
        <ul id={listId} className="grid gap-1.5 py-0.5" data-testid="publish-channels">
          {channels.map((c) => (
            <PublishChannelRow key={c.id} channel={c} />
          ))}
        </ul>
      )}
    </div>
  )
}

function PublishChannelRow({ channel: c }: { channel: NotificationChannel }) {
  const link = c.url ? safeHttpUrl(c.url) : null
  const label = channelLabel(c)

  return (
    <li className="grid grid-cols-[auto_minmax(0,1fr)] items-start gap-x-2 text-xs">
      <ChannelAvatar channel={like(c)} className="mt-px size-5" />
      <div className="grid min-w-0 gap-0.5">
        <span className="flex min-w-0 flex-wrap items-center gap-x-2 gap-y-0.5">
          <span className="min-w-0 truncate font-medium text-foreground" title={label}>
            {label}
          </span>
          {c.status === 'published' ? (
            <span className={TONES.success.text}>Published</span>
          ) : (
            <span className={cn('inline-flex items-center gap-1', TONES.danger.text)}>
              <XCircle className="size-3" aria-hidden /> Failed
            </span>
          )}
        </span>
        {c.status === 'failed' && c.error && <span className={cn('wrap-anywhere', TONES.danger.text)}>{c.error}</span>}
        {link && (
          <a
            href={link}
            target="_blank"
            rel="noopener noreferrer nofollow"
            className="inline-flex w-fit items-center gap-1 text-brand underline underline-offset-2 pointer-coarse:min-h-10"
            aria-label={`Open on the platform: ${label}`}
          >
            Open on the platform <ExternalLink className="size-3" aria-hidden />
          </a>
        )}
      </div>
    </li>
  )
}
