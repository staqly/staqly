'use client'

import { useMutation } from '@tanstack/react-query'
import { MailX } from 'lucide-react'
import Link from 'next/link'
import { useEffect, useRef } from 'react'

import { PublicShell } from '@/components/auth/public-shell'
import { Button } from '@/components/ui/button'
import { api, errorMessage } from '@/lib/api'

const EVENTS: Record<string, string> = {
  'post.published': 'published posts',
  'post.failed': 'posts that failed to publish',
  'post.feedback_added': 'feedback on previews',
  'channel.needs_reconnect': 'channels that need reconnecting',
  'schedule.posts_requested': 'schedules out of posts',
  'invitation.accepted': 'people joining',
}

/** The unsubscribe link of a personal email: turns that kind of email off at once, no sign-in. */
export function UnsubscribePage() {
  const run = useMutation({ mutationFn: (token: string) => api.unsubscribe(token) })
  const started = useRef(false)

  useEffect(() => {
    const token = window.location.hash.slice(1)

    if (!started.current && token) {
      started.current = true
      run.mutate(token)
    }
  }, [run])

  const what = run.data ? (EVENTS[run.data.event] ?? 'these') : ''

  return (
    <PublicShell className="flex max-w-md flex-col justify-center pt-6">
      <div className="grid gap-4 rounded-2xl border bg-card p-6 text-center shadow-xl shadow-black/[0.04]">
        <span className="mx-auto flex size-12 items-center justify-center rounded-2xl bg-brand-soft text-brand">
          <MailX className="size-6" />
        </span>
        {run.isSuccess ? (
          <>
            <h1 className="text-xl font-semibold tracking-tight">You&apos;re unsubscribed</h1>
            <p className="text-sm text-muted-foreground">
              No more emails about {what}. The bell in Staqly still shows them unless you turn that off too.
            </p>
          </>
        ) : run.isError ? (
          <>
            <h1 className="text-xl font-semibold tracking-tight">This link didn&apos;t work</h1>
            <p className="text-sm text-muted-foreground">{errorMessage(run.error)}</p>
          </>
        ) : (
          <h1 className="text-xl font-semibold tracking-tight">Unsubscribing...</h1>
        )}
        <Button asChild variant="outline" className="mx-auto">
          <Link href="/settings/account#notifications">Change notification settings</Link>
        </Button>
      </div>
    </PublicShell>
  )
}
