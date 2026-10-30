'use client'

import { ExternalLink } from 'lucide-react'
import { useMemo } from 'react'

import type { StashEvent } from '@/lib/api'
import { formatDateTimeIn, timeAgo } from '@/lib/format'
import { notificationOf, type Notification } from '@/lib/notifications'
import { safeHttpUrl } from '@/lib/safe'
import { TONES } from '@/lib/tones'
import { cn } from '@/lib/utils'

import { ICONS } from './notification-icons'
import { PublishChannels } from './publish-channels'

/**
 * The bell's rows: each notification's sentence (opens where it leads), a publish's stacked channels, the link on
 * the platform and when, in the workspace's time zone on hover. Loaded when the bell opens, not with the shell.
 */
export function NotificationList({
  groups,
  seenSeq,
  zone,
  onGo,
}: {
  /** Entries of groupNotificationEvents, newest first. */
  groups: StashEvent[][]
  seenSeq: number
  zone?: string
  onGo: (n: Notification) => void
}) {
  const list = useMemo(
    () =>
      groups.flatMap((g) => {
        const n = notificationOf(g)

        return n ? [n] : []
      }),
    [groups],
  )

  return (
    <ul className="max-h-[60svh] overflow-y-auto overscroll-contain py-1" data-testid="notifications-list">
      {list.map((n) => {
        const Icon = ICONS[n.tone]
        const link = n.link ? safeHttpUrl(n.link) : undefined
        const isNew = n.seq > seenSeq

        return (
          <li
            key={n.id}
            data-unread={isNew || undefined}
            className="grid grid-cols-[auto_minmax(0,1fr)_auto] gap-x-2.5 px-3 py-2 hover:bg-muted/60"
          >
            <Icon className={cn('mt-0.5 size-4 shrink-0', TONES[n.tone].text)} aria-hidden />
            <div className="min-w-0">
              <button
                type="button"
                onClick={() => onGo(n)}
                className={cn(
                  'block min-h-6 w-full text-left text-sm leading-snug wrap-anywhere hover:underline pointer-coarse:min-h-10 pointer-coarse:py-1',
                  isNew && 'font-medium',
                )}
              >
                {n.text}
              </button>
              {n.channels && n.channels.length > 1 && <PublishChannels channels={n.channels} />}
              {link && (
                <a
                  href={link}
                  target="_blank"
                  rel="noopener noreferrer nofollow"
                  className="inline-flex min-h-6 max-w-full items-center gap-1 text-xs text-brand underline underline-offset-2 pointer-coarse:min-h-10"
                >
                  <span className="truncate">{link}</span>
                  <ExternalLink className="size-3 shrink-0" aria-hidden />
                </a>
              )}
              <time
                dateTime={n.at}
                className="mt-0.5 block text-xs text-muted-foreground"
                title={formatDateTimeIn(n.at, zone)}
              >
                {timeAgo(n.at)}
              </time>
            </div>
            {isNew && <span className="mt-1.5 size-2 rounded-full bg-brand" aria-label="New" role="img" />}
          </li>
        )
      })}
    </ul>
  )
}
