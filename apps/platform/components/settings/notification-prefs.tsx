'use client'

import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { BellRing } from 'lucide-react'
import { useState } from 'react'
import { toast } from 'sonner'

import { ErrorState } from '@/components/common/error-state'
import { NativeSelect } from '@/components/common/native-select'
import { Button } from '@/components/ui/button'
import { Skeleton } from '@/components/ui/skeleton'
import { Switch } from '@/components/ui/switch'
import { api, type NotificationChoice, type NotificationPrefs } from '@/lib/api'
import { toastApiError } from '@/lib/notify'
import { qk } from '@/lib/queries'

import { SettingsCard as Card } from './settings-card'

const LABELS: Record<string, [string, string]> = {
  'post.published': ['Post published', 'Each post once, with a link per channel'],
  'post.failed': ['Post failed to publish', 'Which channel refused it and why'],
  'post.feedback_added': ['Feedback on a preview', 'A reviewer commented on a shared post'],
  'channel.needs_reconnect': ['Channel needs reconnecting', 'For owners and admins'],
  'schedule.posts_requested': ['Schedule out of posts', 'For owners and admins'],
  'invitation.accepted': ['Someone joined', 'For owners and admins'],
}

/**
 * The person's own notifications: per event an email (to the address they sign in with) and the bell, and
 * whether post events cover their own posts or every post. One Save for the card.
 */
export function NotificationPrefsCard() {
  const qc = useQueryClient()
  const prefs = useQuery({ queryKey: qk.notificationPrefs, queryFn: api.notificationPrefs })
  const [draft, setDraft] = useState<NotificationPrefs | null>(null)
  const save = useMutation({
    mutationFn: (p: NotificationPrefs) => api.setNotificationPrefs({ scope: p.scope, events: p.events }),
    onSuccess: (p) => {
      qc.setQueryData(qk.notificationPrefs, p)
      setDraft(null)
      toast.success('Notification settings saved')
    },
    onError: (e) => toastApiError(e),
  })

  const current = draft ?? prefs.data
  const dirty = !!draft
  const set = (event: string, key: 'email' | 'in_app', value: boolean) =>
    current &&
    setDraft({
      ...current,
      events: current.events.map((c) => (c.event === event ? ({ ...c, [key]: value } as NotificationChoice) : c)),
    })

  return (
    <Card
      title="Notifications"
      icon={<BellRing />}
      description={
        current?.email
          ? `What Staqly tells you about, by email to ${current.email} and in the bell.`
          : 'What Staqly tells you about, by email and in the bell.'
      }
    >
      <div id="notifications" className="grid gap-4 p-4 scroll-mt-20">
        {prefs.isLoading ? (
          <Skeleton className="h-48" />
        ) : prefs.error ? (
          <ErrorState error={prefs.error} onRetry={() => prefs.refetch()} />
        ) : current ? (
          <>
            {!current.mail_ready && (
              <p className="rounded-lg border border-dashed p-3 text-sm text-muted-foreground">
                This server can&apos;t send email yet, so only the bell works until the operator sets up mail.
              </p>
            )}
            <div className="flex flex-wrap items-center justify-between gap-3">
              <label htmlFor="notify-scope" className="text-sm font-medium">
                Posts to tell me about
              </label>
              <NativeSelect
                id="notify-scope"
                className="w-full sm:w-64"
                value={current.scope}
                onChange={(e) => setDraft({ ...current, scope: e.target.value as NotificationPrefs['scope'] })}
              >
                <option value="mine">My posts</option>
                <option value="all">Every post in my workspaces</option>
              </NativeSelect>
            </div>
            <table className="w-full text-sm">
              <caption className="sr-only">Notifications by event</caption>
              <thead>
                <tr className="text-xs text-muted-foreground">
                  <th scope="col" className="pb-2 text-left font-medium">
                    Event
                  </th>
                  <th scope="col" className="w-16 pb-2 text-center font-medium">
                    Email
                  </th>
                  <th scope="col" className="w-16 pb-2 text-center font-medium">
                    Bell
                  </th>
                </tr>
              </thead>
              <tbody className="divide-y">
                {current.events.map((c) => {
                  const [label, hint] = LABELS[c.event] ?? [c.event, '']

                  return (
                    <tr key={c.event}>
                      <th scope="row" className="py-2.5 pr-3 text-left font-normal">
                        <span className="block font-medium">{label}</span>
                        {hint && <span className="block text-xs text-muted-foreground">{hint}</span>}
                      </th>
                      <td className="py-2.5 text-center">
                        <Switch
                          checked={c.email}
                          onCheckedChange={(v) => set(c.event, 'email', v)}
                          aria-label={`${label}: email`}
                        />
                      </td>
                      <td className="py-2.5 text-center">
                        <Switch
                          checked={c.in_app}
                          onCheckedChange={(v) => set(c.event, 'in_app', v)}
                          aria-label={`${label}: bell`}
                        />
                      </td>
                    </tr>
                  )
                })}
              </tbody>
            </table>
            <div className="flex justify-end gap-2">
              {dirty && (
                <Button type="button" variant="ghost" onClick={() => setDraft(null)}>
                  Cancel
                </Button>
              )}
              <Button type="button" disabled={!dirty || save.isPending} onClick={() => current && save.mutate(current)}>
                {save.isPending ? 'Saving...' : 'Save changes'}
              </Button>
            </div>
          </>
        ) : null}
      </div>
    </Card>
  )
}
