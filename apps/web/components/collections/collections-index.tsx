'use client'

import { Archive, FolderOpen, Plus } from 'lucide-react'
import dynamic from 'next/dynamic'
import Link from 'next/link'
import { usePathname, useRouter, useSearchParams } from 'next/navigation'
import { useState } from 'react'

import { EmptyState, ErrorState } from '@/components/common/error-state'
import { PageContainer, PageHeader } from '@/components/common/page-header'
import { SkeletonList } from '@/components/common/skeleton-list'
import { Button } from '@/components/ui/button'
import { Label } from '@/components/ui/label'
import { Switch } from '@/components/ui/switch'
import { mediaUrl, type Collection, type ItemStatus } from '@/lib/api'
import { pluralize, timeAgo } from '@/lib/format'
import { useCollections } from '@/lib/queries'
import { STATUS_META } from '@/lib/status'
import { cn } from '@/lib/utils'

// zod + react-hook-form load with the dialog, on first open.
const CreateCollectionDialog = dynamic(
  () => import('./create-collection-dialog').then((m) => m.CreateCollectionDialog),
  {
    ssr: false,
  },
)

const COUNT_ORDER: ItemStatus[] = [
  'pending',
  'approved',
  'rejected',
  'expired',
  'processing',
  'completed',
  'failed',
  'draft',
  'archived',
]

export function coverSrc(c: Collection): string | null {
  if (c.cover?.media_id) {
    return mediaUrl(c.cover.media_id)
  }

  if (c.cover?.url) {
    return c.cover.url
  }

  return null
}

function CoverImage({ collection }: { collection: Collection }) {
  const src = coverSrc(collection)
  const [broken, setBroken] = useState(false)

  if (!src || broken) {
    return (
      <div className="bg-grid flex h-full w-full items-center justify-center bg-muted/60">
        <FolderOpen className="size-8 text-muted-foreground/50" />
      </div>
    )
  }

  return (
    // oxlint-disable-next-line nextjs/no-img-element -- authenticated media served via the API proxy
    <img
      src={src}
      alt=""
      loading="lazy"
      onError={() => setBroken(true)}
      className="h-full w-full object-cover transition-transform duration-300 group-hover:scale-[1.03]"
    />
  )
}

export function StatusCounts({ counts, className }: { counts: Record<string, number>; className?: string }) {
  const entries = COUNT_ORDER.filter((s) => (counts[s] ?? 0) > 0)

  if (!entries.length) {
    return <span className={cn('text-xs text-muted-foreground', className)}>Empty</span>
  }

  return (
    <div className={cn('flex flex-wrap gap-x-3 gap-y-1 text-xs', className)}>
      {entries.map((s) => (
        <span key={s} className="inline-flex items-center gap-1.5 text-muted-foreground" title={STATUS_META[s].label}>
          <span className={cn('size-1.5 rounded-full', STATUS_META[s].dot)} />
          <span className="text-foreground tabular-nums">{counts[s]}</span> {STATUS_META[s].label.toLowerCase()}
        </span>
      ))}
    </div>
  )
}

export function CollectionCard({ collection: c }: { collection: Collection }) {
  return (
    <Link
      href={`/collections/${c.id}`}
      data-testid="collection-card"
      className={cn(
        'group flex flex-col overflow-hidden rounded-xl border bg-card shadow-xs transition-all hover:-translate-y-0.5 hover:border-foreground/20 hover:shadow-md',
        c.archived && 'opacity-70',
      )}
    >
      <div className="relative aspect-[16/9] overflow-hidden border-b max-sm:aspect-[3/1]">
        <CoverImage key={coverSrc(c) ?? 'none'} collection={c} />
        {(c.status_counts?.pending ?? 0) > 0 && (
          <span className="absolute top-2 right-2 rounded-full bg-amber-500 px-2 py-0.5 text-[11px] font-semibold text-white shadow">
            {c.status_counts.pending} to review
          </span>
        )}
        {c.archived && (
          <span className="absolute top-2 left-2 inline-flex items-center gap-1 rounded-full bg-background/90 px-2 py-0.5 text-[11px] font-medium">
            <Archive className="size-3" /> Archived
          </span>
        )}
      </div>
      <div className="flex flex-1 flex-col gap-2 p-3.5">
        <div className="flex items-baseline justify-between gap-2">
          <h3 className="truncate font-medium">{c.name}</h3>
          <span className="shrink-0 text-xs text-muted-foreground tabular-nums">{pluralize(c.item_count, 'item')}</span>
        </div>
        {c.description ? (
          <p className="line-clamp-2 text-sm text-muted-foreground">{c.description}</p>
        ) : (
          <p className="text-sm text-muted-foreground/60 italic">No description</p>
        )}
        <StatusCounts counts={c.status_counts ?? {}} className="mt-auto pt-1" />
        <div className="text-[11px] text-muted-foreground/70">Updated {timeAgo(c.updated_at)}</div>
      </div>
    </Link>
  )
}

export function CollectionsIndex() {
  const params = useSearchParams()
  const router = useRouter()
  const pathname = usePathname()
  const showArchived = params.get('archived') === '1'
  const [create, setCreate] = useState(() => ({ open: params.get('new') === '1', used: params.get('new') === '1' }))
  const setCreateOpen = (open: boolean) => setCreate({ open, used: true })
  const collections = useCollections(showArchived)

  function toggleArchived(on: boolean) {
    router.replace(on ? `${pathname}?archived=1` : pathname, { scroll: false })
  }

  return (
    <PageContainer>
      <PageHeader
        title="Collections"
        description="Where your agents put their work. Each collection has its own review actions."
        actions={
          <>
            <div className="flex items-center gap-2">
              <Switch id="show-archived" checked={showArchived} onCheckedChange={toggleArchived} />
              <Label htmlFor="show-archived" className="text-sm font-normal text-muted-foreground">
                Show archived
              </Label>
            </div>
            <Button onClick={() => setCreateOpen(true)}>
              <Plus /> New collection
            </Button>
          </>
        }
      />
      <div className="mt-6">
        {collections.isLoading ? (
          <div className="grid gap-4 sm:grid-cols-2 lg:grid-cols-3">
            <SkeletonList count={6} className="h-64 rounded-xl" />
          </div>
        ) : collections.error ? (
          <ErrorState error={collections.error} onRetry={() => collections.refetch()} />
        ) : (collections.data ?? []).length === 0 ? (
          <EmptyState
            icon={<FolderOpen />}
            title="No collections yet"
            description="Create a collection, then point an agent at it by ID or name."
            action={
              <Button onClick={() => setCreateOpen(true)}>
                <Plus /> New collection
              </Button>
            }
          />
        ) : (
          <div className="grid gap-4 sm:grid-cols-2 lg:grid-cols-3">
            {collections.data!.map((c) => (
              <CollectionCard key={c.id} collection={c} />
            ))}
          </div>
        )}
      </div>
      {create.used && <CreateCollectionDialog open={create.open} onOpenChange={setCreateOpen} />}
    </PageContainer>
  )
}
