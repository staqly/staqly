'use client'

import { Archive, FolderOpen, Plus } from 'lucide-react'
import dynamic from 'next/dynamic'
import Link from 'next/link'
import { usePathname, useRouter, useSearchParams } from 'next/navigation'
import { useMemo, useRef, useState } from 'react'

import { EmptyState, ErrorState } from '@/components/common/error-state'
import { PageContainer } from '@/components/common/page-header'
import { SkeletonList } from '@/components/common/skeleton-list'
import { Button } from '@/components/ui/button'
import { Label } from '@/components/ui/label'
import { Switch } from '@/components/ui/switch'
import { useRouteId } from '@/hooks/use-route-id'
import { useScrollFade } from '@/hooks/use-scroll-fade'
import type { Collection } from '@/lib/api'
import { useCollections, usePost } from '@/lib/queries'
import { safeCssColor } from '@/lib/safe'
import { TONES } from '@/lib/tones'
import { cn } from '@/lib/utils'

import { CollectionPosts } from './collection-posts'

// zod + react-hook-form load with the drawer, on first open.
const CreateCollectionDialog = dynamic(
  () => import('@/components/collections/create-collection-dialog').then((m) => m.CreateCollectionDialog),
  { ssr: false },
)

function Dot({ color, className }: { color?: string; className?: string }) {
  return (
    <span
      aria-hidden
      className={cn('size-2.5 shrink-0 rounded-full bg-muted-foreground/40', className)}
      style={color ? { background: safeCssColor(color) } : undefined}
    />
  )
}

/** How many posts of a collection are scheduled, as a small count next to its name. */
function ScheduledBadge({ n, className }: { n: number; className?: string }) {
  return n > 0 ? (
    <span
      className={cn('shrink-0 rounded-full px-1.5 text-[11px] font-semibold tabular-nums', TONES.info.badge, className)}
      title={`${n} scheduled`}
    >
      {n}
    </span>
  ) : null
}

const hrefOf = (id: string, archived: boolean) => `/posts?collection=${id}${archived ? '&archived=1' : ''}`

/** One collection in the pane: colour, name, archived mark and how many posts wait. */
function CollectionRow({ c, active, href }: { c: Collection; active: boolean; href: string }) {
  return (
    <li>
      <Link
        prefetch={false}
        href={href}
        aria-current={active ? 'page' : undefined}
        title={c.name}
        data-testid="collection-row"
        className={cn(
          'flex min-h-9 items-center gap-2 rounded-lg px-2 py-1.5 text-sm transition-colors hover:bg-muted pointer-coarse:min-h-10',
          active && 'bg-muted font-medium',
          c.archived && 'text-muted-foreground',
        )}
      >
        <Dot color={c.color} />
        <span className="min-w-0 flex-1 truncate">{c.name}</span>
        {c.archived && <Archive className="size-3.5 shrink-0 text-muted-foreground" aria-label="Archived" />}
        <ScheduledBadge n={c.status_counts?.scheduled ?? 0} />
      </Link>
    </li>
  )
}

/** The collections pane: every collection, the open one marked, archived ones behind a switch. */
function CollectionsPane({
  collections,
  isLoading,
  selectedId,
  showArchived,
  onToggleArchived,
  onCreate,
}: {
  collections: Collection[]
  isLoading: boolean
  selectedId?: string
  showArchived: boolean
  onToggleArchived: (on: boolean) => void
  onCreate: () => void
}) {
  return (
    <nav
      aria-label="Collections"
      data-testid="collections-pane"
      className="hidden min-h-0 flex-col border-r bg-sidebar/40 md:sticky md:top-0 md:flex md:h-svh md:w-75 md:shrink-0"
    >
      <div className="flex items-center justify-between gap-2 border-b px-3 py-3">
        {/* The page's h1 is the open collection's name; this pane is a landmark with its own heading. */}
        <h2 className="flex items-center gap-2 text-sm font-semibold">
          <FolderOpen className="size-4 text-muted-foreground" aria-hidden /> Collections
        </h2>
        <Button size="sm" variant="outline" onClick={onCreate}>
          <Plus /> New
        </Button>
      </div>
      <div className="min-h-0 flex-1 overflow-y-auto overscroll-contain p-2">
        {isLoading ? (
          <div className="grid gap-1.5">
            <SkeletonList count={6} className="h-9 rounded-lg" />
          </div>
        ) : collections.length === 0 ? (
          <p className="px-2 py-3 text-xs text-muted-foreground">No collections yet.</p>
        ) : (
          (() => {
            const archived = collections.filter((c) => c.archived)
            const current = collections.filter((c) => !c.archived)
            const heading = 'px-2 pt-2 pb-1 text-[11px] font-medium tracking-wide text-muted-foreground uppercase'

            // Archived collections get their own section, above the active ones, while "Show archived" is on.
            return archived.length === 0 ? (
              <ul className="grid gap-0.5" data-testid="collections-list">
                {current.map((c) => (
                  <CollectionRow key={c.id} c={c} active={c.id === selectedId} href={hrefOf(c.id, showArchived)} />
                ))}
              </ul>
            ) : (
              <div className="grid gap-3">
                <section aria-labelledby="archived-collections-heading">
                  <h3 id="archived-collections-heading" className={heading}>
                    Archived
                  </h3>
                  <ul className="grid gap-0.5" data-testid="archived-collections">
                    {archived.map((c) => (
                      <CollectionRow key={c.id} c={c} active={c.id === selectedId} href={hrefOf(c.id, showArchived)} />
                    ))}
                  </ul>
                </section>
                <section aria-labelledby="active-collections-heading">
                  <h3 id="active-collections-heading" className={heading}>
                    Active
                  </h3>
                  <ul className="grid gap-0.5" data-testid="collections-list">
                    {current.map((c) => (
                      <CollectionRow key={c.id} c={c} active={c.id === selectedId} href={hrefOf(c.id, showArchived)} />
                    ))}
                  </ul>
                </section>
              </div>
            )
          })()
        )}
      </div>
      <div className="flex items-center gap-2 border-t px-3 py-2.5">
        <Switch id="show-archived" checked={showArchived} onCheckedChange={onToggleArchived} />
        <Label htmlFor="show-archived" className="text-xs font-normal text-muted-foreground">
          Show archived
        </Label>
      </div>
    </nav>
  )
}

/** Phones: the collections as a strip of chips over the open one. */
function CollectionsStrip({
  collections,
  selectedId,
  showArchived,
  onCreate,
}: {
  collections: Collection[]
  selectedId?: string
  showArchived: boolean
  onCreate: () => void
}) {
  const ref = useRef<HTMLDivElement>(null)
  const fade = useScrollFade(ref)

  return (
    <nav aria-label="Collections" className="border-b md:hidden" data-testid="collections-strip">
      <div
        ref={ref}
        data-fade={fade.fade ?? undefined}
        className={cn('scrollbar-none flex items-center gap-1.5 overflow-x-auto px-4 py-2', fade.className)}
      >
        <button
          type="button"
          onClick={onCreate}
          aria-label="New collection"
          className="flex size-9 shrink-0 items-center justify-center rounded-full border text-muted-foreground pointer-coarse:size-10"
        >
          <Plus className="size-4" />
        </button>
        {collections.map((c) => {
          const active = c.id === selectedId

          return (
            <Link
              key={c.id}
              prefetch={false}
              href={hrefOf(c.id, showArchived)}
              aria-current={active ? 'page' : undefined}
              className={cn(
                'inline-flex h-9 max-w-48 shrink-0 items-center gap-1.5 rounded-full border px-3 text-sm whitespace-nowrap pointer-coarse:h-10',
                active ? 'border-foreground bg-foreground text-background' : 'bg-background text-foreground',
              )}
            >
              <Dot color={c.color} className={cn(active && 'ring-1 ring-background')} />
              <span className="truncate">{c.name}</span>
              <ScheduledBadge n={c.status_counts?.scheduled ?? 0} />
            </Link>
          )
        })}
      </div>
    </nav>
  )
}

const countAll = (cols: Collection[]) => cols.length

/**
 * Posts by collection in one view: the collections on the left (a strip of chips on phones), the open
 * collection's posts on the right. /posts shows the first collection; /posts?collection=<id> a chosen one
 * (deep links keep working). It is the posts layout, so it stays mounted while a post's sheet opens over it
 * (/posts/<id>); a post opened without ?collection= (a bookmark, a link from elsewhere) shows its own collection.
 */
export function PostsWorkspace() {
  const params = useSearchParams()
  const router = useRouter()
  const selectedId = params.get('collection') ?? undefined
  const pathname = usePathname()
  const onPost = !selectedId && pathname.startsWith('/posts/')
  const routeId = useRouteId()
  // Fetched together with the collections (the sheet reads the same query): its collection opens behind it.
  const openPost = usePost(onPost && routeId ? routeId : '')
  // Until the post says which collection it is in, nothing opens (no list of another collection is loaded).
  const waiting = onPost && (!routeId || openPost.isLoading)
  const showArchived = params.get('archived') === '1'
  const [create, setCreate] = useState(() => ({ open: params.get('new') === '1', used: params.get('new') === '1' }))
  const setCreateOpen = (open: boolean) => setCreate({ open, used: true })
  const collections = useCollections(showArchived)
  // One collection (archived ones count) is the usual case: no pane or strip until there is a second one.
  const single = useCollections(true, countAll).data === 1
  const list = useMemo(() => collections.data ?? [], [collections.data])
  const current = waiting
    ? undefined
    : (selectedId ??
      (onPost ? openPost.data?.collection_id : undefined) ??
      list.find((c) => !c.archived)?.id ??
      list[0]?.id)

  function toggleArchived(on: boolean) {
    router.replace(selectedId ? hrefOf(selectedId, on) : on ? '/posts?archived=1' : '/posts', { scroll: false })
  }

  const openCreate = () => setCreateOpen(true)
  const pane = (
    <CollectionsPane
      collections={list}
      isLoading={collections.isLoading}
      selectedId={current}
      showArchived={showArchived}
      onToggleArchived={toggleArchived}
      onCreate={openCreate}
    />
  )

  let content: React.ReactNode

  if (collections.isLoading || waiting) {
    content = (
      <PageContainer>
        <SkeletonList count={6} className="h-10 rounded-lg" />
      </PageContainer>
    )
  } else if (collections.error) {
    content = (
      <PageContainer>
        <ErrorState error={collections.error} onRetry={() => collections.refetch()} />
      </PageContainer>
    )
  } else if (!current) {
    content = (
      <PageContainer>
        <EmptyState
          className="pt-[12vh]"
          icon={<FolderOpen />}
          title="No collections yet"
          description="A collection groups posts (a brand, a client, a campaign) and holds their publishing schedules."
          action={
            <Button onClick={openCreate}>
              <Plus /> New collection
            </Button>
          }
        />
      </PageContainer>
    )
  } else {
    content = <CollectionPosts key={current} id={current} onNewCollection={single ? openCreate : undefined} />
  }

  return (
    <div className="flex min-h-0 flex-1 flex-col md:flex-row" data-testid="posts-workspace">
      {!single && pane}
      <div className="flex min-w-0 flex-1 flex-col">
        {!single && !collections.isLoading && list.length > 0 && (
          <CollectionsStrip collections={list} selectedId={current} showArchived={showArchived} onCreate={openCreate} />
        )}
        {content}
      </div>
      {create.used && <CreateCollectionDialog open={create.open} onOpenChange={setCreateOpen} />}
    </div>
  )
}
