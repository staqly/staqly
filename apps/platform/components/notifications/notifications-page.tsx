'use client'

import { Bell, ExternalLink, History } from 'lucide-react'
import dynamic from 'next/dynamic'
import Link from 'next/link'
import { useEffect, useMemo, useState } from 'react'

import { ErrorState, LoadMore } from '@/components/common/error-state'
import { IdChip } from '@/components/common/id-chip'
import { PageContainer, PageHeader } from '@/components/common/page-header'
import { NOTIFICATIONS_QUERY, useNotifications } from '@/components/shell/notifications-bell'
import { Button } from '@/components/ui/button'
import { Skeleton } from '@/components/ui/skeleton'
import { formatDateTimeIn, lazyDateFormat, timeAgo } from '@/lib/format'
import { channelLabel, notificationsFor, type Notification } from '@/lib/notifications'
import { useEventsInfinite, useSession } from '@/lib/queries'
import { safeHttpUrl } from '@/lib/safe'
import { addDays, dayKey } from '@/lib/schedule'
import { TONES } from '@/lib/tones'
import { cn } from '@/lib/utils'

import { ICONS } from './notification-icons'

// A publish's channel avatars bring every platform's mark: loaded with the first stacked entry, not with the page.
const PublishChannels = dynamic(() => import('./publish-channels').then((m) => m.PublishChannels), {
  ssr: false,
  loading: () => <span className="mt-1 block h-5" aria-hidden />,
})

/** Notifications shown at first and added by each "Load more", whatever their day. */
const PAGE = 15

const dayFormat = lazyDateFormat({ weekday: 'long', day: 'numeric', month: 'long', year: 'numeric' })

/** "Today", "Yesterday", or the date: the heading of a day's notifications. */
function dayLabel(at: string, now = new Date()) {
  const d = new Date(at)
  const key = dayKey(d)

  return key === dayKey(now) ? 'Today' : key === dayKey(addDays(now, -1)) ? 'Yesterday' : dayFormat().format(d)
}

/**
 * Every notification of the workspace, newest first, a page at a time, by day: what happened, when, and where to
 * go from it (the post, the platform, the same event in Activity), with the event's id. The bell holds only what
 * is new and links here.
 */
export function NotificationsPage() {
  const session = useSession()
  const notes = useNotifications()
  const events = useEventsInfinite(NOTIFICATIONS_QUERY)
  const workspace = session.data?.workspace?.name
  const zone = session.data?.workspace?.timezone
  // Grouped over every loaded page: a publish split by a page boundary joins up once the older page loads.
  const rows = useMemo(() => notificationsFor(events.data?.pages.flatMap((p) => p.data)), [events.data])
  const [shown, setShown] = useState(PAGE)
  const { hasNextPage, isFetchingNextPage, isFetchNextPageError, isError, error, fetchNextPage, refetch } = events

  // A page of events groups into fewer notifications: keep fetching until there are enough to show.
  useEffect(() => {
    if (rows.length < shown && hasNextPage && !isFetchingNextPage && !isFetchNextPageError) {
      void fetchNextPage()
    }
  }, [rows.length, shown, hasNextPage, isFetchingNextPage, isFetchNextPageError, fetchNextPage])

  const days = useMemo(() => {
    const out: { label: string; rows: typeof rows }[] = []

    for (const r of rows.slice(0, shown)) {
      const label = dayLabel(r.at)

      if (out.at(-1)?.label !== label) {
        out.push({ label, rows: [] })
      }

      out.at(-1)!.rows.push(r)
    }

    return out
  }, [rows, shown])

  return (
    <PageContainer className="max-w-4xl">
      <PageHeader
        icon={<Bell />}
        title="Notifications"
        description={
          workspace
            ? `What was published, failed or needs you in ${workspace}.`
            : 'What was published, failed or needs you.'
        }
        actions={
          <Button variant="outline" onClick={notes.markRead} disabled={notes.unread === 0}>
            Mark all as read
          </Button>
        }
      />
      <div className="mt-6 grid gap-6">
        {events.isPending ? (
          <div className="grid gap-2">
            {Array.from({ length: 4 }, (_, i) => (
              <Skeleton key={i} className="h-20 rounded-xl" />
            ))}
          </div>
        ) : isError && !events.data ? (
          <ErrorState error={error} onRetry={() => void refetch()} />
        ) : rows.length === 0 ? (
          <p className="rounded-xl border p-8 text-center text-sm text-muted-foreground">
            No notifications yet. Published, failed and scheduled runs show up here.
          </p>
        ) : (
          days.map((day) => (
            <section key={day.label} aria-label={day.label} className="grid gap-2">
              <h2 className="text-xs font-medium tracking-wide text-muted-foreground uppercase">{day.label}</h2>
              <ul className="divide-y rounded-xl border" data-testid="notifications-page-list">
                {day.rows.map((n) => (
                  <NotificationRow key={n.id} n={n} workspace={workspace} zone={zone} isNew={n.seq > notes.seenSeq} />
                ))}
              </ul>
            </section>
          ))
        )}
        {(rows.length > shown || hasNextPage) && (
          <LoadMore
            loading={isFetchingNextPage}
            failed={isFetchNextPageError}
            onLoad={() => {
              if (isFetchNextPageError) {
                void fetchNextPage()
              } else {
                setShown((n) => n + PAGE)
              }
            }}
          />
        )}
      </div>
    </PageContainer>
  )
}

/** One notification: its sentence (to the page it is about), when, and its details. */
function NotificationRow({
  n,
  workspace,
  zone,
  isNew,
}: {
  n: Notification
  workspace?: string
  zone?: string
  isNew: boolean
}) {
  const e = n.event
  const Icon = ICONS[n.tone]
  const link = n.link ? safeHttpUrl(n.link) : null
  const only = n.channels?.length === 1 ? n.channels[0] : undefined
  const channel = only ? channelLabel(only) : (e.data as { channel?: { name?: string } } | undefined)?.channel?.name
  const when = formatDateTimeIn(n.at, zone)
  const activity = e.post_id
    ? `/activity?post=${encodeURIComponent(e.post_id)}`
    : `/activity?type=${encodeURIComponent(e.type)}`

  return (
    <li
      data-unread={isNew || undefined}
      className={cn('grid grid-cols-[auto_minmax(0,1fr)] gap-x-3 px-4 py-3', isNew && 'bg-brand-soft/20')}
    >
      <Icon className={cn('mt-0.5 size-4 shrink-0', TONES[n.tone].text)} aria-hidden />
      <div className="grid min-w-0 gap-1.5">
        <div className="flex items-start gap-2">
          {n.href ? (
            <Link
              href={n.href}
              prefetch={false}
              className={cn(
                'min-w-0 text-sm leading-snug wrap-anywhere hover:underline pointer-coarse:min-h-10',
                isNew && 'font-medium',
              )}
            >
              {n.text}
            </Link>
          ) : (
            <p className={cn('min-w-0 text-sm leading-snug wrap-anywhere', isNew && 'font-medium')}>{n.text}</p>
          )}
          {isNew && <span className="mt-1.5 size-2 shrink-0 rounded-full bg-brand" aria-label="New" role="img" />}
        </div>
        {n.channels && n.channels.length > 1 && <PublishChannels channels={n.channels} />}
        <dl className="flex flex-wrap items-center gap-x-4 gap-y-1 text-xs text-muted-foreground">
          <div className="flex gap-1">
            <dt className="sr-only">When</dt>
            <dd>
              <time dateTime={n.at} title={when}>
                {when} · {timeAgo(n.at)}
              </time>
            </dd>
          </div>
          {workspace && (
            <div className="flex gap-1">
              <dt>Workspace</dt>
              <dd className="text-foreground/80">{workspace}</dd>
            </div>
          )}
          {channel && !(n.channels && n.channels.length > 1) && (
            <div className="flex gap-1">
              <dt>Channel</dt>
              <dd className="text-foreground/80">{channel}</dd>
            </div>
          )}
          {e.actor?.name && (
            <div className="flex gap-1">
              <dt>By</dt>
              <dd className="text-foreground/80">
                {e.actor.name}
                {e.actor.agent ? ` (${e.actor.agent})` : ''}
              </dd>
            </div>
          )}
          <div className="flex min-w-0 items-center gap-1">
            <dt>Event</dt>
            <dd className="min-w-0">
              <IdChip id={e.id} label="event" />
            </dd>
          </div>
        </dl>
        <div className="flex flex-wrap items-center gap-x-3 gap-y-1 text-xs">
          {e.post_id && (
            <Link
              href={`/posts/${encodeURIComponent(e.post_id)}`}
              prefetch={false}
              className="inline-flex items-center text-brand underline underline-offset-2 pointer-coarse:min-h-10"
            >
              Open the post
            </Link>
          )}
          {link && (
            <a
              href={link}
              target="_blank"
              rel="noopener noreferrer nofollow"
              className="inline-flex items-center gap-1 text-brand underline underline-offset-2 pointer-coarse:min-h-10"
            >
              Open on the platform <ExternalLink className="size-3" aria-hidden />
            </a>
          )}
          <Link
            href={activity}
            prefetch={false}
            className="inline-flex items-center gap-1 text-muted-foreground hover:text-foreground pointer-coarse:min-h-10"
          >
            <History className="size-3" aria-hidden /> In Activity
          </Link>
        </div>
      </div>
    </li>
  )
}
