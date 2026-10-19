import { type InfiniteData, type QueryClient, replaceEqualDeep } from '@tanstack/react-query'

import type { Media, MediaLibraryItem, MediaPage, MediaQuery } from '@/lib/api'
import { mediaKind } from '@/lib/media'
import { qk } from '@/lib/queries'

/**
 * The media library's cache. Mutations write their answer into the cached lists (the library's paged lists and
 * the post composer's picker) instead of invalidating `['media']`, which would reload every loaded page of every
 * list, the folder tree and the picker. Only the folder tree (its counts) and the plan usage are refetched.
 */
export const mediaKeys = {
  folders: ['media', 'folders'] as const,
  libraries: ['media', 'library'] as const,
  library: (query: MediaQuery) => ['media', 'library', query] as const,
  pickers: ['media', 'picker'] as const,
  picker: (kind: 'image' | 'video', q: string) => ['media', 'picker', kind, q] as const,
}

type Library = InfiniteData<MediaPage, number>

/**
 * Structural sharing by id (like `sharePages` for posts): a file added or removed locally shifts every later
 * tile, and TanStack's default compares by index, so every later tile would get a fresh copy and re-render.
 */
export function shareMediaPages(oldData: unknown, newData: unknown): unknown {
  const prev = oldData as Library | undefined
  const next = newData as Library | undefined

  if (!prev?.pages || !next?.pages) {
    return replaceEqualDeep(oldData, newData)
  }

  const byId = new Map(prev.pages.flatMap((p) => p.data.map((m) => [m.id, m] as const)))
  const pages = next.pages.map((page, i) => {
    const data = page.data.map((m) => {
      const old = byId.get(m.id)

      return old ? replaceEqualDeep(old, m) : m
    })
    const before = prev.pages[i]
    const same =
      before !== undefined &&
      before.next_offset === page.next_offset &&
      before.data.length === data.length &&
      data.every((m, j) => m === before.data[j])

    return same ? before : { ...page, data }
  })
  const pageParams = replaceEqualDeep(prev.pageParams, next.pageParams)
  const unchanged =
    pageParams === prev.pageParams && pages.length === prev.pages.length && pages.every((p, i) => p === prev.pages[i])

  return unchanged ? prev : { pages, pageParams }
}

/** Whether a file belongs in a list: yes, no, or undefined when only the server can tell (a search, duplicates). */
export function fitsQuery(query: MediaQuery, m: MediaLibraryItem): boolean | undefined {
  if (!!query.deleted !== !!m.deleted_at) {
    return false
  }

  if (query.kind && mediaKind(m.mime_type) !== query.kind) {
    return false
  }

  if (query.folder && m.folder_id !== (query.folder === 'root' ? '' : query.folder)) {
    return false
  }

  if (query.tag && !m.tags.includes(query.tag)) {
    return false
  }

  if ((query.used === 'unused' && m.uses > 0) || (query.used === 'used' && m.uses === 0)) {
    return false
  }

  if (query.q || query.duplicates) {
    return undefined
  }

  return true
}

function libraries(qc: QueryClient) {
  return qc
    .getQueriesData<Library>({ queryKey: mediaKeys.libraries })
    .flatMap(([key, data]) => (data?.pages ? [{ key, query: (key[2] ?? {}) as MediaQuery, data }] : []))
}

/** The files of these ids found in any cached list (to keep their use counts when they come back). */
export function findMedia(qc: QueryClient, ids: string[]) {
  const want = new Set(ids)
  const found = new Map<string, MediaLibraryItem>()

  for (const { data } of libraries(qc)) {
    for (const m of data.pages.flatMap((p) => p.data)) {
      if (want.has(m.id)) {
        found.set(m.id, m)
      }
    }
  }

  return found
}

/** Shifts where the next page starts by the files added (or removed) in the loaded part. */
function shiftNext(pages: MediaPage[], by: number) {
  const last = pages.at(-1)

  if (!by || last?.next_offset === undefined || last.next_offset === null) {
    return pages
  }

  return [...pages.slice(0, -1), { ...last, next_offset: Math.max(0, last.next_offset + by) }]
}

/** Puts a file into a list newest first: where it falls among the loaded files, or nowhere if it is older. */
function insertSorted(data: Library, m: MediaLibraryItem): Library {
  let pages = data.pages.map((p) =>
    p.data.some((x) => x.id === m.id) ? { ...p, data: p.data.filter((x) => x.id !== m.id) } : p,
  )
  const removed = data.pages.reduce((n, p) => n + p.data.length, 0) - pages.reduce((n, p) => n + p.data.length, 0)
  const at = pages.findIndex((p) => p.data.some((x) => x.created_at < m.created_at))

  if (at >= 0) {
    const page = pages[at]
    const j = page.data.findIndex((x) => x.created_at < m.created_at)

    pages = pages.map((p, i) => (i === at ? { ...p, data: [...p.data.slice(0, j), m, ...p.data.slice(j)] } : p))
  } else if (pages.at(-1)?.next_offset === undefined || pages.at(-1)?.next_offset === null) {
    // The whole list is loaded: the file goes last.
    pages = pages.map((p, i) => (i === pages.length - 1 ? { ...p, data: [...p.data, m] } : p))
  } else {
    // Older than everything loaded: the next page brings it.
    return { ...data, pages: shiftNext(pages, -removed) }
  }

  return { ...data, pages: shiftNext(pages, 1 - removed) }
}

function refetchLists(qc: QueryClient, keys: readonly (readonly unknown[])[]) {
  for (const queryKey of keys) {
    void qc.invalidateQueries({ queryKey, exact: true })
  }
}

/** Library lists matching `pred` may gain files only the server knows about: they reload (now if in view). */
export function invalidateLists(qc: QueryClient, pred: (query: MediaQuery) => boolean) {
  void qc.invalidateQueries({
    queryKey: mediaKeys.libraries,
    predicate: (q) => pred((q.queryKey[2] ?? {}) as MediaQuery),
  })
}

/** The folder tree (its counts and tags) and the plan's storage. */
export function refreshMediaCounts(qc: QueryClient) {
  void qc.invalidateQueries({ queryKey: mediaKeys.folders, exact: true })
  void qc.invalidateQueries({ queryKey: qk.usage })
}

/** New (uploaded) or restored files: into every cached list they belong to, newest first. */
export function insertMedia(qc: QueryClient, items: MediaLibraryItem[]) {
  const stale: (readonly unknown[])[] = []

  for (const { key, query, data } of libraries(qc)) {
    let next = data

    for (const m of items) {
      const fits = fitsQuery(query, m)

      if (fits === false) {
        continue
      }

      if (fits === undefined || (query.sort ?? 'created_desc') !== 'created_desc') {
        stale.push(key)
        next = data

        break
      }

      next = insertSorted(next, m)
    }

    if (next !== data) {
      qc.setQueryData<Library>(key, next)
    }
  }

  for (const [key, data] of qc.getQueriesData<MediaPage>({ queryKey: mediaKeys.pickers })) {
    const [, , kind, q] = key as [string, string, string, string]
    const fresh = items.filter((m) => !m.deleted_at && mediaKind(m.mime_type) === kind)

    if (!data || !fresh.length) {
      continue
    }

    if (q) {
      stale.push(key)
      continue
    }

    const ids = new Set(fresh.map((m) => m.id))

    qc.setQueryData<MediaPage>(key, { ...data, data: [...fresh, ...data.data.filter((m) => !ids.has(m.id))] })
  }

  refetchLists(qc, stale)
}

/** Files gone from lists (deleted, purged, restored out of Deleted): from the lists `where` picks (default all). */
export function removeMedia(qc: QueryClient, ids: string[], where: (query: MediaQuery) => boolean = () => true) {
  const gone = new Set(ids)

  for (const { key, query, data } of libraries(qc)) {
    if (!where(query) || !data.pages.some((p) => p.data.some((m) => gone.has(m.id)))) {
      continue
    }

    let removed = 0
    const pages = data.pages.map((p) => {
      const kept = p.data.filter((m) => !gone.has(m.id))

      removed += p.data.length - kept.length

      return kept.length === p.data.length ? p : { ...p, data: kept }
    })

    qc.setQueryData<Library>(key, { ...data, pages: shiftNext(pages, -removed) })
  }

  for (const [key, data] of qc.getQueriesData<MediaPage>({ queryKey: mediaKeys.pickers })) {
    if (data?.data.some((m) => gone.has(m.id))) {
      qc.setQueryData<MediaPage>(key, { ...data, data: data.data.filter((m) => !gone.has(m.id)) })
    }
  }
}

/**
 * Changed files (moved, tagged, renamed): patched in every cached list, and taken out of the lists they no
 * longer belong to. Lists they may now belong to are the caller's to reload (`invalidateLists`).
 */
export function patchMedia(qc: QueryClient, ids: string[], patch: (m: MediaLibraryItem) => MediaLibraryItem) {
  const want = new Set(ids)

  for (const { key, query, data } of libraries(qc)) {
    if (!data.pages.some((p) => p.data.some((m) => want.has(m.id)))) {
      continue
    }

    let removed = 0
    const pages = data.pages.map((p) => {
      const next = p.data.flatMap((m) => {
        if (!want.has(m.id)) {
          return [m]
        }

        const changed = patch(m)

        if (fitsQuery(query, changed) === false) {
          removed++

          return []
        }

        return [changed]
      })

      return { ...p, data: next }
    })

    qc.setQueryData<Library>(key, { ...data, pages: shiftNext(pages, -removed) })
  }

  for (const [key, data] of qc.getQueriesData<MediaPage>({ queryKey: mediaKeys.pickers })) {
    if (data?.data.some((m) => want.has(m.id))) {
      qc.setQueryData<MediaPage>(key, { ...data, data: data.data.map((m) => (want.has(m.id) ? patch(m) : m)) })
    }
  }
}

/** Files back from Deleted: out of the Deleted lists, into the others (with the use counts known before). */
export function restoreInCache(
  qc: QueryClient,
  restored: Media[],
  known = findMedia(
    qc,
    restored.map((m) => m.id),
  ),
) {
  const ids = restored.map((m) => m.id)

  removeMedia(qc, ids, (query) => !!query.deleted)
  insertMedia(
    qc,
    restored.map((m) => ({ ...m, deleted_at: undefined, purge_at: undefined, uses: known.get(m.id)?.uses ?? 0 })),
  )
  // A file back from Deleted may have twins again.
  invalidateLists(qc, (query) => !!query.duplicates)
  refreshMediaCounts(qc)
}

/** Files moved to Deleted: out of the library lists; the Deleted lists and the duplicates reload. */
export function trashInCache(qc: QueryClient, ids: string[]) {
  removeMedia(qc, ids, (query) => !query.deleted)
  invalidateLists(qc, (query) => !!query.deleted || !!query.duplicates)
  refreshMediaCounts(qc)
}

/** A folder deleted: its files move up to its parent (its own list goes), and the parent's lists reload. */
export function folderGoneInCache(qc: QueryClient, folder: { id: string; parent_id: string }) {
  const ids = libraries(qc).flatMap(({ data }) =>
    data.pages.flatMap((p) => p.data.filter((m) => m.folder_id === folder.id).map((m) => m.id)),
  )

  patchMedia(qc, ids, (m) => ({ ...m, folder_id: folder.parent_id }))
  qc.removeQueries({
    queryKey: mediaKeys.libraries,
    predicate: (q) => ((q.queryKey[2] ?? {}) as MediaQuery).folder === folder.id,
  })
  invalidateLists(qc, (query) => query.folder === (folder.parent_id || 'root'))
  void qc.invalidateQueries({ queryKey: mediaKeys.folders, exact: true })
}
