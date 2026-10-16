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
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from '@/components/ui/table'
import { type Collection, type ItemStatus } from '@/lib/api'
import { pluralize, timeAgo } from '@/lib/format'
import { useCollections } from '@/lib/queries'
import { safeCssColor } from '@/lib/safe'
import { STATUS_META } from '@/lib/status'
import { TONES } from '@/lib/tones'
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

export function StatusCounts({
  counts,
  className,
  hidePending,
}: {
  counts: Record<string, number>
  className?: string
  /** Skip "pending" when a "to review" badge already shows it. */
  hidePending?: boolean
}) {
  const entries = COUNT_ORDER.filter((s) => (counts[s] ?? 0) > 0 && !(hidePending && s === 'pending'))

  if (!entries.length) {
    if (hidePending && (counts.pending ?? 0) > 0) {
      return null
    }

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

function Dot({ color }: { color?: string }) {
  return (
    <span
      aria-hidden
      className="size-2.5 shrink-0 rounded-full bg-muted-foreground/40"
      style={color ? { background: safeCssColor(color) } : undefined}
    />
  )
}

function ToReview({ n }: { n: number }) {
  return n > 0 ? (
    <span className={cn('rounded-full px-2 py-0.5 text-[11px] font-semibold', TONES.warning.badge)}>{n} to review</span>
  ) : null
}

/** One row per collection; the whole row opens it. */
function CollectionsTable({ collections }: { collections: Collection[] }) {
  return (
    <>
      <ul className="grid gap-2 sm:hidden" data-testid="collections-list">
        {collections.map((c) => (
          <li key={c.id}>
            <Link
              href={`/collections/${c.id}`}
              data-testid="collection-row"
              className={cn(
                'grid gap-1.5 rounded-xl border bg-card p-3 active:bg-muted/60',
                c.archived && 'opacity-70',
              )}
            >
              <span className="flex items-center gap-2">
                <Dot color={c.color} />
                <span className="truncate font-medium">{c.name}</span>
                {c.is_default && <DefaultBadge />}
                <span className="ml-auto shrink-0 text-xs text-muted-foreground tabular-nums">
                  {pluralize(c.item_count, 'item')}
                </span>
              </span>
              <span className="flex flex-wrap items-center gap-2">
                <ToReview n={c.status_counts?.pending ?? 0} />
                <StatusCounts counts={c.status_counts ?? {}} hidePending />
              </span>
            </Link>
          </li>
        ))}
      </ul>
      <div className="hidden overflow-hidden rounded-xl border bg-card sm:block">
        <Table>
          <TableHeader>
            <TableRow className="hover:bg-transparent">
              {/* No status breakdown here: the inbox steps show where items are. */}
              <TableHead>Name</TableHead>
              <TableHead className="w-24 text-center">Items</TableHead>
              <TableHead className="w-32 text-right">Updated</TableHead>
            </TableRow>
          </TableHeader>
          <TableBody>
            {collections.map((c) => (
              <TableRow key={c.id} className={cn('relative', c.archived && 'opacity-70')} data-testid="collection-row">
                <TableCell className="max-w-0 py-2.5">
                  <Link
                    href={`/collections/${c.id}`}
                    className="flex min-w-0 items-center gap-2.5 font-medium after:absolute after:inset-0 after:content-['']"
                  >
                    <Dot color={c.color} />
                    <span className="grid min-w-0">
                      <span className="flex min-w-0 items-center gap-2">
                        <span className="truncate">{c.name}</span>
                        {c.is_default && <DefaultBadge />}
                      </span>
                      {/* Always two lines, so every row has the same height. */}
                      <span className="truncate text-xs font-normal text-muted-foreground">
                        {c.description || <span className="opacity-60">No description</span>}
                      </span>
                    </span>
                    {/* Pinned to the right edge of the column, so archived rows line up. */}
                    {c.archived && (
                      <span
                        className={cn(
                          'ml-auto inline-flex shrink-0 items-center gap-1 rounded-md px-1.5 py-0.5 text-[11px] font-normal',
                          TONES.neutral.badge,
                        )}
                      >
                        <Archive className="size-3" /> Archived
                      </span>
                    )}
                  </Link>
                </TableCell>
                <TableCell className="text-center text-muted-foreground tabular-nums">{c.item_count}</TableCell>
                <TableCell className="text-right text-xs text-muted-foreground" title={c.updated_at}>
                  {timeAgo(c.updated_at)}
                </TableCell>
              </TableRow>
            ))}
          </TableBody>
        </Table>
      </div>
    </>
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
        icon={<FolderOpen className="size-4" />}
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
          <div className="grid gap-2">
            <SkeletonList count={5} className="h-12 rounded-lg" />
          </div>
        ) : collections.error ? (
          <ErrorState error={collections.error} onRetry={() => collections.refetch()} />
        ) : (collections.data ?? []).length === 0 ? (
          <EmptyState
            className="pt-[12vh]"
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
          <CollectionsTable collections={collections.data!} />
        )}
      </div>
      {create.used && <CreateCollectionDialog open={create.open} onOpenChange={setCreateOpen} />}
    </PageContainer>
  )
}

function DefaultBadge() {
  return (
    <span className="shrink-0 rounded bg-muted px-1.5 py-px text-[11px] font-normal text-muted-foreground">
      Default
    </span>
  )
}
