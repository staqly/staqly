'use client'

import { useQuery } from '@tanstack/react-query'
import { LibraryBig, Search } from 'lucide-react'
import { useEffect, useState } from 'react'

import { EmptyState, ErrorState } from '@/components/common/error-state'
import { MediaGrid } from '@/components/media/media-grid'
import { Button } from '@/components/ui/button'
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from '@/components/ui/dialog'
import { Input } from '@/components/ui/input'
import { api, type Media, type MediaLibraryItem } from '@/lib/api'

/** "From library": pick uploaded pictures and videos for a post (or a comment) without uploading again. */
const NONE: string[] = []

export function LibraryPickerButton({ onAdd, exclude = NONE }: { onAdd: (m: Media) => void; exclude?: string[] }) {
  const [open, setOpen] = useState(false)

  return (
    <>
      <Button type="button" variant="outline" onClick={() => setOpen(true)} data-testid="media-from-library">
        <LibraryBig /> From library
      </Button>
      {open && (
        <LibraryPicker
          onClose={() => setOpen(false)}
          exclude={exclude}
          onPick={(items) => {
            items.forEach(onAdd)
            setOpen(false)
          }}
        />
      )}
    </>
  )
}

function LibraryPicker({
  onClose,
  onPick,
  exclude,
}: {
  onClose: () => void
  onPick: (items: MediaLibraryItem[]) => void
  exclude: string[]
}) {
  const [search, setSearch] = useState('')
  const [q, setQ] = useState('')
  const [picked, setPicked] = useState<MediaLibraryItem[]>([])

  useEffect(() => {
    const t = setTimeout(() => setQ(search.trim()), 250)

    return () => clearTimeout(t)
  }, [search])

  // Pictures and videos only: what a post can carry.
  const images = useQuery({
    queryKey: ['media', 'picker', 'image', q],
    queryFn: () => api.mediaLibrary({ q: q || undefined, kind: 'image', limit: 120 }),
  })
  const videos = useQuery({
    queryKey: ['media', 'picker', 'video', q],
    queryFn: () => api.mediaLibrary({ q: q || undefined, kind: 'video', limit: 120 }),
  })
  const items = [...(images.data?.data ?? []), ...(videos.data?.data ?? [])]
    .filter((m) => !exclude.includes(m.id))
    .toSorted((a, b) => b.created_at.localeCompare(a.created_at))
  const selected = new Set(picked.map((m) => m.id))
  const toggle = (m: MediaLibraryItem) =>
    setPicked((xs) => (xs.some((x) => x.id === m.id) ? xs.filter((x) => x.id !== m.id) : [...xs, m]))
  const error = images.error ?? videos.error

  return (
    <Dialog open onOpenChange={(o) => !o && onClose()}>
      <DialogContent className="flex max-h-[85svh] flex-col sm:max-w-3xl" data-testid="media-picker">
        <DialogHeader>
          <DialogTitle>Media library</DialogTitle>
          <DialogDescription>Pick pictures and videos you uploaded before.</DialogDescription>
        </DialogHeader>
        <div className="relative">
          <Search className="pointer-events-none absolute top-1/2 left-2.5 size-3.5 -translate-y-1/2 text-muted-foreground" />
          <Input
            value={search}
            onChange={(e) => setSearch(e.target.value)}
            placeholder="Search files"
            aria-label="Search files"
            className="h-8 pl-8"
          />
        </div>
        <div className="min-h-0 flex-1 overflow-y-auto">
          {images.isLoading || videos.isLoading ? (
            <p className="py-8 text-center text-sm text-muted-foreground">Loading</p>
          ) : error ? (
            <ErrorState error={error} onRetry={() => void images.refetch().then(() => videos.refetch())} />
          ) : items.length === 0 ? (
            <EmptyState
              icon={<LibraryBig />}
              title={q ? 'No files match' : 'Nothing in the library yet'}
              description={
                q ? 'Try another name.' : 'Upload pictures and videos on the Media page or with Insert media.'
              }
            />
          ) : (
            <MediaGrid
              items={items}
              selected={selected}
              onOpen={toggle}
              onToggle={toggle}
              className="grid-cols-2 sm:grid-cols-3 md:grid-cols-4 xl:grid-cols-4"
            />
          )}
        </div>
        <DialogFooter>
          <Button type="button" variant="ghost" onClick={onClose}>
            Cancel
          </Button>
          <Button type="button" disabled={!picked.length} onClick={() => onPick(picked)} data-testid="media-picker-add">
            {picked.length ? `Add ${picked.length} ${picked.length === 1 ? 'file' : 'files'}` : 'Add'}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  )
}
