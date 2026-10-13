"use client";

import { useInfiniteQuery, useQuery, type InfiniteData, type QueryClient } from "@tanstack/react-query";
import { api, setCsrfToken, type EventQuery, type Mode, type Item, type ItemPage, type ItemQuery } from "./api";

export const qk = {
  setup: ["setup"] as const,
  session: ["session"] as const,
  sessions: ["sessions"] as const,
  collections: (includeArchived = false) => ["collections", { includeArchived }] as const,
  collectionsAll: ["collections"] as const,
  collection: (id: string) => ["collection", id] as const,
  items: (query: ItemQuery) => ["items", query] as const,
  itemsAll: ["items"] as const,
  item: (id: string) => ["item", id] as const,
  itemActions: (id: string) => ["item", id, "actions"] as const,
  itemEvents: (id: string) => ["item", id, "events"] as const,
  itemVersions: (id: string) => ["item", id, "versions"] as const,
  events: (query: EventQuery) => ["events", query] as const,
  eventsAll: ["events"] as const,
  apiKeys: ["api-keys"] as const,
  webhooks: ["webhooks"] as const,
  deliveries: (webhookId?: string) => ["deliveries", webhookId ?? "all"] as const,
  health: ["health"] as const,
  plans: ["plans"] as const,
  usage: ["usage"] as const,
  rules: ["rules"] as const,
  watchers: ["watchers"] as const,
  watcherPresets: ["watcher-presets"] as const,
  channels: ["notification-channels"] as const,
  channelDeliveries: (id: string) => ["notification-channels", id, "deliveries"] as const,
  workspaceExport: (id: string) => ["workspace-export", id] as const,
  readiness: ["readyz"] as const,
};

export function useSetupStatus() {
  return useQuery({ queryKey: qk.setup, queryFn: api.setupStatus, staleTime: 30_000 });
}

async function sessionQueryFn() {
  const s = await api.session();
  setCsrfToken(s.csrf_token ?? "");
  return s;
}

export function useSession() {
  return useQuery({
    queryKey: qk.session,
    queryFn: sessionQueryFn,
    staleTime: 60_000,
    retry: false,
  });
}

/**
 * Server mode. Older servers did not report it and behaved like cloud mode.
 * The session (loaded by the auth gate) is authoritative; setup status is the fallback.
 */
export function useMode() {
  const session = useQuery({ queryKey: qk.session, queryFn: sessionQueryFn, enabled: false, staleTime: 60_000 });
  const setup = useSetupStatus();
  const mode: Mode = session.data?.mode ?? setup.data?.mode ?? "cloud";
  return { mode, isLocal: mode === "local", isCloud: mode !== "local" };
}

/** Public cloud plans (not including the local "unlocked" plan). */
export function usePlans(opts?: { enabled?: boolean }) {
  return useQuery({
    queryKey: qk.plans,
    queryFn: async () => (await api.plans()).data ?? [],
    staleTime: 10 * 60_000,
    enabled: opts?.enabled,
  });
}

/** Current plan, period and usage counters of the signed-in workspace. */
export function useUsage() {
  return useQuery({ queryKey: qk.usage, queryFn: api.usage, staleTime: 15_000 });
}

export function useCollections(includeArchived = false) {
  return useQuery({
    queryKey: qk.collections(includeArchived),
    queryFn: async () => (await api.collections({ includeArchived })).data ?? [],
  });
}

export function useCollection(id: string) {
  return useQuery({ queryKey: qk.collection(id), queryFn: () => api.collection(id), enabled: !!id });
}

export function useItemsInfinite(query: ItemQuery, opts?: { enabled?: boolean; refetchInterval?: number }) {
  return useInfiniteQuery({
    queryKey: qk.items(query),
    queryFn: ({ pageParam, signal }) => api.items({ ...query, cursor: pageParam || undefined }, signal),
    initialPageParam: "",
    getNextPageParam: (last) => last.next_cursor || undefined,
    enabled: opts?.enabled,
    refetchInterval: opts?.refetchInterval,
  });
}

export function useItem(id: string) {
  return useQuery({ queryKey: qk.item(id), queryFn: () => api.item(id), enabled: !!id });
}

export function useItemActions(id: string | undefined, version?: number) {
  return useQuery({
    queryKey: [...qk.itemActions(id ?? ""), version ?? 0],
    queryFn: async () => (await api.itemActions(id!)).data ?? [],
    enabled: !!id,
    staleTime: 10_000,
  });
}

export function useEventsInfinite(query: EventQuery) {
  return useInfiniteQuery({
    queryKey: qk.events(query),
    queryFn: ({ pageParam }) => api.events({ ...query, cursor: pageParam || undefined }),
    initialPageParam: "",
    getNextPageParam: (last) => last.next_cursor || undefined,
  });
}

/** Flatten infinite item pages. */
export function flattenItems(data?: InfiniteData<ItemPage>): Item[] {
  return data?.pages.flatMap((p) => p.data) ?? [];
}

/** Write an updated item everywhere it is cached (detail + any list). */
export function syncItemInCache(qc: QueryClient, item: Item) {
  qc.setQueryData(qk.item(item.id), item);
  qc.setQueriesData<InfiniteData<ItemPage>>({ queryKey: qk.itemsAll }, (old) =>
    old
      ? {
          ...old,
          pages: old.pages.map((p) => ({ ...p, data: p.data.map((it) => (it.id === item.id ? item : it)) })),
        }
      : old,
  );
}

/** Remove an item from all cached lists (e.g. after delete). */
export function removeItemFromLists(qc: QueryClient, id: string) {
  qc.setQueriesData<InfiniteData<ItemPage>>({ queryKey: qk.itemsAll }, (old) =>
    old ? { ...old, pages: old.pages.map((p) => ({ ...p, data: p.data.filter((it) => it.id !== id) })) } : old,
  );
}

/** Refresh everything that depends on item state after a mutation. */
export function invalidateAfterItemChange(qc: QueryClient, itemId?: string) {
  void qc.invalidateQueries({ queryKey: qk.usage });
  void qc.invalidateQueries({ queryKey: qk.collectionsAll });
  void qc.invalidateQueries({ queryKey: ["collection"] });
  void qc.invalidateQueries({ queryKey: qk.eventsAll });
  if (itemId) void qc.invalidateQueries({ queryKey: ["item", itemId] });
}

export function useRules() {
  return useQuery({ queryKey: qk.rules, queryFn: async () => (await api.rules()).data ?? [] });
}

export function useWatchers(opts?: { refetchInterval?: number }) {
  return useQuery({
    queryKey: qk.watchers,
    queryFn: async () => (await api.watchers()).data ?? [],
    refetchInterval: opts?.refetchInterval,
  });
}

/** The preset catalog is static per server version. */
export function useWatcherPresets() {
  return useQuery({
    queryKey: qk.watcherPresets,
    queryFn: async () => (await api.watcherPresets()).data ?? [],
    staleTime: 60 * 60_000,
  });
}

export function useChannels(opts?: { enabled?: boolean }) {
  return useQuery({
    queryKey: qk.channels,
    queryFn: async () => (await api.channels()).data ?? [],
    enabled: opts?.enabled,
  });
}
