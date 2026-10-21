'use client'

import { useQuery } from '@tanstack/react-query'
import { AlertTriangle, Bell, CheckCircle2, ExternalLink, Info, XCircle } from 'lucide-react'
import { useRouter } from 'next/navigation'
import { useMemo, useState, type ReactNode } from 'react'

import { Button } from '@/components/ui/button'
import { Popover, PopoverContent, PopoverTrigger } from '@/components/ui/popover'
import { api } from '@/lib/api'
import { timeAgo } from '@/lib/format'
import { notificationsFor, readSeenSeq, writeSeenSeq, type Notification } from '@/lib/notifications'
import { qk, useRecentEvents, useRole, useSession } from '@/lib/queries'
import { safeHttpUrl } from '@/lib/safe'
import { TONES, type Tone } from '@/lib/tones'
import { cn } from '@/lib/utils'

const ICONS: Record<Tone, typeof Bell> = {
  success: CheckCircle2,
  danger: XCircle,
  warning: AlertTriangle,
  info: Info,
  neutral: Info,
  attention: AlertTriangle,
  accent: Info,
}

/**
 * The bell: what the automations and staqly did while nobody was looking (published, failed, started on
 * schedule, expired, a source failing), newest first, from the same small events request that keeps every
 * page live. Unread = newer than the last seen mark (kept per workspace in localStorage): unread rows carry a
 * dot, closing the list or Mark all as read moves the mark.
 */
/** The bell's state: the list, the unread count and the seen mark of this workspace. */
export function useNotifications() {
  const events = useRecentEvents()
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
  const list = useMemo(() => notificationsFor(events.data, muted), [events.data, muted])
  const unread = list.filter((n) => n.seq > seenSeq).length

  function markRead() {
    const top = list[0]?.seq ?? events.data?.[0]?.seq ?? seenSeq

    if (top > seenSeq) {
      writeSeenSeq(workspace, top)
      setSeen({ workspace, seq: top })
    }
  }

  return {
    list,
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
  const { list, unread, seenSeq, markRead } = notes

  function change(o: boolean) {
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
        {list.length === 0 ? (
          <p className="px-3 py-6 text-center text-sm text-muted-foreground">
            Nothing new. Published, failed and scheduled runs show up here.
          </p>
        ) : (
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
                      onClick={() => go(n)}
                      className={cn(
                        'block w-full text-left text-sm leading-snug wrap-anywhere hover:underline pointer-coarse:py-1',
                        isNew && 'font-medium',
                      )}
                    >
                      {n.text}
                    </button>
                    {link && (
                      <a
                        href={link}
                        target="_blank"
                        rel="noopener noreferrer nofollow"
                        className="mt-0.5 inline-flex max-w-full items-center gap-1 text-xs text-brand underline underline-offset-2 pointer-coarse:py-1"
                      >
                        <span className="truncate">{link}</span>
                        <ExternalLink className="size-3 shrink-0" aria-hidden />
                      </a>
                    )}
                    <span className="mt-0.5 block text-xs text-muted-foreground" title={n.at}>
                      {timeAgo(n.at)}
                    </span>
                  </div>
                  {isNew && <span className="mt-1.5 size-2 rounded-full bg-brand" aria-label="New" role="img" />}
                </li>
              )
            })}
          </ul>
        )}
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
