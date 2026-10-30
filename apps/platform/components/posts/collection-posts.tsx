'use client'

import {
  Archive,
  BadgeCheck,
  CalendarClock,
  FolderPlus,
  PenSquare,
  Plus,
  Search,
  Settings2,
  X,
  type LucideIcon,
} from 'lucide-react'
import dynamic from 'next/dynamic'
import { useRouter, useSearchParams } from 'next/navigation'
import { useCallback, useEffect, useMemo, useRef, useState } from 'react'

import { EmptyState, ErrorState, LoadMore } from '@/components/common/error-state'
import { FilterButton } from '@/components/common/filter-button'
import { IdChip } from '@/components/common/id-chip'
import { MultiSelect } from '@/components/common/multi-select'
import { PageContainer } from '@/components/common/page-header'
import { SkeletonList } from '@/components/common/skeleton-list'
import { TagFilter } from '@/components/common/tag-filter'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from '@/components/ui/select'
import { useDebounced } from '@/hooks/use-debounced'
import { useLazyDialog } from '@/hooks/use-lazy-dialog'
import { POST_STATUSES, type Collection, type Post, type PostQuery, type PostStatus } from '@/lib/api'
import { setDragChip } from '@/lib/drag-chip'
import { isLocked, postTitle } from '@/lib/posts'
import {
  flattenPosts,
  useChannels,
  useCollections,
  usePostsInfinite,
  useRefetchOnForeignChange,
  useRefreshDuePosts,
  useRole,
} from '@/lib/queries'
import { POST_STATUS_META } from '@/lib/status'
import { cn, hrefWithParams } from '@/lib/utils'

import { PostsQueryContext, postsQuery } from './post-href'
import { POST_DRAG, PostRow, PostSelectionBar, SelectBox, usePostRowActions, type PostAction } from './post-list'
import { StatusCounts } from './status-counts'

const Composer = dynamic(() => import('./composer').then((m) => m.Composer), { ssr: false })
const CollectionSettingsSheet = dynamic(
  () => import('@/components/collections/collection-settings').then((m) => m.CollectionSettingsSheet),
  { ssr: false },
)

const splitList = (v: string) => v.split(',').filter(Boolean)
const NO_POSTS: Post[] = []

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

/** One part of the list (upcoming or published): a heading with its icon, count and subtitle, then its rows. */
function PostSection({
  icon: Icon,
  title,
  description,
  count,
  posts,
  list,
  selected,
  selecting,
  onToggle,
  onSelect,
  onDrag,
  onAction,
  empty,
  testId,
}: {
  icon: LucideIcon
  title: string
  description: string
  count?: number
  posts: ReturnType<typeof usePostsInfinite>
  list: Post[]
  selected: Set<string>
  /** Anything is selected (in either section): every row shows its box. */
  selecting: boolean
  onToggle?: (postId: string, range: boolean) => void
  onSelect?: (ids: string[]) => void
  onDrag?: (e: React.DragEvent, post: Post) => void
  onAction: (action: PostAction, post: Post) => void
  empty: string
  testId: string
}) {
  const all = list.length > 0 && list.every((p) => selected.has(p.id))

  return (
    <section aria-labelledby={`${testId}-title`} data-testid={testId} className="@container">
      <div className="flex items-start gap-2 px-4 pt-4 pb-3 sm:px-6 lg:px-8">
        {/* Just the icon, on the title's line. */}
        <Icon aria-hidden className="mt-0.5 size-4 shrink-0 text-muted-foreground" />
        <div className="min-w-0">
          <h2 id={`${testId}-title`} className="flex items-center gap-2 text-sm font-semibold">
            {title}{' '}
            {count !== undefined && (
              <span className="rounded-full bg-muted px-2 py-0.5 text-xs font-medium text-muted-foreground tabular-nums">
                {count}
              </span>
            )}
          </h2>
          <p className="text-xs text-muted-foreground">{description}</p>
        </div>
        {/* Selects (or clears) the section's loaded posts; Shift+click on rows selects a range. */}
        {onSelect && list.length > 0 && (
          <SelectBox
            checked={all}
            label={`Select all ${title.toLowerCase()} posts`}
            onClick={() => onSelect(list.map((p) => p.id))}
            className="mt-2 mr-3 ml-auto"
          />
        )}
      </div>
      {posts.isLoading ? (
        <div className="grid gap-2 px-4 pb-4 sm:px-6 lg:px-8">
          <SkeletonList count={3} className="h-12 rounded-lg" />
        </div>
      ) : posts.error && !posts.data ? (
        <div className="px-4 pb-4 sm:px-6 lg:px-8">
          <ErrorState error={posts.error} onRetry={() => posts.refetch()} />
        </div>
      ) : list.length === 0 ? (
        <p className="px-4 pb-4 text-sm text-muted-foreground sm:px-6 lg:px-8">{empty}</p>
      ) : (
        <div className="px-4 pb-2 sm:px-6 lg:px-8">
          {/* data-selecting: every row's box shows while anything is selected, by CSS (no row renders for it). */}
          <ul
            className="group/list divide-y overflow-hidden rounded-xl border"
            data-testid="posts-list"
            data-selecting={selecting || undefined}
          >
            {list.map((p) => (
              <PostRow
                key={p.id}
                post={p}
                selected={selected.has(p.id)}
                onToggle={onToggle}
                onDrag={onDrag}
                onAction={onAction}
              />
            ))}
          </ul>
          {posts.hasNextPage && (
            <LoadMore
              className="py-4"
              size="sm"
              onLoad={() => void posts.fetchNextPage()}
              loading={posts.isFetchingNextPage}
              failed={posts.isFetchNextPageError}
            />
          )}
        </div>
      )}
    </section>
  )
}

/** A collection's posts: header, filters, then the upcoming posts and, under them, the published ones. */
export function CollectionPosts({ id, onNewCollection }: { id: string; onNewCollection?: () => void }) {
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
  // Two lists: what is still to come (drafts, scheduled, publishing, failed) and what went out. A status filter
  // shows the one it belongs to.
  const showUpcoming = status !== 'published'
  const showPublished = !status || status === 'published'
  const base = useMemo(
    () => ({
      collection_id: id,
      channel_id: channelIds[0] || undefined,
      tag: tags[0] || undefined,
      q: q || undefined,
      limit: 50,
    }),
    [id, channelIds, tags, q],
  )
  const upcomingQuery: PostQuery = useMemo(
    () => ({ ...base, status: status || undefined, hide_past: true, sort: 'created_desc' }),
    [base, status],
  )
  const publishedQuery: PostQuery = useMemo(() => ({ ...base, status: 'published', sort: 'publish_desc' }), [base])
  const upcoming = usePostsInfinite(upcomingQuery, { enabled: showUpcoming, refetchOnWindowFocus: false })
  const published = usePostsInfinite(publishedQuery, { enabled: showPublished, refetchOnWindowFocus: false })
  // ?compose=1 (the getting-started checklist) opens the composer at once.
  const compose = useLazyDialog(params.get('compose') === '1')
  const settings = useLazyDialog()

  const { refetch: refetchUpcoming } = upcoming
  const { refetch: refetchPublished } = published
  const refetch = useCallback(() => {
    if (showUpcoming) {
      void refetchUpcoming()
    }

    if (showPublished) {
      void refetchPublished()
    }
  }, [showUpcoming, showPublished, refetchUpcoming, refetchPublished])

  useRefetchOnForeignChange(refetch)

  const { canWrite } = useRole()
  // Links to a post keep this view (collection, filters) behind its sheet.
  const linkQuery = useMemo(() => postsQuery(params, id), [params, id])
  const rowActions = usePostRowActions(linkQuery)
  const [selected, setSelected] = useState<Set<string>>(() => new Set())
  const upcomingList = showUpcoming ? flattenPosts(upcoming.data) : NO_POSTS
  const publishedList = showPublished ? flattenPosts(published.data) : NO_POSTS
  const list = useMemo(() => [...upcomingList, ...publishedList], [upcomingList, publishedList])

  useRefreshDuePosts(list)
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

  // Dragging a selected row takes the selection along (the posts that can move); any other row goes alone.
  const selectedRef = useRef(selected)
  const [drag] = useState(() => (e: React.DragEvent, post: Post) => {
    const sel = selectedRef.current
    const ids = sel.has(post.id)
      ? listRef.current.filter((p) => sel.has(p.id) && !isLocked(p)).map((p) => p.id)
      : [post.id]

    e.dataTransfer.setData(POST_DRAG, ids.join(','))
    e.dataTransfer.effectAllowed = 'move'
    setDragChip(e, ids.length > 1 ? `${ids.length} posts` : postTitle(post, 40), 'round')
  })

  useEffect(() => {
    listRef.current = list
    selectedRef.current = selected
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
  const counts = c.status_counts ?? {}
  const loading = (showUpcoming && upcoming.isLoading) || (showPublished && published.isLoading)
  const placeholder = upcoming.isPlaceholderData || published.isPlaceholderData
  // Counts per section come from the collection; with a filter they would not match the rows, so they hide.
  const upcomingCount = filtered
    ? undefined
    : (counts.draft ?? 0) + (counts.scheduled ?? 0) + (counts.publishing ?? 0) + (counts.failed ?? 0)
  const publishedCount = filtered ? undefined : (counts.published ?? 0)
  // Shift+click ranges and the header's box work per section; the selection bar acts on both.
  const selectSection = (ids: string[]) =>
    setSelected((cur) => {
      const next = new Set(cur)
      const all = ids.every((x) => next.has(x))

      for (const x of ids) {
        if (all) {
          next.delete(x)
        } else {
          next.add(x)
        }
      }

      return next
    })

  return (
    <div className="flex min-h-0 flex-1 flex-col">
      <div className="grid gap-3 px-4 pt-5 pb-3 sm:px-6 lg:px-8">
        <div className="flex flex-wrap items-start justify-between gap-x-4 gap-y-2">
          {/* basis + min-w-0: a long name or a description without spaces wraps instead of widening the page. */}
          <div className="min-w-0 flex-1 basis-64">
            {/* The ID beside the name, outside the heading so it is not part of its accessible name. */}
            <div className="flex min-w-0 items-center gap-x-3">
              <h1 className="flex min-w-0 items-center gap-2 text-xl font-semibold tracking-tight">
                <span className="truncate" title={c.name}>
                  {c.name}
                </span>
                {c.archived && <Archive className="size-4 text-muted-foreground" aria-label="Archived" />}
              </h1>
              <IdChip id={c.id} label="collection" className="shrink-[3]" />
            </div>
            {c.description && <p className="mt-0.5 text-sm wrap-anywhere text-muted-foreground">{c.description}</p>}
            {/* Only with posts: an empty collection says so in the list below. */}
            {Object.values(c.status_counts ?? {}).some((n) => n > 0) && (
              <StatusCounts counts={c.status_counts ?? {}} className="mt-1.5" />
            )}
          </div>
          <div className="flex shrink-0 items-center gap-2">
            {/* The only collection: the collections pane is hidden, so its New lives here. */}
            {onNewCollection && (
              <Button variant="outline" size="sm" onClick={onNewCollection}>
                <FolderPlus /> <span className="max-sm:sr-only">New collection</span>
              </Button>
            )}
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
        {/* Filters in use, as chips under the title (each one comes off with its ×). */}
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
      </div>

      <div className={cn('flex min-h-0 flex-1 flex-col', placeholder && 'opacity-60')} aria-busy={placeholder}>
        {loading ? (
          <div className="grid gap-2 px-4 sm:px-6 lg:px-8" data-testid="posts-loading">
            <SkeletonList count={6} className="h-12 rounded-lg" />
          </div>
        ) : list.length === 0 && !upcoming.error && !published.error ? (
          <PageContainer>
            <EmptyState
              icon={<PenSquare />}
              title={filtered ? 'No posts match' : 'No posts yet'}
              description={
                filtered
                  ? 'Try another status, channel or search.'
                  : 'Write the first post here, or let your agent add posts to this collection through the API.'
              }
              action={
                filtered ? undefined : (
                  <Button onClick={() => compose.setOpen(true)} disabled={c.archived}>
                    <Plus /> New post
                  </Button>
                )
              }
            />
          </PageContainer>
        ) : (
          <PostsQueryContext value={linkQuery}>
            {showUpcoming && (
              <PostSection
                icon={CalendarClock}
                title="Upcoming"
                description="Drafts, scheduled posts and the ones to retry"
                count={upcomingCount}
                posts={upcoming}
                list={upcomingList}
                selected={selected}
                selecting={picked.length > 0}
                onToggle={canWrite ? toggle : undefined}
                onSelect={canWrite ? selectSection : undefined}
                onDrag={canWrite ? drag : undefined}
                onAction={rowActions.onAction}
                empty={
                  filtered ? 'No upcoming posts match.' : 'Nothing coming up: new drafts and scheduled posts show here.'
                }
                testId="posts-upcoming"
              />
            )}
            {/* Nothing went out yet: no empty Published section under the upcoming posts. */}
            {showPublished && (publishedList.length > 0 || !!published.error || status === 'published') && (
              <PostSection
                icon={BadgeCheck}
                title="Published"
                description="Out on their channels, the latest first"
                count={publishedCount}
                posts={published}
                list={publishedList}
                selected={selected}
                selecting={picked.length > 0}
                onToggle={canWrite ? toggle : undefined}
                onSelect={canWrite ? selectSection : undefined}
                onDrag={canWrite ? drag : undefined}
                onAction={rowActions.onAction}
                empty="No published posts match."
                testId="posts-published"
              />
            )}
            {picked.length > 0 && (
              <PostSelectionBar selected={picked} canWrite={canWrite} onClear={() => setSelected(new Set())} />
            )}
          </PostsQueryContext>
        )}
      </div>

      {rowActions.dialogs}
      {compose.used && <Composer open={compose.open} onOpenChange={compose.setOpen} collectionId={c.id} />}
      {settings.used && <CollectionSettingsSheet collection={c} open={settings.open} onOpenChange={settings.setOpen} />}
    </div>
  )
}
