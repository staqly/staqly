'use client'

import { Info } from 'lucide-react'

import { ChannelAvatar } from '@/components/channels/channel-avatars'
import {
  AlertDialog,
  AlertDialogAction,
  AlertDialogCancel,
  AlertDialogContent,
  AlertDialogDescription,
  AlertDialogFooter,
  AlertDialogHeader,
  AlertDialogTitle,
} from '@/components/ui/alert-dialog'
import type { Channel } from '@/lib/api'

export interface ChannelNotes {
  channel: Channel
  notes: string[]
}

/**
 * Before a post goes out: each channel that leaves something out (files it doesn't take, comments it can't post),
 * so nobody finds out after publishing. Go on publishes it as the list says; Cancel goes back to the post.
 */
export function PublishReview({
  open,
  onOpenChange,
  channels,
  confirmLabel,
  onConfirm,
}: {
  open: boolean
  onOpenChange: (open: boolean) => void
  channels: ChannelNotes[]
  confirmLabel: string
  onConfirm: () => void
}) {
  return (
    <AlertDialog open={open} onOpenChange={onOpenChange}>
      <AlertDialogContent data-testid="publish-review" className="sm:data-[size=default]:max-w-lg">
        <AlertDialogHeader>
          <AlertDialogTitle>Some channels take less</AlertDialogTitle>
          <AlertDialogDescription>
            Each one gets what it can take; everything else goes out as written.
          </AlertDialogDescription>
        </AlertDialogHeader>
        <ul className="grid gap-2">
          {channels.map(({ channel, notes }) => (
            <li key={channel.id} className="rounded-lg border bg-muted/30 p-3">
              <div className="flex min-w-0 items-center gap-2.5">
                <ChannelAvatar channel={channel} className="size-8" />
                <p className="min-w-0 text-sm font-medium wrap-anywhere">{channel.name}</p>
              </div>
              <ul className="mt-2.5 grid gap-1.5 text-sm">
                {notes.map((n) => {
                  // A note reads "what the platform takes: what happens to this post"; the two go on their own lines.
                  const at = n.indexOf(': ')
                  const rule = at < 0 ? n : n.slice(0, at)
                  const what = at < 0 ? '' : n.slice(at + 2)

                  return (
                    <li key={n} className="flex gap-2">
                      <Info aria-hidden className="mt-0.5 size-4 shrink-0 text-muted-foreground" />
                      <div className="min-w-0 wrap-anywhere">
                        <p className="font-medium">{rule}</p>
                        {what ? <p className="text-muted-foreground first-letter:uppercase">{what}</p> : null}
                      </div>
                    </li>
                  )
                })}
              </ul>
            </li>
          ))}
        </ul>
        <AlertDialogFooter>
          <AlertDialogCancel>Back to the post</AlertDialogCancel>
          <AlertDialogAction onClick={onConfirm} data-testid="publish-review-confirm">
            {confirmLabel}
          </AlertDialogAction>
        </AlertDialogFooter>
      </AlertDialogContent>
    </AlertDialog>
  )
}
