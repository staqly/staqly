'use client'

import { Archive, FolderOpen, Plus, Search, Settings2, SlidersHorizontal, X } from 'lucide-react'
import { usePathname, useRouter, useSearchParams } from 'next/navigation'
import { useCallback, useEffect, useMemo, useRef, useState } from 'react'

import { CopyButton } from '@/components/common/copy-button'
import { EmptyState, ErrorState } from '@/components/common/error-state'
import { PageContainer } from '@/components/common/page-header'
import { SkeletonList } from '@/components/common/skeleton-list'
import { TYPE_LABELS } from '@/components/common/type-icon'
import { ItemDetail } from '@/components/items/item-detail'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from '@/components/ui/select'
import { Sheet, SheetContent, SheetDescription, SheetHeader, SheetTitle } from '@/components/ui/sheet'
import { Skeleton } from '@/components/ui/skeleton'
import { ToggleGroup, ToggleGroupItem } from '@/components/ui/toggle-group'
import { useDebounced } from '@/hooks/use-debounced'
import { useIsMobile } from '@/hooks/use-mobile'
import { ApiError, ITEM_STATUSES, ITEM_TYPES, type CollectionView, type Item, type ItemQuery } from '@/lib/api'
import { pluralize } from '@/lib/format'
import { flattenItems, useCollection, useItemsInfinite, useRefetchOnForeignChange } from '@/lib/queries'
import { STATUS_META } from '@/lib/status'

import { AddItemDialog } from './add-item-dialog'
import { CollectionSettingsSheet } from './collection-settings'
import { StatusCounts } from './collections-index'
import { VIEW_OPTIONS } from './create-collection-dialog'
import { GalleryView, ListView, TableView } from './item-views'

const SORTS = [
  { value: 'relevance', label: 'Best match' },
  { value: 'created_desc', label: 'Newest first' },
  { value: 'created_asc', label: 'Oldest first' },
  { value: 'updated_desc', label: 'Recently updated' },
  { value: 'decided_desc', label: 'Recently decided' },
  { value: 'title_asc', label: 'Title A–Z' },
] as const

type Sort = (typeof SORTS)[number]['value']

export function CollectionPage({ id }: { id: string }) {
  const params = useSearchParams()
  const router = useRouter()
  const pathname = usePathname()
  const collection = useCollection(id)

  const view = (params.get('view') as CollectionView | null) ?? collection.data?.default_view ?? 'gallery'
  const status = (params.get('status') as ItemQuery['status']) || undefined
  const type = (params.get('type') as ItemQuery['type']) || undefined
  const urlSort = params.get('sort') as Sort | null
  const openItem = params.get('item')
  const [q, setQ] = useState(params.get('q') ?? '')
  const debouncedQ = useDebounced(q.trim(), 300)
  // Full-text search ranks by relevance by default; without a query relevance means nothing.
  const defaultSort: Sort = debouncedQ ? 'relevance' : 'created_desc'
  const sort: Sort = urlSort && (urlSort !== 'relevance' || debouncedQ) ? urlSort : defaultSort
  const sortOptions = SORTS.filter((s) => s.value !== 'relevance' || debouncedQ)
  const isMobile = useIsMobile()
  const [filtersOpen, setFiltersOpen] = useState(false)
  const [addOpen, setAddOpen] = useState(false)
  const [settingsOpen, setSettingsOpen] = useState(false)

  function setParams(patch: Record<string, string | undefined>) {
    const sp = new URLSearchParams(params.toString())

    for (const [k, v] of Object.entries(patch)) {
      if (v) {
        sp.set(k, v)
      } else {
        sp.delete(k)
      }
    }

    const s = sp.toString()

    router.replace(`${pathname}${s ? `?${s}` : ''}`, { scroll: false })
  }

  // Mirror the debounced search box into the URL.
  const urlQ = params.get('q') ?? ''

  useEffect(() => {
    if (debouncedQ !== urlQ) {
      setParams({ q: debouncedQ || undefined })
    }
    // oxlint-disable-next-line react/exhaustive-deps -- only react to the debounced value
  }, [debouncedQ])

  const query: ItemQuery = useMemo(
    () => ({ collection_id: id, status, type, q: debouncedQ || undefined, sort, limit: 48 }) satisfies ItemQuery,
    [id, status, type, debouncedQ, sort],
  )
  // Like the inbox: no refetch of every loaded page on window focus; refetch only when another tab, agent or
  // rule changed something (useRefetchOnForeignChange). Revisits start from the first page (useItemsInfinite).
  const items = useItemsInfinite(query, { enabled: !!collection.data, refetchOnWindowFocus: false })

  useRefetchOnForeignChange(items.refetch)
  const list = useMemo(() => flattenItems(items.data), [items.data])
  const filtered = !!(status || type || debouncedQ)
  const activeFilters = (status ? 1 : 0) + (type ? 1 : 0) + (urlSort && urlSort !== defaultSort ? 1 : 0)

  // Every card gets this callback, so it must not change when the URL does (opening an item sets ?item=,
  // which would otherwise re-render all loaded cards). The latest params are read from a ref.
  const latestParams = useRef(params)

  useEffect(() => {
    latestParams.current = params
  }, [params])

  const open = useCallback(
    (it: Item) => {
      const sp = new URLSearchParams(latestParams.current.toString())

      sp.set('item', it.id)
      router.replace(`${pathname}?${sp}`, { scroll: false })
    },
    [router, pathname],
  )

  if (collection.isLoading) {
    return (
      <PageContainer className="max-w-7xl">
        <Skeleton className="h-10 w-72" />
        <Skeleton className="mt-6 h-96" />
      </PageContainer>
    )
  }

  if (collection.error) {
    const nf = collection.error instanceof ApiError && collection.error.status === 404

    return (
      <PageContainer>
        <ErrorState
          title={nf ? 'Collection not found' : 'Could not load collection'}
          error={collection.error}
          onRetry={nf ? undefined : () => collection.refetch()}
        />
      </PageContainer>
    )
  }

  const c = collection.data!

  return (
    <PageContainer className="max-w-7xl">
      <div className="flex items-start justify-between gap-3">
        <div className="min-w-0">
          <div className="flex items-center gap-2">
            <h1 className="truncate text-xl font-semibold tracking-tight">{c.name}</h1>
            {c.archived && (
              <span className="inline-flex items-center gap-1 rounded-md bg-muted px-1.5 py-0.5 text-xs text-muted-foreground">
                <Archive className="size-3" /> Archived
              </span>
            )}
          </div>
          {c.description && <p className="mt-0.5 max-w-2xl text-sm text-muted-foreground">{c.description}</p>}
          <div className="mt-2 flex flex-wrap items-center gap-3">
            <span className="text-xs text-muted-foreground">{pluralize(c.item_count, 'item')}</span>
            <StatusCounts counts={c.status_counts ?? {}} />
            <span className="hidden items-center gap-0.5 font-mono text-[11px] text-muted-foreground sm:inline-flex">
              {c.id} <CopyButton value={c.id} size="icon-xs" label="Copy collection ID" />
            </span>
          </div>
        </div>
        <div className="flex shrink-0 gap-2">
          <Button
            variant="outline"
            onClick={() => setSettingsOpen(true)}
            aria-label="Collection settings"
            className="max-sm:w-11 max-sm:px-0"
          >
            <Settings2 /> <span className="max-sm:sr-only">Settings</span>
          </Button>
          <Button
            onClick={() => setAddOpen(true)}
            disabled={c.archived}
            title={c.archived ? 'Archived collections are read-only' : undefined}
          >
            <Plus /> Add<span className="max-sm:hidden"> item</span>
          </Button>
        </div>
      </div>

      <div className="mt-5 flex flex-wrap items-center gap-2 sm:mt-6">
        <div className="relative min-w-0 flex-1 basis-40 sm:max-w-xs">
          <Search className="pointer-events-none absolute top-1/2 left-2.5 size-3.5 -translate-y-1/2 text-muted-foreground" />
          <Input
            value={q}
            onChange={(e) => setQ(e.target.value)}
            placeholder="Search items…"
            className="h-8 pr-9 pl-8"
            aria-label="Search items"
          />
          {q && (
            <button
              type="button"
              className="absolute top-1/2 right-1 flex size-7 -translate-y-1/2 items-center justify-center text-muted-foreground hover:text-foreground pointer-coarse:size-9"
              onClick={() => setQ('')}
              aria-label="Clear search"
            >
              <X className="size-3.5" />
            </button>
          )}
        </div>
        {isMobile ? (
          <Button variant="outline" onClick={() => setFiltersOpen(true)} aria-label="Filters">
            <SlidersHorizontal />
            {activeFilters > 0 && (
              <span className="flex size-5 items-center justify-center rounded-full bg-brand text-[11px] font-semibold text-brand-foreground">
                {activeFilters}
              </span>
            )}
          </Button>
        ) : (
          <>
            <Select value={status ?? 'all'} onValueChange={(v) => setParams({ status: v === 'all' ? undefined : v })}>
              <SelectTrigger size="sm" className="w-36" aria-label="Status filter">
                <SelectValue />
              </SelectTrigger>
              <SelectContent>
                <SelectItem value="all">All statuses</SelectItem>
                {ITEM_STATUSES.map((s) => (
                  <SelectItem key={s} value={s}>
                    {STATUS_META[s].label}
                  </SelectItem>
                ))}
              </SelectContent>
            </Select>
            <Select value={type ?? 'all'} onValueChange={(v) => setParams({ type: v === 'all' ? undefined : v })}>
              <SelectTrigger size="sm" className="w-32" aria-label="Type filter">
                <SelectValue />
              </SelectTrigger>
              <SelectContent>
                <SelectItem value="all">All types</SelectItem>
                {ITEM_TYPES.map((t) => (
                  <SelectItem key={t} value={t}>
                    {TYPE_LABELS[t]}
                  </SelectItem>
                ))}
              </SelectContent>
            </Select>
            <Select value={sort} onValueChange={(v) => setParams({ sort: v === defaultSort ? undefined : v })}>
              <SelectTrigger size="sm" className="w-40" aria-label="Sort">
                <SelectValue />
              </SelectTrigger>
              <SelectContent>
                {sortOptions.map((s) => (
                  <SelectItem key={s.value} value={s.value}>
                    {s.label}
                  </SelectItem>
                ))}
              </SelectContent>
            </Select>
          </>
        )}
        <ToggleGroup
          type="single"
          size="sm"
          variant="outline"
          className="ml-auto"
          value={view}
          onValueChange={(v) => v && setParams({ view: v })}
          aria-label="View"
        >
          {VIEW_OPTIONS.map(({ value, label, Icon }) => (
            <ToggleGroupItem key={value} value={value} aria-label={`${label} view`} title={`${label} view`}>
              <Icon />
            </ToggleGroupItem>
          ))}
        </ToggleGroup>
      </div>

      <div className="mt-4">
        {items.isLoading ? (
          <div className="grid grid-cols-2 gap-3 sm:grid-cols-3 lg:grid-cols-4">
            <SkeletonList count={8} className="aspect-[4/5] rounded-xl" />
          </div>
        ) : items.error ? (
          <ErrorState error={items.error} onRetry={() => items.refetch()} />
        ) : list.length === 0 ? (
          <EmptyState
            icon={<FolderOpen />}
            title={filtered ? 'No items match these filters' : 'This collection is empty'}
            description={
              filtered
                ? 'Try a different search or clear the filters.'
                : 'Add an item yourself, or let an agent post to this collection by ID or name.'
            }
            action={
              filtered ? (
                <Button
                  variant="outline"
                  size="sm"
                  onClick={() => {
                    setQ('')
                    setParams({ status: undefined, type: undefined, q: undefined })
                  }}
                >
                  Clear filters
                </Button>
              ) : !c.archived ? (
                <Button size="sm" onClick={() => setAddOpen(true)}>
                  <Plus /> Add item
                </Button>
              ) : undefined
            }
          />
        ) : view === 'table' ? (
          <TableView items={list} onOpen={open} />
        ) : view === 'list' ? (
          <ListView items={list} onOpen={open} />
        ) : (
          <GalleryView items={list} onOpen={open} />
        )}
        {items.hasNextPage && (
          <div className="mt-6 flex justify-center">
            <Button variant="outline" onClick={() => items.fetchNextPage()} disabled={items.isFetchingNextPage}>
              {items.isFetchingNextPage ? 'Loading…' : 'Load more'}
            </Button>
          </div>
        )}
      </div>

      <Sheet open={filtersOpen} onOpenChange={setFiltersOpen}>
        <SheetContent side="bottom" className="rounded-t-2xl pb-[calc(1.25rem+env(safe-area-inset-bottom))]">
          <SheetHeader className="pb-0">
            <SheetTitle>Filter & sort</SheetTitle>
            <SheetDescription>{c.name}</SheetDescription>
          </SheetHeader>
          <div className="grid gap-4 px-4">
            <Select value={status ?? 'all'} onValueChange={(v) => setParams({ status: v === 'all' ? undefined : v })}>
              <SelectTrigger className="w-full" aria-label="Status filter">
                <SelectValue />
              </SelectTrigger>
              <SelectContent>
                <SelectItem value="all">All statuses</SelectItem>
                {ITEM_STATUSES.map((s) => (
                  <SelectItem key={s} value={s}>
                    {STATUS_META[s].label}
                  </SelectItem>
                ))}
              </SelectContent>
            </Select>
            <Select value={type ?? 'all'} onValueChange={(v) => setParams({ type: v === 'all' ? undefined : v })}>
              <SelectTrigger className="w-full" aria-label="Type filter">
                <SelectValue />
              </SelectTrigger>
              <SelectContent>
                <SelectItem value="all">All types</SelectItem>
                {ITEM_TYPES.map((t) => (
                  <SelectItem key={t} value={t}>
                    {TYPE_LABELS[t]}
                  </SelectItem>
                ))}
              </SelectContent>
            </Select>
            <Select value={sort} onValueChange={(v) => setParams({ sort: v === defaultSort ? undefined : v })}>
              <SelectTrigger className="w-full" aria-label="Sort">
                <SelectValue />
              </SelectTrigger>
              <SelectContent>
                {sortOptions.map((s) => (
                  <SelectItem key={s.value} value={s.value}>
                    {s.label}
                  </SelectItem>
                ))}
              </SelectContent>
            </Select>
            <Button onClick={() => setFiltersOpen(false)}>Show results</Button>
          </div>
        </SheetContent>
      </Sheet>
      <AddItemDialog collectionId={c.id} open={addOpen} onOpenChange={setAddOpen} />
      <CollectionSettingsSheet collection={c} open={settingsOpen} onOpenChange={setSettingsOpen} />
      <Sheet open={!!openItem} onOpenChange={(o) => !o && setParams({ item: undefined })}>
        <SheetContent className="overflow-y-auto overscroll-contain p-4 pt-[max(1rem,env(safe-area-inset-top))] pb-[max(1.25rem,env(safe-area-inset-bottom))] sm:p-5 data-[side=right]:w-full data-[side=right]:sm:max-w-2xl data-[side=right]:lg:max-w-3xl">
          <SheetHeader className="sr-only">
            <SheetTitle>Item details</SheetTitle>
            <SheetDescription>Preview, edit and review this item</SheetDescription>
          </SheetHeader>
          {openItem && <ItemDetail id={openItem} variant="sheet" onDeleted={() => setParams({ item: undefined })} />}
        </SheetContent>
      </Sheet>
    </PageContainer>
  )
}
