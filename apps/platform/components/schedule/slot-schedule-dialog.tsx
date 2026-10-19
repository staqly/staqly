'use client'

import { CalendarPlus, PenSquare, Search } from 'lucide-react'
import dynamic from 'next/dynamic'
import { useMemo, useState } from 'react'

import { ChannelStack } from '@/components/channels/channel-avatars'
import { Button } from '@/components/ui/button'
import { Dialog, DialogContent, DialogDescription, DialogHeader, DialogTitle } from '@/components/ui/dialog'
import { Input } from '@/components/ui/input'
import { Skeleton } from '@/components/ui/skeleton'
import type { Post, PostQuery } from '@/lib/api'
import { postTitle } from '@/lib/posts'
import { flattenPosts, usePostsInfinite } from '@/lib/queries'
import { formatPicked } from '@/lib/schedule'

import { useSetSchedule } from './use-schedule'

// The composer (large) loads only when "New post" is chosen.
const Composer = dynamic(() => import('@/components/posts/composer').then((m) => m.Composer), { ssr: false })

/**
 * "+" on a future slot of the calendar: give a draft (a post without a time) this time, or write a new post
 * that starts with it.
 */
export function SlotScheduleDialog({ at, onOpenChange }: { at: Date; onOpenChange: (open: boolean) => void }) {
  const [search, setSearch] = useState('')
  const [composing, setComposing] = useState(false)
  const query: PostQuery = useMemo(() => ({ status: 'draft', sort: 'created_desc', limit: 100 }), [])
  const list = usePostsInfinite(query, { refetchOnWindowFocus: false })
  const drafts = useMemo(() => {
    const all = flattenPosts(list.data).filter((p) => !p.publish_at)
    const q = search.trim().toLowerCase()

    return q ? all.filter((p) => p.content.toLowerCase().includes(q)) : all
  }, [list.data, search])
  const setSchedule = useSetSchedule({ onDone: () => onOpenChange(false) })
  const pick = (p: Post) => setSchedule.mutate({ post: p, publishAt: at.toISOString() })

  if (composing) {
    return (
      <Composer
        open
        onOpenChange={(o) => {
          if (!o) {
            onOpenChange(false)
          }
        }}
        defaultPublishAt={at.toISOString()}
      />
    )
  }

  return (
    <Dialog open onOpenChange={onOpenChange}>
      <DialogContent className="sm:max-w-lg">
        <DialogHeader>
          <DialogTitle className="flex items-center gap-2">
            <CalendarPlus className="size-4 text-muted-foreground" aria-hidden /> {formatPicked(at)}
          </DialogTitle>
          <DialogDescription>Pick a draft to publish at this time, or write a new post for it.</DialogDescription>
        </DialogHeader>
        <div className="grid gap-3">
          <div className="relative">
            <Search className="pointer-events-none absolute top-1/2 left-2.5 size-3.5 -translate-y-1/2 text-muted-foreground" />
            <Input
              value={search}
              onChange={(e) => setSearch(e.target.value)}
              placeholder="Search drafts"
              aria-label="Search drafts"
              className="pl-8"
            />
          </div>
          <div className="max-h-[40vh] overflow-y-auto overscroll-contain" data-testid="slot-drafts">
            {list.isLoading ? (
              <div className="grid gap-2">
                <Skeleton className="h-12" />
                <Skeleton className="h-12" />
              </div>
            ) : drafts.length === 0 ? (
              <p className="py-4 text-center text-sm text-muted-foreground">
                {search ? 'No draft matches this search.' : 'No drafts waiting. Write a new post below.'}
              </p>
            ) : (
              <ul className="grid gap-1.5">
                {drafts.map((p) => (
                  <li key={p.id}>
                    <button
                      type="button"
                      onClick={() => pick(p)}
                      disabled={setSchedule.isPending}
                      className="grid w-full grid-cols-[minmax(0,1fr)_auto] items-center gap-x-3 rounded-lg border bg-card px-3 py-2 text-left text-sm transition-colors hover:bg-muted"
                    >
                      <span className="min-w-0">
                        <span className="block truncate font-medium">{postTitle(p)}</span>
                        <span className="block truncate text-xs text-muted-foreground">
                          {p.collection_name}
                          {(p.targets?.length ?? 0) === 0 && ' · no channels yet'}
                        </span>
                      </span>
                      <ChannelStack
                        channels={(p.targets ?? []).map((t) => ({
                          id: t.channel_id,
                          provider: t.provider,
                          name: t.channel_name,
                          avatar_url: t.avatar_url,
                        }))}
                        size="size-6"
                      />
                    </button>
                  </li>
                ))}
              </ul>
            )}
          </div>
          <div className="flex justify-end border-t pt-3">
            <Button variant="outline" onClick={() => setComposing(true)}>
              <PenSquare /> New post for this time
            </Button>
          </div>
        </div>
      </DialogContent>
    </Dialog>
  )
}
