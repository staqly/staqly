'use client'

import {
  replaceEqualDeep,
  useInfiniteQuery,
  useMutation,
  useQueries,
  useQuery,
  useQueryClient,
  type InfiniteData,
  type QueryClient,
  keepPreviousData,
} from '@tanstack/react-query'
import { useEffect, useMemo, useRef, useState } from 'react'

import {
  api,
  setCsrfToken,
  type Collection,
  type EventPage,
  type EventQuery,
  type Mode,
  type Post,
  type PostPage,
  type PostQuery,
  type StashEvent,
} from './api'
import { toastApiError } from './notify'

export const qk = {
  ai: ['ai'] as const,
  aiJob: (id: string) => ['ai', 'job', id] as const,
  setup: ['setup'] as const,
  session: ['session'] as const,
  sessions: ['sessions'] as const,
  members: ['members'] as const,
  invitations: ['invitations'] as const,
  notificationPrefs: ['notification-prefs'] as const,
  // One cached list, archived collections included; useCollections(false) filters them out in `select`.
  collections: () => ['collections', { includeArchived: true }] as const,
  collectionsAll: ['collections'] as const,
  collection: (id: string) => ['collection', id] as const,
  posts: (query: PostQuery) => ['posts', query] as const,
  postsAll: ['posts'] as const,
  post: (id: string) => ['post', id] as const,
  channels: ['channels'] as const,
  channelGroups: ['channel-groups'] as const,
  providers: ['providers'] as const,
  schedules: (collectionId?: string) => ['schedules', collectionId ?? 'all'] as const,
  schedulesAll: ['schedules'] as const,
  jobs: (status?: string) => ['jobs', status ?? 'all'] as const,
  jobsAll: ['jobs'] as const,
  jobStats: ['jobs', 'stats'] as const,
  events: (query: EventQuery) => ['events', query] as const,
  // Outside ['events'] on purpose: invalidating the activity feed after our own changes must not trigger it.
  recentEvents: ['recent-events'] as const,
  tags: ['tags'] as const,
  eventsAll: ['events'] as const,
  apiKeys: ['api-keys'] as const,
  webhooks: ['webhooks'] as const,
  deliveries: (webhookId?: string) => ['deliveries', webhookId ?? 'all'] as const,
  health: ['health'] as const,
  plans: ['plans'] as const,
  billing: ['billing'] as const,
  billingDetails: ['billing', 'details'] as const,
  pendingPayment: ['billing', 'pending-payment'] as const,
  xPosts: ['billing', 'x-posts'] as const,
  addons: ['billing', 'addons'] as const,
  usage: ['usage'] as const,
  alerts: ['alerts'] as const,
  alertDeliveries: (id: string) => ['alerts', id, 'deliveries'] as const,
  workspaceExport: (id: string) => ['workspace-export', id] as const,
  workspaceExports: ['workspace-exports'] as const,
  readiness: ['readyz'] as const,
}

export function useSetupStatus() {
  return useQuery({ queryKey: qk.setup, queryFn: api.setupStatus, staleTime: 30_000 })
}

async function sessionQueryFn() {
  const s = await api.session()

  setCsrfToken(s.csrf_token ?? '')

  return s
}

export function useSession() {
  return useQuery({
    queryKey: qk.session,
    queryFn: sessionQueryFn,
    staleTime: 60_000,
    retry: false,
  })
}

/**
 * Server mode. Older servers did not report it and behaved like cloud mode.
 * The session (loaded by the auth gate) is authoritative; setup status is the fallback.
 */
export function useMode() {
  const session = useQuery({ queryKey: qk.session, queryFn: sessionQueryFn, enabled: false, staleTime: 60_000 })
  const setup = useSetupStatus()
  const mode: Mode = session.data?.mode ?? setup.data?.mode ?? 'cloud'

  return { mode, isLocal: mode === 'local', isCloud: mode !== 'local' }
}

/** Public cloud plans (not including the local "unlocked" plan). */
export function useBilling(opts?: { enabled?: boolean }) {
  return useQuery({ queryKey: qk.billing, queryFn: api.billing, enabled: opts?.enabled })
}

/** A plan change waiting for its payment and why it isn't paid, read from Stripe (asked again on focus). */
export function usePendingPayment(opts?: { enabled?: boolean }) {
  return useQuery({ queryKey: qk.pendingPayment, queryFn: api.pendingPayment, enabled: opts?.enabled })
}

/** The subscription period, cards and the first page of invoices, read from Stripe (cached a minute). */
export function useBillingDetails(opts?: { enabled?: boolean }) {
  return useQuery({
    queryKey: qk.billingDetails,
    queryFn: () => api.billingDetails(),
    enabled: opts?.enabled,
    staleTime: 60_000,
  })
}

/** Posts to X this period (cloud): what the plan includes, what's used, bought posts left. */
export function useXPosts(opts?: { enabled?: boolean }) {
  return useQuery({ queryKey: qk.xPosts, queryFn: api.xPosts, enabled: opts?.enabled, staleTime: 60_000 })
}

export function useAddons(opts?: { enabled?: boolean }) {
  return useQuery({
    queryKey: qk.addons,
    queryFn: async () => (await api.addons()).data ?? [],
    enabled: opts?.enabled,
    staleTime: 60_000,
  })
}

export function usePlans(opts?: { enabled?: boolean }) {
  return useQuery({
    queryKey: qk.plans,
    queryFn: async () => (await api.plans()).data ?? [],
    staleTime: 10 * 60_000,
    enabled: opts?.enabled,
  })
}

export function useUsage() {
  return useQuery({ queryKey: qk.usage, queryFn: api.usage, staleTime: 30_000 })
}

/**
 * Whether backups (workspace export with files) are available: always locally, in the cloud on a plan with data
 * export (Plus and above, and the trial); `data-only` on one without (Starter). Undefined while the plan is unknown.
 */
export function useBackupAccess(): 'local' | 'paid' | 'data-only' | undefined {
  const { isLocal } = useMode()
  const usage = useUsage()

  if (isLocal) {
    return 'local'
  }

  const plan = usage.data?.plan

  if (!plan) {
    return undefined
  }

  return plan.limits.data_export ? 'paid' : 'data-only'
}

const activeOnly = (cols: Collection[]) => cols.filter((c) => !c.archived)

/**
 * Every caller shares one request (archived included; `includeArchived = false` filters in `select`). Two
 * lists meant two requests of the whole list on every page with the shell, and two more after every change
 * the live updates notice.
 */
export function useCollections<T = Collection[]>(includeArchived = false, select?: (collections: Collection[]) => T) {
  const pick = useMemo(
    () => (includeArchived ? select : select ? (cols: Collection[]) => select(activeOnly(cols)) : activeOnly),
    [includeArchived, select],
  )

  return useQuery({
    queryKey: qk.collections(),
    queryFn: async () => (await api.collections({ includeArchived: true })).data ?? [],
    select: pick as ((collections: Collection[]) => T) | undefined,
  })
}

/** A cursor page of anything with an id (posts, events). */
interface IdPage {
  data: { id: string }[]
  next_cursor?: string | null
}

/**
 * Structural sharing by id instead of array index. Removing a post locally (publish now, delete) shifts
 * every later post, and TanStack's default `replaceEqualDeep` compares by index, so it would hand every
 * later row a fresh copy and re-render it. Here unchanged posts, pages and the page list keep their identity,
 * so memoised rows only re-render when their own post changed.
 */
export function sharePages(oldData: unknown, newData: unknown): unknown {
  const prev = oldData as InfiniteData<IdPage> | undefined
  const next = newData as InfiniteData<IdPage> | undefined

  if (!prev?.pages || !next?.pages) {
    return replaceEqualDeep(oldData, newData)
  }

  const byId = new Map(prev.pages.flatMap((p) => p.data.map((it) => [it.id, it] as const)))
  const pages = next.pages.map((page, i) => {
    const data = page.data.map((it) => {
      const old = byId.get(it.id)

      return old ? replaceEqualDeep(old, it) : it
    })
    const before = prev.pages[i]
    const same =
      before !== undefined &&
      before.next_cursor === page.next_cursor &&
      before.data.length === data.length &&
      data.every((it, j) => it === before.data[j])

    return same ? before : { ...page, data }
  })
  const pageParams = replaceEqualDeep(prev.pageParams, next.pageParams)
  const unchanged =
    pageParams === prev.pageParams && pages.length === prev.pages.length && pages.every((p, i) => p === prev.pages[i])

  return unchanged ? prev : { pages, pageParams }
}

/**
 * Keep only the first page of a cached list nobody is showing any more. Refetching an infinite query reloads
 * every page it holds, one request after the other, so coming back to a list (revisit, cleared search or
 * filter) would replay them all. Trimmed, it costs one request; "Load more" brings the rest back. The
 * timestamp is kept so staleness is unchanged.
 */
function trimUnwatchedList(qc: QueryClient, queryKey: readonly unknown[], opts?: { watched?: boolean }) {
  const q = qc.getQueryCache().find<InfiniteData<IdPage>>({ queryKey, exact: true })
  const data = q?.state.data

  if (!q || (!opts?.watched && q.getObserversCount() > 0) || !data || data.pages.length <= 1) {
    return
  }

  qc.setQueryData<InfiniteData<IdPage>>(
    queryKey,
    { pages: data.pages.slice(0, 1), pageParams: data.pageParams.slice(0, 1) },
    { updatedAt: q.state.dataUpdatedAt },
  )
}

/** Trim the list to its first page once nothing shows it any more (see trimUnwatchedList). */
function useTrimWhenUnwatched(queryKey: readonly unknown[]) {
  const qc = useQueryClient()
  const key = JSON.stringify(queryKey)

  useEffect(
    () => () => {
      // After the observer has unsubscribed (its own effect cleanup may run after this one).
      setTimeout(() => trimUnwatchedList(qc, JSON.parse(key) as unknown[]), 0)
    },
    [qc, key],
  )
}

export function usePostsInfinite(
  query: PostQuery,
  opts?: { enabled?: boolean; refetchInterval?: number; refetchOnWindowFocus?: boolean },
) {
  const queryKey = qk.posts(query)

  useTrimWhenUnwatched(queryKey)

  return useInfiniteQuery({
    queryKey,
    queryFn: ({ pageParam, signal }) => api.posts({ ...query, cursor: pageParam || undefined }, signal),
    initialPageParam: '',
    getNextPageParam: (last) => last.next_cursor || undefined,
    enabled: opts?.enabled,
    refetchInterval: opts?.refetchInterval,
    refetchOnWindowFocus: opts?.refetchOnWindowFocus,
    structuralSharing: sharePages,
    // A new search or filter keeps showing the previous results (check `isPlaceholderData`) instead of
    // flashing the loading skeleton.
    placeholderData: keepPreviousData,
  })
}

/**
 * The newest workspace events: one small request whose top entry changes whenever any post is created,
 * scheduled, published, edited or deleted. Long paged lists poll this instead of refetching every loaded page.
 */
export function useRecentEvents(opts?: { refetchInterval?: number }) {
  return useQuery({
    queryKey: qk.recentEvents,
    queryFn: async () => (await api.events({ limit: 20 })).data ?? [],
    refetchInterval: opts?.refetchInterval,
  })
}

/**
 * Post ids this tab changed itself in the last `ttlMs`. Their events are not a reason to refetch a list; a
 * later change to the same post (an agent edits it, the scheduler publishes it) is. Entries expire, so the
 * set stays small however many posts a long session touches.
 */
export function createRecentIds(ttlMs = 60_000) {
  const ids = new Map<string, number>()

  return {
    add(id: string) {
      const now = Date.now()

      for (const [k, at] of ids) {
        if (now - at > ttlMs) {
          ids.delete(k)
        }
      }

      ids.delete(id)
      ids.set(id, now)
    },
    has(id: string) {
      const at = ids.get(id)

      return at !== undefined && Date.now() - at <= ttlMs
    },
  }
}

// Posts whose change this tab already wrote into the cache (syncPostInCache / removePostFromLists).
const appliedHere = createRecentIds()

// What happens to a post after this tab let go of it: the queue, the platform or your automation reports it.
const OUTCOME = /^post\.(publish_requested|published|failed|target_\w+|reply_\w+)$/

/**
 * An event this tab has not written into the cache itself. A post this tab just changed still gets news from
 * elsewhere: the outcome of publishing and anything the system did (local mode can't tell the dashboard from
 * an automation by its actor, so outcomes count as foreign whoever reported them).
 */
export function isForeignEvent(e: StashEvent, isLocal?: (postId: string) => boolean) {
  if (!e.post_id || e.actor?.type === 'system' || OUTCOME.test(e.type)) {
    return true
  }

  return !(appliedHere.has(e.post_id) || isLocal?.(e.post_id))
}

/**
 * Refetch a paged list only when the workspace changed in a way this tab has not applied itself: polls the
 * newest events (one small request; also refetched on window focus) instead of the list's pages.
 */
export function useRefetchOnForeignChange(
  refetch: () => unknown,
  opts?: { refetchInterval?: number; isLocal?: (postId: string) => boolean },
) {
  const recentEvents = useRecentEvents({ refetchInterval: opts?.refetchInterval })
  const seenSeq = useRef<number | undefined>(undefined)
  const isLocal = opts?.isLocal

  useEffect(() => {
    const events = recentEvents.data

    if (!events) {
      return
    }

    const previous = seenSeq.current

    seenSeq.current = events[0]?.seq ?? 0

    if (previous === undefined || seenSeq.current === previous) {
      return
    }

    const unseen = events.filter((e) => e.seq > previous)
    const others = unseen.filter((e) => isForeignEvent(e, isLocal))
    // All fetched events are new: there may be more we did not see, so assume a foreign change.
    const complete = unseen.length < events.length

    if (!complete || others.length > 0) {
      void refetch()
    }
  }, [recentEvents.data, refetch, isLocal])
}

/**
 * The queue moves a due post to publishing without an event, so a list would keep saying Scheduled until the
 * outcome arrives. While a loaded post is past its time but still scheduled, read it again every few seconds
 * and write what came back into the lists.
 */
export function useRefreshDuePosts(posts: Post[]) {
  const qc = useQueryClient()
  // Re-render when the next post falls due, not on a ticking clock (it would re-render the whole calendar).
  const [now, setNow] = useState(() => Date.now())
  const times = posts.flatMap((p) => (p.status === 'scheduled' && p.publish_at ? [Date.parse(p.publish_at)] : []))
  const next = Math.min(...times.filter((t) => t > now))
  const due = posts.filter((p) => p.status === 'scheduled' && p.publish_at && Date.parse(p.publish_at) <= now)

  useEffect(() => {
    if (!Number.isFinite(next)) {
      return
    }

    let t: ReturnType<typeof setTimeout> | undefined
    const arm = () => {
      const left = next - Date.now()

      if (left <= 0) {
        setNow(Date.now())

        return
      }

      // Long waits need another timer even when `next` stays unchanged; include the margin in the cap.
      t = setTimeout(arm, Math.min(left + 50, 2 ** 31 - 1))
    }

    arm()

    return () => clearTimeout(t)
  }, [next])
  const results = useQueries({
    queries: due.map((p) => ({
      queryKey: qk.post(p.id),
      queryFn: () => api.post(p.id),
      refetchInterval: 3000,
      staleTime: 0,
    })),
  })

  useEffect(() => {
    for (const post of results.map((r) => r.data)) {
      if (post && post.status !== 'scheduled') {
        // Not syncPostInCache: this change is the queue's, so later events about the post still count.
        qc.setQueriesData<InfiniteData<PostPage>>({ queryKey: qk.postsAll }, (old) =>
          old?.pages.some((p) => p.data.some((it) => it.id === post.id && it.status !== post.status))
            ? {
                ...old,
                pages: old.pages.map((p) => ({ ...p, data: p.data.map((it) => (it.id === post.id ? post : it)) })),
              }
            : old,
        )
      }
    }
  }, [results, qc])
}

export function usePost(id: string) {
  return useQuery({ queryKey: qk.post(id), queryFn: () => api.post(id), enabled: !!id })
}

/**
 * The activity feed. Like post lists: shared by event id (a new event at the top doesn't re-render every
 * row), no refetch on window focus (it would reload every loaded page; the feed has a Refresh button) and
 * trimmed to its first page when nobody shows it.
 */
export function useEventsInfinite(query: EventQuery) {
  const queryKey = qk.events(query)

  useTrimWhenUnwatched(queryKey)

  return useInfiniteQuery({
    queryKey,
    queryFn: ({ pageParam }) => api.events({ ...query, cursor: pageParam || undefined }),
    initialPageParam: '',
    getNextPageParam: (last) => last.next_cursor || undefined,
    refetchOnWindowFocus: false,
    structuralSharing: sharePages,
    placeholderData: keepPreviousData,
  })
}

/**
 * Bring event lists up to date after new workspace events. The activity feed is newest first and events never
 * change, so instead of invalidating it (which refetches every loaded page, one after the other, on every new
 * event) fetch its first page and put the events newer than its top one in front. Lists not on screen and
 * other event queries are just invalidated (they refetch on their next visit, from one page).
 */
export async function refreshEventLists(qc: QueryClient) {
  await Promise.all(
    qc
      .getQueryCache()
      .findAll({ queryKey: qk.eventsAll })
      .map((q) => refreshEventList(qc, q.queryKey)),
  )
}

export async function refreshEventList(qc: QueryClient, queryKey: readonly unknown[]) {
  const q = qc.getQueryCache().find<InfiniteData<EventPage>>({ queryKey, exact: true })
  const data = q?.state.data
  const invalidate = () => qc.invalidateQueries({ queryKey, exact: true })

  if (!q || !data?.pages || data.pages.length <= 1 || q.getObserversCount() === 0) {
    void invalidate()

    return
  }

  if (q.state.fetchStatus === 'fetching') {
    // A refetch or "Load older" is under way and brings the new state itself.
    return
  }

  const top = data.pages[0]?.data[0]?.seq ?? 0
  let first: EventPage

  try {
    first = await api.events(queryKey[1] as EventQuery)
  } catch {
    void invalidate()

    return
  }

  const fresh = (first.data ?? []).filter((e) => e.seq > top)

  if (!fresh.length) {
    return
  }

  if (fresh.length === first.data?.length) {
    // More new events than one page: there may be a gap below them, so reload from the first page.
    trimUnwatchedList(qc, queryKey, { watched: true })
    void invalidate()

    return
  }

  qc.setQueryData<InfiniteData<EventPage>>(queryKey, (old) => {
    if (!old?.pages.length) {
      return old
    }

    // Against the list as it is now: another refresh may have put some of these in front while this one waited.
    const now = old.pages[0].data[0]?.seq ?? 0
    const add = fresh.filter((e) => e.seq > now)

    return add.length
      ? { ...old, pages: [{ ...old.pages[0], data: [...add, ...old.pages[0].data] }, ...old.pages.slice(1)] }
      : old
  })
}

/** Flatten infinite post pages. */
export function flattenPosts(data?: InfiniteData<PostPage>): Post[] {
  return data?.pages.flatMap((p) => p.data) ?? []
}

/** Write an updated post everywhere it is cached (detail + any list). */
export function syncPostInCache(qc: QueryClient, post: Post) {
  appliedHere.add(post.id)
  qc.setQueryData(qk.post(post.id), post)
  qc.setQueriesData<InfiniteData<PostPage>>({ queryKey: qk.postsAll }, (old) =>
    old
      ? {
          ...old,
          pages: old.pages.map((p) => ({ ...p, data: p.data.map((it) => (it.id === post.id ? post : it)) })),
        }
      : old,
  )
}

/**
 * After this tab created a post: reload post lists from their first page. Invalidating them as they are
 * would refetch every loaded page one by one (and the new post's event would trigger a second round in
 * useRefetchOnForeignChange, so it is marked as applied here).
 */
export function refetchPostListsFromStart(qc: QueryClient, created?: string | string[]) {
  for (const id of typeof created === 'string' ? [created] : (created ?? [])) {
    appliedHere.add(id)
  }

  for (const q of qc.getQueryCache().findAll({ queryKey: qk.postsAll })) {
    trimUnwatchedList(qc, q.queryKey, { watched: true })
  }

  void qc.invalidateQueries({ queryKey: qk.postsAll })
}

/** Remove a post from all cached lists (e.g. after delete). */
export function removePostFromLists(qc: QueryClient, id: string) {
  appliedHere.add(id)
  qc.setQueriesData<InfiniteData<PostPage>>({ queryKey: qk.postsAll }, (old) =>
    old ? { ...old, pages: old.pages.map((p) => ({ ...p, data: p.data.filter((it) => it.id !== id) })) } : old,
  )
}

/**
 * Refresh everything that depends on posts after a mutation. Post lists are not refetched: callers write
 * the server's response into the cache with `syncPostInCache` / `removePostFromLists` instead. `usage`:
 * only creating or deleting posts/media changes the plan counters.
 */
export function invalidateAfterPostChange(qc: QueryClient, postId?: string, opts?: { usage?: boolean }) {
  if (opts?.usage) {
    void qc.invalidateQueries({ queryKey: qk.usage })
  }

  void qc.invalidateQueries({ queryKey: qk.collectionsAll })
  void qc.invalidateQueries({ queryKey: ['collection'] })
  void refreshEventLists(qc)
  // Post lists not on screen (another filter) may now miss or wrongly hold the post. Dropping them makes the
  // next visit load its first page fresh; lists on screen were updated in place.
  qc.removeQueries({ queryKey: qk.postsAll, type: 'inactive' })

  if (postId) {
    // Only mark stale: the post itself was just written from the response.
    void qc.invalidateQueries({ queryKey: ['post', postId], refetchType: 'none' })
  }
}

export function useWebhooks() {
  return useQuery({ queryKey: qk.webhooks, queryFn: async () => (await api.webhooks()).data ?? [] })
}

/** Connected channels (social accounts), every provider. */
export function useChannels(opts?: { enabled?: boolean; refetchInterval?: number }) {
  return useQuery({
    queryKey: qk.channels,
    queryFn: async () => (await api.channels()).data ?? [],
    enabled: opts?.enabled,
    refetchInterval: opts?.refetchInterval,
  })
}

/** Channel groups (Default first) with their channel counts. */
export function useChannelGroups(opts?: { enabled?: boolean }) {
  return useQuery({
    queryKey: qk.channelGroups,
    queryFn: async () => (await api.channelGroups()).data ?? [],
    enabled: opts?.enabled,
  })
}

/** The provider catalog: every platform, available or coming soon, with its fields and limits. */
export function useProviders() {
  return useQuery({
    queryKey: qk.providers,
    queryFn: async () => (await api.providers()).data ?? [],
    staleTime: 10 * 60_000,
  })
}

/** A collection's publishing schedules (or every schedule without a collection id). */
export function useSchedules(collectionId?: string, opts?: { enabled?: boolean }) {
  return useQuery({
    queryKey: qk.schedules(collectionId),
    queryFn: async () => (await api.schedules(collectionId)).data ?? [],
    enabled: opts?.enabled,
  })
}

/** Alerts: Slack, Telegram, Discord, ntfy, email. */
export function useAlerts(opts?: { enabled?: boolean }) {
  return useQuery({
    queryKey: qk.alerts,
    queryFn: async () => (await api.alerts()).data ?? [],
    enabled: opts?.enabled,
  })
}

/** The workspace's post tags, most used first (for tag pickers and filters). */
export function useTags() {
  return useQuery({ queryKey: qk.tags, queryFn: async () => (await api.tags()).data ?? [] })
}

/** The job queue: what waits, runs and failed (Settings → System). */
export function useJobs(status?: string, opts?: { refetchInterval?: number }) {
  return useQuery({
    queryKey: qk.jobs(status),
    queryFn: async () => (await api.jobs({ status: status || undefined, limit: 50 })).data ?? [],
    refetchInterval: opts?.refetchInterval,
  })
}

export function useJobStats(opts?: { refetchInterval?: number }) {
  return useQuery({ queryKey: qk.jobStats, queryFn: api.jobStats, refetchInterval: opts?.refetchInterval })
}

/** People in cloud mode: what the signed-in person may do here (the server enforces it; the UI only hides). */
export function useRole() {
  const session = useSession()
  const role = session.data?.role ?? 'owner'

  return {
    role,
    accountsEnabled: !!session.data?.accounts_enabled,
    /** Signed in with a guest link: no account or workspace settings (Plan & usage only, read-only). */
    guest: !!session.data?.guest,
    /** An owner of this workspace with people (pays, deletes it, adds workspaces); never in local mode. */
    isOwner: !!session.data?.accounts_enabled && role === 'owner',
    canManagePeople: role === 'owner' || role === 'admin',
    canWrite: role !== 'viewer',
    /** The session holds this scope (sections that need it are hidden without it). */
    can: (scope: string) => (session.data?.principal.scopes as string[] | undefined)?.includes(scope) ?? false,
  }
}

/** Opens another workspace: a full reload, so nothing cached for the previous one shows in the next. */
export function useSwitchWorkspace() {
  return useMutation({
    mutationFn: (id: string) => api.switchWorkspace(id),
    onSuccess: () => window.location.assign('/schedule'),
    onError: (e) => toastApiError(e),
  })
}
