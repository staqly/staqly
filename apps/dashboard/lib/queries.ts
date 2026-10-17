'use client'

import {
  replaceEqualDeep,
  useInfiniteQuery,
  useQuery,
  useQueryClient,
  type InfiniteData,
  type QueryClient,
  keepPreviousData,
} from '@tanstack/react-query'
import { useEffect, useMemo, useRef } from 'react'

import {
  api,
  setCsrfToken,
  type ActionDef,
  type Collection,
  type EventPage,
  type EventQuery,
  type Item,
  type ItemPage,
  type ItemQuery,
  type Mode,
} from './api'

export const qk = {
  setup: ['setup'] as const,
  session: ['session'] as const,
  sessions: ['sessions'] as const,
  collections: (includeArchived = false) => ['collections', { includeArchived }] as const,
  collectionsAll: ['collections'] as const,
  collection: (id: string) => ['collection', id] as const,
  items: (query: ItemQuery) => ['items', query] as const,
  itemsAll: ['items'] as const,
  item: (id: string) => ['item', id] as const,
  itemActions: (id: string) => ['item', id, 'actions'] as const,
  itemEvents: (id: string) => ['item', id, 'events'] as const,
  itemVersions: (id: string) => ['item', id, 'versions'] as const,
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
  usage: ['usage'] as const,
  watchers: ['watchers'] as const,
  channels: ['notification-channels'] as const,
  channelDeliveries: (id: string) => ['notification-channels', id, 'deliveries'] as const,
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
  return useQuery({ queryKey: qk.billing, queryFn: () => api.billing(), enabled: opts?.enabled })
}

export function usePlans(opts?: { enabled?: boolean }) {
  return useQuery({
    queryKey: qk.plans,
    queryFn: async () => (await api.plans()).data ?? [],
    staleTime: 10 * 60_000,
    enabled: opts?.enabled,
  })
}

/** Current plan, period and usage counters of the signed-in workspace. */
export function useUsage() {
  return useQuery({ queryKey: qk.usage, queryFn: api.usage, staleTime: 15_000 })
}

/**
 * Who gets Settings -> Backups (the tab and the page): every local install (`local`); in the cloud, paid plans
 * (`paid`) but not Free (`free`). `undefined` while the cloud plan is still loading.
 */
export function useBackupAccess(): 'local' | 'paid' | 'free' | undefined {
  const { isCloud } = useMode()
  const { data: plan } = useQuery({
    queryKey: qk.usage,
    queryFn: api.usage,
    staleTime: 15_000,
    enabled: isCloud,
    select: (u) => u.plan?.id,
  })

  if (!isCloud) {
    return 'local'
  }

  return plan ? (plan === 'free' ? 'free' : 'paid') : undefined
}

/**
 * The collection list. `select` picks out the part a component shows: it then renders only when that part
 * changes, not on every pending count change (the list is refetched after each decision).
 */
export function useCollections<T = Collection[]>(includeArchived = false, select?: (collections: Collection[]) => T) {
  return useQuery({
    queryKey: qk.collections(includeArchived),
    queryFn: async () => (await api.collections({ includeArchived })).data ?? [],
    select,
  })
}

export function useCollection(id: string) {
  return useQuery({ queryKey: qk.collection(id), queryFn: () => api.collection(id), enabled: !!id })
}

/** A cursor page of anything with an id (items, events). */
interface IdPage {
  data: { id: string }[]
  next_cursor?: string | null
}

/**
 * Structural sharing by item id instead of array index. Removing an item locally (approve, delete) shifts
 * every later item, and TanStack's default `replaceEqualDeep` compares by index, so it would hand every
 * later row a fresh copy and re-render it. Here unchanged items, pages and the page list keep their identity,
 * so memoised rows only re-render when their own item changed.
 */
export function shareItemPages(oldData: unknown, newData: unknown): unknown {
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
 * Keep only the first page of a cached item list nobody is showing any more. Refetching an infinite query
 * reloads every page it holds, one request after the other (62 for 3000 gallery cards), so coming back to
 * a list (revisit, cleared search or filter) would replay them all. Trimmed, it costs one request; "Load
 * more" brings the rest back. The timestamp is kept so staleness is unchanged.
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

export function useItemsInfinite(
  query: ItemQuery,
  opts?: { enabled?: boolean; refetchInterval?: number; refetchOnWindowFocus?: boolean },
) {
  const queryKey = qk.items(query)

  useTrimWhenUnwatched(queryKey)

  return useInfiniteQuery({
    queryKey,
    queryFn: ({ pageParam, signal }) => api.items({ ...query, cursor: pageParam || undefined }, signal),
    initialPageParam: '',
    getNextPageParam: (last) => last.next_cursor || undefined,
    enabled: opts?.enabled,
    refetchInterval: opts?.refetchInterval,
    refetchOnWindowFocus: opts?.refetchOnWindowFocus,
    structuralSharing: shareItemPages,
    // A new search or filter keeps showing the previous results (check `isPlaceholderData`) instead of
    // flashing the loading skeleton.
    placeholderData: keepPreviousData,
  })
}

/**
 * The newest workspace events: one small request whose top entry changes whenever any item is created,
 * decided, edited, expired or deleted. Long paged lists poll this instead of refetching every loaded page.
 */
export function useRecentEvents(opts?: { refetchInterval?: number }) {
  return useQuery({
    queryKey: qk.recentEvents,
    queryFn: async () => (await api.events({ limit: 20 })).data ?? [],
    refetchInterval: opts?.refetchInterval,
  })
}

/**
 * Item ids this tab changed itself in the last `ttlMs`. Their events are not a reason to refetch a list; a
 * later change to the same item (an agent edits it, another reviewer decides it) is. Entries expire, so the
 * set stays small however many items a long session touches.
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

// Items whose change this tab already wrote into the cache (syncItemInCache / removeItemFromLists).
const appliedHere = createRecentIds()

/**
 * Refetch a paged list only when the workspace changed in a way this tab has not applied itself: polls the
 * newest events (one small request; also refetched on window focus) instead of the list's pages.
 * `isLocal` marks more item ids as handled here (e.g. the inbox's bulk decisions).
 */
export function useRefetchOnForeignChange(
  refetch: () => unknown,
  opts?: { refetchInterval?: number; isLocal?: (itemId: string) => boolean },
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
    // All fetched events are new: there may be more we did not see, so assume a foreign change.
    const foreign =
      unseen.length === events.length ||
      unseen.some((e) => !e.item_id || !(appliedHere.has(e.item_id) || isLocal?.(e.item_id)))

    if (foreign) {
      void refetch()
    }
  }, [recentEvents.data, refetch, isLocal])
}

export function useItem(id: string) {
  return useQuery({ queryKey: qk.item(id), queryFn: () => api.item(id), enabled: !!id })
}

// Mirrors builtinActions in apps/api/internal/app/collections.go (GET /v1/items/{id}/actions lists them first).
const BUILTIN_ACTIONS: ActionDef[] = [
  {
    name: 'approve',
    label: 'Approve',
    event: 'item.approved',
    set_status: 'approved',
    allowed_statuses: ['pending'],
    requires_feedback: false,
    builtin: true,
  },
  {
    name: 'reject',
    label: 'Reject',
    event: 'item.rejected',
    set_status: 'rejected',
    allowed_statuses: ['pending'],
    requires_feedback: false,
    builtin: true,
  },
]

/**
 * What GET /v1/items/{id}/actions returns for a dashboard user: built-ins, then the collection's custom
 * actions, filtered by the item's status. Sessions and local mode hold every scope, so no scope filter.
 */
export function actionsFor(collection: Pick<Collection, 'actions'>, status: Item['status']): ActionDef[] {
  // While an automation works on an item nothing else runs (the API refuses it): it reports a result first.
  if (status === 'processing') {
    return []
  }

  return [...BUILTIN_ACTIONS, ...(collection.actions ?? [])].filter(
    (a) =>
      (!a.allowed_statuses?.length || a.allowed_statuses.includes(status)) &&
      // An action that would set the status the item already has does nothing; the API refuses it.
      (!a.set_status || a.set_status !== status),
  )
}

/**
 * Actions for an item, computed from the (already cached) collection list and the item's status, so moving
 * through the inbox with j/k costs no request. Falls back to the API when the list has no such collection
 * or failed to load.
 */
export function useItemActions(item: Pick<Item, 'id' | 'collection_id' | 'status' | 'version'> | undefined) {
  // Same list (archived included) as the shell and the inbox, so this adds no request of its own.
  const collections = useCollections(true)
  const col = item ? collections.data?.find((c) => c.id === item.collection_id) : undefined
  const status = item?.status
  // Memoised before the query hook: computed in the early return below, it was a new array on every render,
  // which re-rendered the inbox preview and its buttons on any unrelated state change.
  const local = useMemo(() => (col && status ? actionsFor(col, status) : undefined), [col, status])
  const fetched = useQuery({
    queryKey: [...qk.itemActions(item?.id ?? ''), item?.version ?? 0],
    queryFn: async () => (await api.itemActions(item!.id)).data ?? [],
    enabled: !!item && !col && !collections.isPending,
    staleTime: 5 * 60_000,
  })

  if (local) {
    return { data: local, isLoading: false }
  }

  return { data: fetched.data, isLoading: !!item && (collections.isPending || fetched.isLoading) }
}

/**
 * The activity feed. Like item lists: shared by event id (a new event at the top doesn't re-render every
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
    structuralSharing: shareItemPages,
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

/** Flatten infinite item pages. */
export function flattenItems(data?: InfiniteData<ItemPage>): Item[] {
  return data?.pages.flatMap((p) => p.data) ?? []
}

/** Write an updated item everywhere it is cached (detail + any list). */
export function syncItemInCache(qc: QueryClient, item: Item) {
  appliedHere.add(item.id)
  qc.setQueryData(qk.item(item.id), item)
  qc.setQueriesData<InfiniteData<ItemPage>>({ queryKey: qk.itemsAll }, (old) =>
    old
      ? {
          ...old,
          pages: old.pages.map((p) => ({ ...p, data: p.data.map((it) => (it.id === item.id ? item : it)) })),
        }
      : old,
  )
}

/**
 * After this tab created an item: reload item lists from their first page. Invalidating them as they are
 * would refetch every loaded page one by one (and the new item's event would trigger a second round in
 * useRefetchOnForeignChange, so it is marked as applied here).
 */
export function refetchItemListsFromStart(qc: QueryClient, createdId?: string) {
  if (createdId) {
    appliedHere.add(createdId)
  }

  for (const q of qc.getQueryCache().findAll({ queryKey: qk.itemsAll })) {
    trimUnwatchedList(qc, q.queryKey, { watched: true })
  }

  void qc.invalidateQueries({ queryKey: qk.itemsAll })
}

/** Remove an item from all cached lists (e.g. after delete). */
export function removeItemFromLists(qc: QueryClient, id: string) {
  appliedHere.add(id)
  qc.setQueriesData<InfiniteData<ItemPage>>({ queryKey: qk.itemsAll }, (old) =>
    old ? { ...old, pages: old.pages.map((p) => ({ ...p, data: p.data.filter((it) => it.id !== id) })) } : old,
  )
}

/**
 * Refresh everything that depends on item state after a mutation. Item lists are not refetched: callers
 * write the server's response into the cache with `syncItemInCache` / `removeItemFromLists` instead.
 * `usage`: only creating or deleting items/media changes the plan counters; decisions and edits don't.
 */
export function invalidateAfterItemChange(qc: QueryClient, itemId?: string, opts?: { usage?: boolean }) {
  if (opts?.usage) {
    void qc.invalidateQueries({ queryKey: qk.usage })
  }

  void qc.invalidateQueries({ queryKey: qk.collectionsAll })
  void qc.invalidateQueries({ queryKey: ['collection'] })
  void refreshEventLists(qc)
  // The inbox step counts, so a decided item's step updates right away.
  void qc.invalidateQueries({ queryKey: ['flow'] })
  // Item lists not on screen (another inbox tab, another filter) may now miss or wrongly hold the item.
  // Dropping them makes the next visit load its first page fresh; lists on screen were updated in place.
  qc.removeQueries({ queryKey: qk.itemsAll, type: 'inactive' })

  if (itemId) {
    // Only mark stale: the item itself was just written from the response, and its actions, events and
    // versions are keyed by version, so the new version fetches them anyway. Refetching here would load the
    // item again and the previous version's actions (e.g. for an inbox item that was just approved).
    void qc.invalidateQueries({ queryKey: ['item', itemId], refetchType: 'none' })
  }
}

export function useWatchers(opts?: { refetchInterval?: number }) {
  return useQuery({
    queryKey: qk.watchers,
    queryFn: async () => (await api.watchers()).data ?? [],
    refetchInterval: opts?.refetchInterval,
  })
}

export function useChannels(opts?: { enabled?: boolean }) {
  return useQuery({
    queryKey: qk.channels,
    queryFn: async () => (await api.channels()).data ?? [],
    enabled: opts?.enabled,
  })
}

/** The workspace's item tags, most used first (for tag pickers and filters). */
export function useTags() {
  return useQuery({ queryKey: qk.tags, queryFn: async () => (await api.tags()).data ?? [] })
}
