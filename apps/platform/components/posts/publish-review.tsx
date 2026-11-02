'use client'

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
      <AlertDialogContent data-testid="publish-review">
        <AlertDialogHeader>
          <AlertDialogTitle>Some channels take less</AlertDialogTitle>
          <AlertDialogDescription>
            Each one gets what it can take; everything else goes out as written.
          </AlertDialogDescription>
        </AlertDialogHeader>
        <ul className="grid gap-3">
          {channels.map(({ channel, notes }) => (
            <li key={channel.id} className="flex gap-3">
              <ChannelAvatar channel={channel} className="size-7" />
              <div className="min-w-0 text-sm">
                <p className="font-medium wrap-anywhere">{channel.name}</p>
                <ul className="mt-0.5 list-disc pl-4 text-muted-foreground">
                  {notes.map((n) => (
                    <li key={n} className="wrap-anywhere">
                      {n}
                    </li>
                  ))}
                </ul>
              </div>
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
