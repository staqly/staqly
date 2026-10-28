'use client'

import { Archive, PenSquare, Plus, Search, Settings2, X } from 'lucide-react'
import dynamic from 'next/dynamic'
import { useRouter, useSearchParams } from 'next/navigation'
import { useCallback, useEffect, useMemo, useRef, useState } from 'react'

import { EmptyState, ErrorState, LoadMore } from '@/components/common/error-state'
import { FilterButton } from '@/components/common/filter-button'
import { IdChip } from '@/components/common/id-chip'
import { MultiSelect } from '@/components/common/multi-select'
import { PageContainer } from '@/components/common/page-header'
import { PastPostsSwitch } from '@/components/common/past-posts-switch'
import { SkeletonList } from '@/components/common/skeleton-list'
import { TagFilter } from '@/components/common/tag-filter'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from '@/components/ui/select'
import { useDebounced } from '@/hooks/use-debounced'
import { useLazyDialog } from '@/hooks/use-lazy-dialog'
import { hidePastParam, setShowPast, useShowPast } from '@/hooks/use-show-past'
import { POST_STATUSES, type Collection, type PostQuery, type PostStatus } from '@/lib/api'
import {
  flattenPosts,
  useChannels,
  useCollections,
  usePostsInfinite,
  useRefetchOnForeignChange,
  useRole,
} from '@/lib/queries'
import { POST_STATUS_META } from '@/lib/status'
import { cn, hrefWithParams } from '@/lib/utils'

import { PostsQueryContext, postsQuery } from './post-href'
import { PostListHeader, PostRow, PostSelectionBar, usePostRowActions } from './post-list'
import { StatusCounts } from './status-counts'

const Composer = dynamic(() => import('./composer').then((m) => m.Composer), { ssr: false })
const CollectionSettingsSheet = dynamic(
  () => import('@/components/collections/collection-settings').then((m) => m.CollectionSettingsSheet),
  { ssr: false },
)

const splitList = (v: string) => v.split(',').filter(Boolean)

/** A filter in use under the title, with × to take it off. */
function FilterChip({ label, onRemove }: { label: string; onRemove: () => void }) {
  return (
    <span className="inline-flex max-w-full items-center gap-0.5 rounded-full border bg-muted/50 py-0.5 pr-0.5 pl-2.5 text-xs">
      <span className="truncate">{label}</span>
      <button
        type="button"
        onClick={onRemove}
        aria-label={`Remove filter ${label}`}
        className="relative inline-flex size-5 shrink-0 items-center justify-center rounded-full text-muted-foreground outline-none after:absolute after:-inset-3 hover:bg-muted hover:text-foreground focus-visible:ring-2 focus-visible:ring-ring"
      >
        <X className="size-3" />
      </button>
    </span>
  )
}

/** A collection's posts: header, filters, the list. */
export function CollectionPosts({ id }: { id: string }) {
  const router = useRouter()
  const params = useSearchParams()
  // From the collections list (cached by the pane already): no request of its own before the posts load.
  const pick = useCallback((cols: Collection[]) => cols.find((c) => c.id === id) ?? null, [id])
  const collection = useCollections(true, pick)
  const channels = useChannels()
  const status = (params.get('status') ?? '') as PostStatus | ''
  const channelIds = useMemo(() => splitList(params.get('channel') ?? ''), [params])
  const tags = useMemo(() => splitList(params.get('tag') ?? ''), [params])
  const [search, setSearch] = useState(params.get('q') ?? '')
  const q = useDebounced(search.trim(), 300)
  const showPast = useShowPast()
  const hidePast = hidePastParam(showPast, status)
  const query: PostQuery = useMemo(
    () => ({
      collection_id: id,
      status: status || undefined,
      hide_past: hidePast,
      channel_id: channelIds[0] || undefined,
      tag: tags[0] || undefined,
      q: q || undefined,
      sort: 'created_desc',
      limit: 50,
    }),
    [id, status, channelIds, tags, q, hidePast],
  )
  const posts = usePostsInfinite(query, { refetchOnWindowFocus: false })
  // ?compose=1 (the getting-started checklist) opens the composer at once.
  const compose = useLazyDialog(params.get('compose') === '1')
  const settings = useLazyDialog()

  useRefetchOnForeignChange(posts.refetch)

  const { canWrite } = useRole()
  // Links to a post keep this view (collection, filters) behind its sheet.
  const linkQuery = useMemo(() => postsQuery(params, id), [params, id])
  const rowActions = usePostRowActions(linkQuery)
  const [selected, setSelected] = useState<Set<string>>(() => new Set())
  const list = flattenPosts(posts.data)
  // Rows get one stable toggle; it reads the current list and the last clicked row from refs (Shift+click ranges).
  const listRef = useRef(list)
  const anchor = useRef<string | null>(null)
  const [toggle] = useState(() => (postId: string, range: boolean) => {
    setSelected((cur) => {
      const next = new Set(cur)
      const ids = listRef.current.map((p) => p.id)
      const from = anchor.current ? ids.indexOf(anchor.current) : -1

      if (range && from >= 0) {
        const to = ids.indexOf(postId)

        for (const x of ids.slice(Math.min(from, to), Math.max(from, to) + 1)) {
          next.add(x)
        }
      } else if (next.has(postId)) {
        next.delete(postId)
      } else {
        next.add(postId)
      }

      anchor.current = postId

      return next
    })
  })

  useEffect(() => {
    listRef.current = list
  })

  // Esc clears the selection (unless a dialog or menu took the key); Cmd/Ctrl+A selects every loaded post.
  useEffect(() => {
    const onKey = (e: KeyboardEvent) => {
      const typing = (e.target as HTMLElement | null)?.closest('input, textarea, [contenteditable=true], [role=dialog]')

      if (
        e.key === 'Escape' &&
        !e.defaultPrevented &&
        !document.querySelector('[role=dialog], [role=alertdialog], [role=menu]')
      ) {
        setSelected((cur) => (cur.size ? new Set() : cur))
      } else if (
        canWrite &&
        (e.metaKey || e.ctrlKey) &&
        e.key.toLowerCase() === 'a' &&
        !typing &&
        listRef.current.length
      ) {
        e.preventDefault()
        setSelected(new Set(listRef.current.map((p) => p.id)))
      }
    }

    window.addEventListener('keydown', onKey)

    return () => window.removeEventListener('keydown', onKey)
  }, [canWrite])

  const setParam = (key: string, value: string | undefined) =>
    router.replace(hrefWithParams('/posts', params, { collection: id, [key]: value }), { scroll: false })

  if (collection.isLoading) {
    return (
      <PageContainer>
        <SkeletonList count={6} className="h-10 rounded-lg" />
      </PageContainer>
    )
  }

  if (collection.error || !collection.data) {
    return (
      <PageContainer>
        <ErrorState
          error={collection.error ?? new Error('Collection not found')}
          onRetry={() => collection.refetch()}
        />
      </PageContainer>
    )
  }

  const c = collection.data
  const picked = list.filter((p) => selected.has(p.id))
  const filtered = !!status || channelIds.length > 0 || tags.length > 0 || !!q
  // Filters help only once there is something to filter; an active filter keeps them so it can be cleared.
  const showFilters = filtered || !!search || Object.values(c.status_counts ?? {}).some((n) => n > 0)
  // Posts that went out are left out of the list: say so when that is why it is empty.
  const pastHidden = !!hidePast && (c.status_counts?.published ?? 0) + (c.status_counts?.failed ?? 0) > 0
  const showPastButton = (
    <Button variant="outline" onClick={() => setShowPast(true)}>
      Show past posts
    </Button>
  )

  return (
    <div className="flex min-h-0 flex-1 flex-col">
      <div className="grid gap-3 px-4 pt-5 pb-3 sm:px-6 lg:px-8">
        <div className="flex flex-wrap items-start justify-between gap-x-4 gap-y-2">
          {/* basis + min-w-0: a long name or a description without spaces wraps instead of widening the page. */}
          <div className="min-w-0 flex-1 basis-64">
            <h1 className="flex min-w-0 items-center gap-2 text-xl font-semibold tracking-tight">
              <span className="truncate" title={c.name}>
                {c.name}
              </span>
              {c.archived && <Archive className="size-4 text-muted-foreground" aria-label="Archived" />}
            </h1>
            {c.description && <p className="mt-0.5 text-sm wrap-anywhere text-muted-foreground">{c.description}</p>}
            <div className="mt-1 flex flex-wrap items-center gap-x-3 gap-y-1">
              <StatusCounts counts={c.status_counts ?? {}} />
              <IdChip id={c.id} label="collection" />
            </div>
          </div>
          <div className="flex shrink-0 items-center gap-2">
            <Button variant="outline" size="sm" onClick={() => settings.setOpen(true)}>
              <Settings2 /> <span className="max-sm:sr-only">Settings</span>
            </Button>
            {/* Search and the selects live behind one button; it shows only once there is something to filter. */}
            {showFilters && (
              <FilterButton
                active={(q ? 1 : 0) + (status ? 1 : 0) + (channelIds.length ? 1 : 0) + (tags.length ? 1 : 0)}
                onClear={() => {
                  setSearch('')
                  router.replace(`/posts?collection=${id}`, { scroll: false })
                }}
              >
                <div className="relative">
                  <Search className="pointer-events-none absolute top-1/2 left-2.5 size-3.5 -translate-y-1/2 text-muted-foreground" />
                  <Input
                    value={search}
                    onChange={(e) => setSearch(e.target.value)}
                    placeholder="Search posts"
                    aria-label="Search posts"
                    className="pl-8"
                  />
                </div>
                <Select value={status || 'any'} onValueChange={(v) => setParam('status', v === 'any' ? undefined : v)}>
                  <SelectTrigger className="w-full" aria-label="Status filter">
                    <SelectValue />
                  </SelectTrigger>
                  <SelectContent>
                    <SelectItem value="any">Any status</SelectItem>
                    {POST_STATUSES.map((s) => (
                      <SelectItem key={s} value={s}>
                        {POST_STATUS_META[s].label}
                      </SelectItem>
                    ))}
                  </SelectContent>
                </Select>
                <MultiSelect
                  label="Channel filter"
                  className="w-full"
                  anyLabel="Any channel"
                  value={channelIds}
                  options={(channels.data ?? []).map((ch) => ({ value: ch.id, label: ch.name }))}
                  onChange={(v) => setParam('channel', v.join(',') || undefined)}
                />
                <TagFilter
                  className="w-full"
                  value={tags}
                  onChange={(v) => setParam('tag', v.join(',') || undefined)}
                />
              </FilterButton>
            )}
            <Button size="sm" onClick={() => compose.setOpen(true)} disabled={c.archived} data-testid="new-post">
              <Plus /> New post
            </Button>
          </div>
        </div>
        {/* Filters in use, as chips under the title (each one comes off with its ×), and the past posts switch. */}
        {showFilters && (
          <div className="flex flex-wrap items-center justify-between gap-x-4 gap-y-2">
            {filtered && (
              <div className="flex min-w-0 flex-wrap items-center gap-1.5" data-testid="post-filters">
                {q && <FilterChip label={`“${q}”`} onRemove={() => setSearch('')} />}
                {status && (
                  <FilterChip label={POST_STATUS_META[status].label} onRemove={() => setParam('status', undefined)} />
                )}
                {channelIds.map((ch) => (
                  <FilterChip
                    key={ch}
                    label={channels.data?.find((x) => x.id === ch)?.name ?? ch}
                    onRemove={() => setParam('channel', channelIds.filter((x) => x !== ch).join(',') || undefined)}
                  />
                ))}
                {tags.map((t) => (
                  <FilterChip
                    key={t}
                    label={`#${t}`}
                    onRemove={() => setParam('tag', tags.filter((x) => x !== t).join(',') || undefined)}
                  />
                ))}
              </div>
            )}
            <PastPostsSwitch className="ml-auto" />
          </div>
        )}
      </div>

      <div
        className={cn('flex min-h-0 flex-1 flex-col', posts.isPlaceholderData && 'opacity-60')}
        aria-busy={posts.isPlaceholderData}
      >
        {posts.isLoading ? (
          <div className="grid gap-2 px-4 sm:px-6 lg:px-8" data-testid="posts-loading">
            <SkeletonList count={6} className="h-12 rounded-lg" />
          </div>
        ) : posts.error && !posts.data ? (
          <PageContainer>
            <ErrorState error={posts.error} onRetry={() => posts.refetch()} />
          </PageContainer>
        ) : list.length === 0 ? (
          <PageContainer>
            <EmptyState
              icon={<PenSquare />}
              title={filtered ? 'No posts match' : pastHidden ? 'No upcoming posts' : 'No posts yet'}
              description={
                filtered
                  ? `Try another status, channel or search.${pastHidden ? ' Past posts are hidden.' : ''}`
                  : pastHidden
                    ? 'Posts that went out are hidden.'
                    : 'Write the first post here, or let your agent add posts to this collection through the API.'
              }
              action={
                filtered ? (
                  pastHidden ? (
                    showPastButton
                  ) : undefined
                ) : (
                  <div className="flex flex-wrap justify-center gap-2">
                    {pastHidden && showPastButton}
                    <Button onClick={() => compose.setOpen(true)} disabled={c.archived}>
                      <Plus /> New post
                    </Button>
                  </div>
                )
              }
            />
          </PageContainer>
        ) : (
          <>
            {/* Container queries: the row shows its columns once the list itself is wide enough. */}
            <div className="@container border-t">
              <PostListHeader
                allSelected={picked.length > 0 && picked.length === list.length}
                onSelectAll={
                  canWrite
                    ? () => setSelected(picked.length === list.length ? new Set() : new Set(list.map((p) => p.id)))
                    : undefined
                }
              />
              {/* data-selecting: every row's box shows while anything is selected, by CSS (no row renders for it). */}
              <ul
                className="group/list divide-y"
                data-testid="posts-list"
                data-selecting={selected.size > 0 || undefined}
              >
                <PostsQueryContext value={linkQuery}>
                  {list.map((p) => (
                    <PostRow
                      key={p.id}
                      post={p}
                      selected={selected.has(p.id)}
                      onToggle={canWrite ? toggle : undefined}
                      onAction={rowActions.onAction}
                    />
                  ))}
                </PostsQueryContext>
              </ul>
            </div>
            {posts.hasNextPage && (
              <LoadMore
                className="py-4"
                size="sm"
                onLoad={() => void posts.fetchNextPage()}
                loading={posts.isFetchingNextPage}
                failed={posts.isFetchNextPageError}
              />
            )}
            {picked.length > 0 && (
              <PostSelectionBar selected={picked} canWrite={canWrite} onClear={() => setSelected(new Set())} />
            )}
          </>
        )}
      </div>

      {rowActions.dialogs}
      {compose.used && <Composer open={compose.open} onOpenChange={compose.setOpen} collectionId={c.id} />}
      {settings.used && <CollectionSettingsSheet collection={c} open={settings.open} onOpenChange={settings.setOpen} />}
    </div>
  )
}
