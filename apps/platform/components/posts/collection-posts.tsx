'use client'

import { Archive, PenSquare, Plus, Search, Settings2, X } from 'lucide-react'
import dynamic from 'next/dynamic'
import Link from 'next/link'
import { useRouter, useSearchParams } from 'next/navigation'
import { useMemo, useState } from 'react'

import { ChannelStack } from '@/components/channels/channel-avatars'
import { EmptyState, ErrorState } from '@/components/common/error-state'
import { IdChip } from '@/components/common/id-chip'
import { MultiSelect } from '@/components/common/multi-select'
import { PageContainer } from '@/components/common/page-header'
import { SkeletonList } from '@/components/common/skeleton-list'
import { PostStatusBadge } from '@/components/common/status-badge'
import { TagFilter } from '@/components/common/tag-filter'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from '@/components/ui/select'
import { useDebounced } from '@/hooks/use-debounced'
import { useLazyDialog } from '@/hooks/use-lazy-dialog'
import { POST_STATUSES, type Post, type PostQuery, type PostStatus } from '@/lib/api'
import { formatDateTime, pluralize, timeAgo } from '@/lib/format'
import { postTitle } from '@/lib/posts'
import { flattenPosts, useChannels, useCollection, usePostsInfinite, useRefetchOnForeignChange } from '@/lib/queries'
import { formatSchedule } from '@/lib/schedule'
import { POST_STATUS_META } from '@/lib/status'
import { cn, hrefWithParams } from '@/lib/utils'

import { StatusCounts } from './posts-workspace'

const Composer = dynamic(() => import('./composer').then((m) => m.Composer), { ssr: false })
const CollectionSettingsSheet = dynamic(
  () => import('@/components/collections/collection-settings').then((m) => m.CollectionSettingsSheet),
  { ssr: false },
)

const splitList = (v: string) => v.split(',').filter(Boolean)

/** One post as a row: channels, first line, status, when. */
function PostRow({ post, channels }: { post: Post; channels: ReturnType<typeof useChannels>['data'] }) {
  const targets = (post.targets ?? []).map((t) => ({
    id: t.channel_id,
    provider: t.provider,
    name: t.channel_name,
    avatar_url: channels?.find((c) => c.id === t.channel_id)?.avatar_url,
  }))
  const when = post.publish_at ? formatSchedule(post.publish_at) : null

  return (
    <li>
      <Link
        prefetch={false}
        href={`/posts/${post.id}`}
        data-testid="post-row"
        className="grid grid-cols-[auto_minmax(0,1fr)_auto] items-center gap-3 px-4 py-2.5 transition-colors hover:bg-muted/60 sm:grid-cols-[auto_minmax(0,1fr)_auto_auto] pointer-coarse:min-h-12"
      >
        <ChannelStack channels={targets} size="size-7" />
        <span className="min-w-0">
          <span className="block truncate text-sm font-medium">{postTitle(post)}</span>
          <span className="block truncate text-xs text-muted-foreground" title={formatDateTime(post.created_at)}>
            {when ? (post.status === 'published' ? `Published ${when}` : when) : `Created ${timeAgo(post.created_at)}`}
            {post.tags.length > 0 && ` · ${post.tags.join(', ')}`}
          </span>
        </span>
        <PostStatusBadge status={post.status} />
        <span className="hidden text-xs text-muted-foreground tabular-nums sm:block">
          {pluralize(post.targets?.length ?? 0, 'channel')}
        </span>
      </Link>
    </li>
  )
}

/** A collection's posts: header, filters, the list. */
export function CollectionPosts({ id }: { id: string }) {
  const router = useRouter()
  const params = useSearchParams()
  const collection = useCollection(id)
  const channels = useChannels()
  const status = (params.get('status') ?? '') as PostStatus | ''
  const channelIds = useMemo(() => splitList(params.get('channel') ?? ''), [params])
  const tags = useMemo(() => splitList(params.get('tag') ?? ''), [params])
  const [search, setSearch] = useState(params.get('q') ?? '')
  const q = useDebounced(search.trim(), 300)
  const query: PostQuery = useMemo(
    () => ({
      collection_id: id,
      status: status || undefined,
      channel_id: channelIds[0] || undefined,
      tag: tags[0] || undefined,
      q: q || undefined,
      sort: 'created_desc',
      limit: 50,
    }),
    [id, status, channelIds, tags, q],
  )
  const posts = usePostsInfinite(query, { enabled: !!collection.data, refetchOnWindowFocus: false })
  const compose = useLazyDialog()
  const settings = useLazyDialog()

  useRefetchOnForeignChange(posts.refetch)

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
  const list = flattenPosts(posts.data)
  const filtered = !!status || channelIds.length > 0 || tags.length > 0 || !!q

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
            <Button size="sm" onClick={() => compose.setOpen(true)} disabled={c.archived} data-testid="new-post">
              <Plus /> New post
            </Button>
          </div>
        </div>
        <div className="flex flex-wrap items-center gap-2" data-testid="post-filters">
          <div className="relative">
            <Search className="pointer-events-none absolute top-1/2 left-2.5 size-3.5 -translate-y-1/2 text-muted-foreground" />
            <Input
              value={search}
              onChange={(e) => setSearch(e.target.value)}
              placeholder="Search posts"
              aria-label="Search posts"
              className="h-8 w-56 pl-8"
            />
          </div>
          <Select value={status || 'any'} onValueChange={(v) => setParam('status', v === 'any' ? undefined : v)}>
            <SelectTrigger size="sm" className="w-36" aria-label="Status filter">
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
            size="sm"
            className="w-40"
            anyLabel="Any channel"
            value={channelIds}
            options={(channels.data ?? []).map((ch) => ({ value: ch.id, label: ch.name }))}
            onChange={(v) => setParam('channel', v.join(',') || undefined)}
          />
          <TagFilter value={tags} onChange={(v) => setParam('tag', v.join(',') || undefined)} />
          {filtered && (
            <Button
              variant="ghost"
              size="sm"
              onClick={() => {
                setSearch('')
                router.replace(`/posts?collection=${id}`, { scroll: false })
              }}
            >
              <X /> Clear
            </Button>
          )}
        </div>
      </div>

      <div
        className={cn('min-h-0 flex-1', posts.isPlaceholderData && 'opacity-60')}
        aria-busy={posts.isPlaceholderData}
      >
        {posts.isLoading ? (
          <div className="grid gap-2 px-4 sm:px-6 lg:px-8" data-testid="posts-loading">
            <SkeletonList count={6} className="h-12 rounded-lg" />
          </div>
        ) : posts.error ? (
          <PageContainer>
            <ErrorState error={posts.error} onRetry={() => posts.refetch()} />
          </PageContainer>
        ) : list.length === 0 ? (
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
          <>
            <ul className="divide-y border-t" data-testid="posts-list">
              {list.map((p) => (
                <PostRow key={p.id} post={p} channels={channels.data} />
              ))}
            </ul>
            {posts.hasNextPage && (
              <div className="flex justify-center py-4">
                <Button
                  variant="outline"
                  size="sm"
                  onClick={() => posts.fetchNextPage()}
                  disabled={posts.isFetchingNextPage}
                >
                  {posts.isFetchingNextPage ? 'Loading...' : 'Load more'}
                </Button>
              </div>
            )}
          </>
        )}
      </div>

      {compose.used && <Composer open={compose.open} onOpenChange={compose.setOpen} collectionId={c.id} />}
      {settings.used && <CollectionSettingsSheet collection={c} open={settings.open} onOpenChange={settings.setOpen} />}
    </div>
  )
}
