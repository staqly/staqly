'use client'

import { Bell, ExternalLink, History } from 'lucide-react'
import Link from 'next/link'
import { useMemo } from 'react'

import { IdChip } from '@/components/common/id-chip'
import { PageContainer, PageHeader } from '@/components/common/page-header'
import { ICONS, NOTIFICATIONS_QUERY, useNotifications } from '@/components/shell/notifications-bell'
import { Button } from '@/components/ui/button'
import { Skeleton } from '@/components/ui/skeleton'
import type { StashEvent } from '@/lib/api'
import { formatDateTime, lazyDateFormat, timeAgo } from '@/lib/format'
import { notificationFor, type Notification } from '@/lib/notifications'
import { useEventsInfinite, useSession } from '@/lib/queries'
import { safeHttpUrl } from '@/lib/safe'
import { TONES } from '@/lib/tones'
import { cn } from '@/lib/utils'

const dayFormat = lazyDateFormat({ weekday: 'long', day: 'numeric', month: 'long', year: 'numeric' })

/** "Today", "Yesterday", or the date: the heading of a day's notifications. */
const dayKey = (x: Date) => x.toDateString()

function dayLabel(at: string, now = new Date()) {
  const d = new Date(at)
  const key = dayKey
  const yesterday = new Date(now)

  yesterday.setDate(now.getDate() - 1)

  return key(d) === key(now) ? 'Today' : key(d) === key(yesterday) ? 'Yesterday' : dayFormat().format(d)
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
  const rows = useMemo(
    () =>
      (events.data?.pages.flatMap((p) => p.data) ?? []).flatMap((e) => {
        const n = notificationFor(e)

        return n ? [{ e, n }] : []
      }),
    [events.data],
  )
  const days = useMemo(() => {
    const out: { label: string; rows: typeof rows }[] = []

    for (const r of rows) {
      const label = dayLabel(r.n.at)

      if (out.at(-1)?.label !== label) {
        out.push({ label, rows: [] })
      }

      out.at(-1)!.rows.push(r)
    }

    return out
  }, [rows])

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
        ) : rows.length === 0 ? (
          <p className="rounded-xl border p-8 text-center text-sm text-muted-foreground">
            No notifications yet. Published, failed and scheduled runs show up here.
          </p>
        ) : (
          days.map((day) => (
            <section key={day.label} aria-label={day.label} className="grid gap-2">
              <h2 className="text-xs font-medium tracking-wide text-muted-foreground uppercase">{day.label}</h2>
              <ul className="divide-y rounded-xl border" data-testid="notifications-page-list">
                {day.rows.map(({ e, n }) => (
                  <NotificationRow key={n.id} e={e} n={n} workspace={workspace} isNew={n.seq > notes.seenSeq} />
                ))}
              </ul>
            </section>
          ))
        )}
        {events.hasNextPage && (
          <Button
            variant="outline"
            className="justify-self-center"
            onClick={() => void events.fetchNextPage()}
            disabled={events.isFetchingNextPage}
          >
            {events.isFetchingNextPage ? 'Loading...' : 'Load older'}
          </Button>
        )}
      </div>
    </PageContainer>
  )
}

/** One notification: its sentence (to the page it is about), when, and its details. */
function NotificationRow({
  e,
  n,
  workspace,
  isNew,
}: {
  e: StashEvent
  n: Notification
  workspace?: string
  isNew: boolean
}) {
  const Icon = ICONS[n.tone]
  const link = n.link ? safeHttpUrl(n.link) : null
  const channel = (e.data as { channel?: { name?: string } } | undefined)?.channel?.name
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
              className={cn('min-w-0 text-sm leading-snug wrap-anywhere hover:underline', isNew && 'font-medium')}
            >
              {n.text}
            </Link>
          ) : (
            <p className={cn('min-w-0 text-sm leading-snug wrap-anywhere', isNew && 'font-medium')}>{n.text}</p>
          )}
          {isNew && <span className="mt-1.5 size-2 shrink-0 rounded-full bg-brand" aria-label="New" role="img" />}
        </div>
        <dl className="flex flex-wrap items-center gap-x-4 gap-y-1 text-xs text-muted-foreground">
          <div className="flex gap-1">
            <dt className="sr-only">When</dt>
            <dd title={formatDateTime(n.at)}>
              {formatDateTime(n.at)} · {timeAgo(n.at)}
            </dd>
          </div>
          {workspace && (
            <div className="flex gap-1">
              <dt>Workspace</dt>
              <dd className="text-foreground/80">{workspace}</dd>
            </div>
          )}
          {channel && (
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
            <Link href={`/posts/${encodeURIComponent(e.post_id)}`} className="text-brand underline underline-offset-2">
              Open the post
            </Link>
          )}
          {link && (
            <a
              href={link}
              target="_blank"
              rel="noopener noreferrer nofollow"
              className="inline-flex items-center gap-1 text-brand underline underline-offset-2"
            >
              Open on the platform <ExternalLink className="size-3" aria-hidden />
            </a>
          )}
          <Link href={activity} className="inline-flex items-center gap-1 text-muted-foreground hover:text-foreground">
            <History className="size-3" aria-hidden /> In Activity
          </Link>
        </div>
      </div>
    </li>
  )
}
