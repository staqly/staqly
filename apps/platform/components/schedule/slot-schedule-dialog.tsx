'use client'

import { FileText, Search } from 'lucide-react'
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

// The composer (large) loads only when a slot's "+" is used.
const Composer = dynamic(() => import('@/components/posts/composer').then((m) => m.Composer), { ssr: false })

/**
 * "+" on a future slot of the calendar: the composer with that time. When drafts (posts without a time) are waiting,
 * **From drafts** loads one into it instead, keeping the slot's time.
 */
export function SlotScheduleDialog({ at, onOpenChange }: { at: Date; onOpenChange: (open: boolean) => void }) {
  const [search, setSearch] = useState('')
  const [picking, setPicking] = useState(false)
  const [draft, setDraft] = useState<Post>()
  const query: PostQuery = useMemo(() => ({ status: 'draft', sort: 'created_desc', limit: 100 }), [])
  const list = usePostsInfinite(query, { refetchOnWindowFocus: false })
  // A draft with a file in Deleted can't be scheduled until the file is restored: it isn't offered here.
  const all = useMemo(
    () =>
      flattenPosts(list.data).filter(
        (p) =>
          !p.publish_at && ![...p.media_items, ...p.thread.flatMap((t) => t.media_items)].some((m) => m.deleted_at),
      ),
    [list.data],
  )
  const drafts = useMemo(() => {
    const q = search.trim().toLowerCase()

    return q ? all.filter((p) => p.content.toLowerCase().includes(q)) : all
  }, [all, search])

  // shortcut: loading a draft replaces what was typed in the new post without asking; ask first if people lose text.
  const fromDrafts = !draft && all.length > 0 && (
    <Button type="button" size="sm" variant="outline" onClick={() => setPicking(true)}>
      <FileText /> From drafts
    </Button>
  )

  const picker = picking && (
    <Dialog open onOpenChange={setPicking}>
      <DialogContent className="sm:max-w-lg">
        <DialogHeader>
          <DialogTitle>Load a draft</DialogTitle>
          <DialogDescription>It opens in the composer, set for {formatPicked(at)}.</DialogDescription>
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
              <p className="py-4 text-center text-sm text-muted-foreground">No draft matches this search.</p>
            ) : (
              <ul className="grid gap-1.5">
                {drafts.map((p) => (
                  <li key={p.id}>
                    <button
                      type="button"
                      onClick={() => {
                        setDraft(p)
                        setPicking(false)
                      }}
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
        </div>
      </DialogContent>
    </Dialog>
  )

  return (
    <>
      <Composer
        open
        onOpenChange={(o) => {
          if (!o) {
            onOpenChange(false)
          }
        }}
        post={draft}
        defaultPublishAt={at.toISOString()}
        headerActions={fromDrafts || undefined}
      />
      {picker}
    </>
  )
}
