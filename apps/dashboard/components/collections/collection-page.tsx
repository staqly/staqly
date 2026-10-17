'use client'

import { Archive, FolderOpen, Plus, Search, Settings2, SlidersHorizontal, X } from 'lucide-react'
import dynamic from 'next/dynamic'
import { usePathname, useRouter, useSearchParams } from 'next/navigation'
import { useCallback, useEffect, useEffectEvent, useMemo, useRef, useState } from 'react'

import { EmptyState, ErrorState } from '@/components/common/error-state'
import { IdChip } from '@/components/common/id-chip'
import { MultiSelect } from '@/components/common/multi-select'
import { PageContainer } from '@/components/common/page-header'
import { SkeletonList } from '@/components/common/skeleton-list'
import { TYPE_LABELS } from '@/components/common/type-icon'
import { DECISION_OPTIONS, RUN_OPTIONS, TagFilter } from '@/components/items/item-filters'
import { ItemSheet } from '@/components/items/item-sheet'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from '@/components/ui/select'
import { Sheet, SheetContent, SheetDescription, SheetHeader, SheetTitle } from '@/components/ui/sheet'
import { Skeleton } from '@/components/ui/skeleton'
import { useDebounced } from '@/hooks/use-debounced'
import { useLazyDialog } from '@/hooks/use-lazy-dialog'
import { useIsMobile } from '@/hooks/use-mobile'
import { ApiError, ITEM_TYPES, type Item, type ItemQuery } from '@/lib/api'
import { pluralize } from '@/lib/format'
import { flattenItems, useCollection, useItemsInfinite, useRefetchOnForeignChange } from '@/lib/queries'
import { STATUS_META } from '@/lib/status'
import { cn } from '@/lib/utils'

import { StatusCounts } from './collections-index'
import { TableView } from './item-views'

const SORTS = [
  { value: 'relevance', label: 'Best match' },
  { value: 'created_desc', label: 'Newest first' },
  { value: 'created_asc', label: 'Oldest first' },
  { value: 'updated_desc', label: 'Recently updated' },
  { value: 'decided_desc', label: 'Recently decided' },
  { value: 'title_asc', label: 'Title A–Z' },
] as const

type Sort = (typeof SORTS)[number]['value']

const SORTS_WITHOUT_RELEVANCE = SORTS.filter((s) => s.value !== 'relevance')

// Loaded on first open: the settings sheet (zod, react-hook-form, the actions editor) and the add-item form
// stay out of the JavaScript the collection page ships.
const AddItemDialog = dynamic(() => import('./add-item-dialog').then((m) => m.AddItemDialog), { ssr: false })
const CollectionSettingsSheet = dynamic(() => import('./collection-settings').then((m) => m.CollectionSettingsSheet), {
  ssr: false,
})

const splitList = (v: string) => v.split(',').filter(Boolean)

// The decisions plus the archive, which the API keeps apart (?decision=archived).
const COLLECTION_DECISIONS = [
  ...DECISION_OPTIONS,
  { value: 'archived', label: 'Archived', dot: STATUS_META.archived.dot },
]

export function CollectionPage({ id }: { id: string }) {
  const params = useSearchParams()
  const router = useRouter()
  const pathname = usePathname()
  const collection = useCollection(id)

  const decisionParam = params.get('decision') ?? ''
  const runParam = params.get('run') ?? ''
  const tagParam = params.get('tag') ?? ''
  // Stable arrays for the query key: they change only when the URL value does.
  const decisions = useMemo(() => splitList(decisionParam), [decisionParam])
  const runs = useMemo(() => splitList(runParam), [runParam])
  const tags = useMemo(() => splitList(tagParam), [tagParam])
  const type = (params.get('type') as ItemQuery['type']) || undefined
  const urlSort = params.get('sort') as Sort | null
  const openItem = params.get('item')
  const [q, setQ] = useState(params.get('q') ?? '')
  const debouncedQ = useDebounced(q.trim(), 300)
  // Full-text search ranks by relevance by default; without a query relevance means nothing.
  const defaultSort: Sort = debouncedQ ? 'relevance' : 'created_desc'
  const sort: Sort = urlSort && (urlSort !== 'relevance' || debouncedQ) ? urlSort : defaultSort
  const sortOptions = debouncedQ ? SORTS : SORTS_WITHOUT_RELEVANCE
  const isMobile = useIsMobile()
  const [filtersOpen, setFiltersOpen] = useState(false)
  const { open: addOpen, used: addUsed, setOpen: setAddOpen } = useLazyDialog()
  const { open: settingsOpen, used: settingsUsed, setOpen: setSettingsOpen } = useLazyDialog()

  // Every card gets `open`, and the filters get `setParams`, so neither may change when the URL does (opening
  // an item sets ?item=, which would otherwise re-render all loaded cards and every filter). Both read the
  // latest params from a ref.
  const latestParams = useRef(params)

  useEffect(() => {
    latestParams.current = params
  }, [params])

  const setParams = useCallback(
    (patch: Record<string, string | undefined>) => {
      const sp = new URLSearchParams(latestParams.current.toString())

      for (const [k, v] of Object.entries(patch)) {
        if (v) {
          sp.set(k, v)
        } else {
          sp.delete(k)
        }
      }

      const s = sp.toString()

      router.replace(`${pathname}${s ? `?${s}` : ''}`, { scroll: false })
    },
    [router, pathname],
  )

  // Mirror the debounced search box into the URL.
  const urlQ = params.get('q') ?? ''

  const syncUrl = useEffectEvent((value: string) => {
    if (value !== urlQ) {
      setParams({ q: value || undefined })
    }
  })

  useEffect(() => syncUrl(debouncedQ), [debouncedQ])

  const query: ItemQuery = useMemo(
    () =>
      ({
        collection_id: id,
        decision: decisions.join(',') || undefined,
        run: runs.join(',') || undefined,
        tag: tags.join(',') || undefined,
        // Archived items are done with: hidden unless picked.
        archived: decisions.length ? undefined : 'false',
        type,
        q: debouncedQ || undefined,
        sort,
        limit: 48,
      }) satisfies ItemQuery,
    [id, decisions, runs, tags, type, debouncedQ, sort],
  )
  // Like the inbox: no refetch of every loaded page on window focus; refetch only when another tab, agent or
  // rule changed something (useRefetchOnForeignChange). Revisits start from the first page (useItemsInfinite).
  const items = useItemsInfinite(query, { enabled: !!collection.data, refetchOnWindowFocus: false })

  // The query result is a new object on every render; its functions are stable.
  const { refetch, fetchNextPage } = items

  useRefetchOnForeignChange(refetch)
  const list = useMemo(() => flattenItems(items.data), [items.data])
  const filtered = !!(decisions.length || runs.length || tags.length || type || debouncedQ)
  const activeFilters =
    (decisions.length ? 1 : 0) +
    (runs.length ? 1 : 0) +
    (tags.length ? 1 : 0) +
    (type ? 1 : 0) +
    (urlSort && urlSort !== defaultSort ? 1 : 0)

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
      <PageContainer>
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

  // Own statements, so the React Compiler memoises them apart from the rest of the page (it doesn't memoise
  // JSX inside `?:`): opening or closing an item no longer re-renders the six filter selects.
  const desktopFilters = (
    <>
      <MultiSelect
        label="Decision filter"
        size="sm"
        className="w-36"
        anyLabel="All but archived"
        value={decisions}
        options={COLLECTION_DECISIONS}
        onChange={(v) => setParams({ decision: v.join(',') || undefined })}
      />
      <MultiSelect
        label="Run filter"
        size="sm"
        className="w-32"
        anyLabel="Any run"
        value={runs}
        options={RUN_OPTIONS}
        onChange={(v) => setParams({ run: v.join(',') || undefined })}
      />
      <TagFilter value={tags} onChange={(v) => setParams({ tag: v.join(',') || undefined })} />
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
      {/* No sort select on desktop: the table's column headers sort. */}
    </>
  )
  const filterSheet = (
    <Sheet open={filtersOpen} onOpenChange={setFiltersOpen}>
      <SheetContent side="bottom" className="rounded-t-2xl pb-[calc(1.25rem+env(safe-area-inset-bottom))]">
        <SheetHeader className="pb-0">
          <SheetTitle>Filter & sort</SheetTitle>
          <SheetDescription>{c.name}</SheetDescription>
        </SheetHeader>
        <div className="grid gap-4 px-4">
          <MultiSelect
            label="Decision filter"
            className="w-full"
            anyLabel="All but archived"
            value={decisions}
            options={COLLECTION_DECISIONS}
            onChange={(v) => setParams({ decision: v.join(',') || undefined })}
          />
          <MultiSelect
            label="Run filter"
            className="w-full"
            anyLabel="Any run"
            value={runs}
            options={RUN_OPTIONS}
            onChange={(v) => setParams({ run: v.join(',') || undefined })}
          />
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
  )

  return (
    <PageContainer>
      <div className="flex items-start justify-between gap-3">
        <div className="min-w-0">
          <div className="flex items-center gap-2">
            <h1 className="truncate text-xl font-semibold tracking-tight">{c.name}</h1>
            {c.is_default && (
              <span className="rounded-md bg-muted px-1.5 py-0.5 text-xs text-muted-foreground">Default</span>
            )}
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
            <IdChip id={c.id} label="collection" className="hidden sm:inline-flex" />
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
            <Plus />
            <span>
              Add<span className="max-sm:hidden"> item</span>
            </span>
          </Button>
        </div>
      </div>

      <div className="mt-5 flex flex-wrap items-center gap-2 sm:mt-6">
        <div className="relative min-w-0 flex-1 basis-40 sm:max-w-xs">
          <Search className="pointer-events-none absolute top-1/2 left-2.5 size-3.5 -translate-y-1/2 text-muted-foreground" />
          <Input
            value={q}
            onChange={(e) => setQ(e.target.value)}
            placeholder="Search items..."
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
          desktopFilters
        )}
      </div>

      <div
        className={cn('mt-4 transition-opacity', items.isPlaceholderData && 'opacity-60')}
        aria-busy={items.isPlaceholderData}
      >
        {items.isLoading ? (
          // The shape of what loads: cards on phones, table rows (header included) from md up.
          <div
            className="grid gap-2 md:gap-px md:overflow-hidden md:rounded-xl md:border md:bg-border"
            data-testid="items-loading"
          >
            <SkeletonList count={8} className="h-[4.625rem] rounded-xl md:h-11 md:rounded-none md:first:h-10" />
          </div>
        ) : items.error ? (
          <ErrorState error={items.error} onRetry={() => refetch()} />
        ) : (
          <TableView
            items={list}
            onOpen={open}
            empty={
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
                        setParams({
                          decision: undefined,
                          run: undefined,
                          tag: undefined,
                          type: undefined,
                          q: undefined,
                        })
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
            }
          />
        )}
        {items.hasNextPage && (
          <div className="mt-6 flex justify-center">
            <Button variant="outline" onClick={() => fetchNextPage()} disabled={items.isFetchingNextPage}>
              {items.isFetchingNextPage ? 'Loading...' : 'Load more'}
            </Button>
          </div>
        )}
      </div>

      {filterSheet}
      {addUsed && <AddItemDialog collectionId={c.id} open={addOpen} onOpenChange={setAddOpen} />}
      {settingsUsed && <CollectionSettingsSheet collection={c} open={settingsOpen} onOpenChange={setSettingsOpen} />}
      <ItemSheet id={openItem || null} onClose={() => setParams({ item: undefined })} />
    </PageContainer>
  )
}
