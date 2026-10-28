'use client'

import { keepPreviousData, useQuery } from '@tanstack/react-query'
import { ArrowRight, Bell } from 'lucide-react'
import dynamic from 'next/dynamic'
import { useRouter } from 'next/navigation'
import { useMemo, useState, type ReactNode } from 'react'

import { Button } from '@/components/ui/button'
import { Popover, PopoverContent, PopoverTrigger } from '@/components/ui/popover'
import { api } from '@/lib/api'
import { groupNotificationEvents, NOTIFICATION_TYPES, readSeenSeq, writeSeenSeq } from '@/lib/notification-groups'
import type { Notification } from '@/lib/notifications'
import { qk, useRecentEvents, useRole, useSession } from '@/lib/queries'
import { cn } from '@/lib/utils'

// The rows (icons, a publish's channel avatars with every platform's mark) load when the bell opens, not with
// the shell.
const NotificationList = dynamic(
  () => import('@/components/notifications/notification-list').then((m) => m.NotificationList),
  { ssr: false, loading: () => <div className="h-16" aria-hidden /> },
)

/** The events request behind the bell and the Notifications page (they share its first page). */
export const NOTIFICATIONS_QUERY = { types: NOTIFICATION_TYPES, limit: 30 }

/**
 * The bell: what the automations and staqly did while nobody was looking (published, failed, started on
 * schedule, expired, a source failing), newest first, from the same small events request that keeps every
 * page live. Unread = newer than the last seen mark (kept per workspace in localStorage): unread rows carry a
 * dot, closing the list or Mark all as read moves the mark.
 */
/**
 * The bell's state: its entries (events grouped, one per publish; worded by the list when it opens), the unread
 * count and the seen mark of this workspace.
 */
export function useNotifications() {
  // Only the events that make a notification; refreshed with every new event (the live list's newest).
  const live = useRecentEvents().data?.[0]?.seq
  const events = useQuery({
    queryKey: ['notifications', live ?? 0],
    queryFn: async () => (await api.events(NOTIFICATIONS_QUERY)).data,
    placeholderData: keepPreviousData,
  })
  const recent = events.data
  const session = useSession()
  const workspace = session.data?.workspace?.id ?? 'local'
  // The seen mark: read from storage once per workspace (not per render), then kept here so the badge reacts at once.
  const [seen, setSeen] = useState<{ workspace: string; seq: number } | null>(null)
  const stored = useMemo(() => readSeenSeq(workspace), [workspace])
  const seenSeq = seen?.workspace === workspace ? seen.seq : stored
  // People in an account can turn events off for the bell (Settings, Account, Notifications).
  const { accountsEnabled } = useRole()
  const prefs = useQuery({ queryKey: qk.notificationPrefs, queryFn: api.notificationPrefs, enabled: accountsEnabled })
  const muted = useMemo(
    () => new Set((prefs.data?.events ?? []).filter((c) => !c.in_app).map((c) => c.event)),
    [prefs.data],
  )
  const groups = useMemo(() => groupNotificationEvents(recent, muted), [recent, muted])
  const unread = groups.filter((g) => g[0]!.seq > seenSeq).length

  function markRead() {
    const top = groups[0]?.[0]?.seq ?? recent?.[0]?.seq ?? seenSeq

    if (top > seenSeq) {
      writeSeenSeq(workspace, top)
      setSeen({ workspace, seq: top })
    }
  }

  return {
    groups,
    unread,
    seenSeq,
    markRead,
    count: unread > 99 ? '99+' : String(unread),
    label: unread > 0 ? `Notifications, ${unread} unread` : 'Notifications',
  }
}

export type NotificationsState = ReturnType<typeof useNotifications>

/** The list in a popover; `children` is its trigger. */
export function NotificationsPopover({
  notes,
  open,
  onOpenChange,
  side = 'bottom',
  align = 'end',
  children,
}: {
  notes: NotificationsState
  open: boolean
  onOpenChange: (o: boolean) => void
  side?: 'bottom' | 'right'
  align?: 'start' | 'end'
  children: ReactNode
}) {
  const router = useRouter()
  const { groups, unread, seenSeq, markRead } = notes
  const zone = useSession().data?.workspace?.timezone
  // The bell holds what is new; everything else is on the Notifications page. What was new when it opened stays
  // while it is open (Mark all as read takes the dots off), and goes once it closes.
  const [since, setSince] = useState(seenSeq)
  const cut = open ? since : seenSeq
  const fresh = useMemo(() => groups.filter((g) => g[0]!.seq > cut), [groups, cut])

  function change(o: boolean) {
    if (o) {
      setSince(seenSeq)
    }

    onOpenChange(o)

    // Seen once the list closes: the count and the dots go, the rows stay.
    if (!o) {
      markRead()
    }
  }

  function go(n: Notification) {
    change(false)

    if (n.href) {
      router.push(n.href)
    }
  }

  return (
    <Popover open={open} onOpenChange={change}>
      <PopoverTrigger asChild>{children}</PopoverTrigger>
      <PopoverContent
        side={side}
        align={align}
        // Beside the sidebar: clear of its edge rather than over it.
        sideOffset={side === 'right' ? 12 : undefined}
        collisionPadding={8}
        className="w-[min(100vw-1rem,24rem)] p-0"
        aria-label="Notifications"
      >
        <div className="flex items-center justify-between gap-2 border-b px-3 py-2">
          <h2 className="text-sm font-semibold">Notifications</h2>
          <Button type="button" size="xs" variant="ghost" onClick={markRead} disabled={unread === 0}>
            Mark all as read
          </Button>
        </div>
        {fresh.length === 0 ? (
          <p className="px-3 py-6 text-center text-sm text-muted-foreground">
            You&apos;re all caught up. Published, failed and scheduled runs show up here.
          </p>
        ) : (
          <NotificationList groups={fresh} seenSeq={seenSeq} zone={zone} onGo={go} />
        )}
        <div className="border-t p-1">
          <Button
            type="button"
            variant="ghost"
            size="sm"
            className="w-full justify-center"
            data-testid="notifications-all"
            onClick={() => {
              change(false)
              router.push('/notifications')
            }}
          >
            All notifications <ArrowRight />
          </Button>
        </div>
      </PopoverContent>
    </Popover>
  )
}

/** The bell button of the phone top bar; on desktop the list opens from Notifications in the sidebar. */
export function NotificationsBell({ className }: { className?: string }) {
  const notes = useNotifications()
  const [open, setOpen] = useState(false)

  return (
    <NotificationsPopover notes={notes} open={open} onOpenChange={setOpen}>
      <Button
        type="button"
        variant="ghost"
        size="icon"
        aria-label={notes.label}
        data-testid="notifications-bell"
        data-unread={notes.unread}
        className={cn('relative text-muted-foreground hover:text-foreground', className)}
      >
        <Bell />
        {notes.unread > 0 && (
          <span
            aria-hidden
            className="absolute -top-0.5 -right-0.5 min-w-4 rounded-full bg-emerald-700 px-1 text-center text-[10px] leading-4 font-semibold text-white tabular-nums ring-2 ring-background dark:bg-brand dark:text-brand-foreground"
          >
            {notes.count}
          </span>
        )}
      </Button>
    </NotificationsPopover>
  )
}
