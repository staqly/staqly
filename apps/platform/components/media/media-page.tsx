'use client'

import { useInfiniteQuery, useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import {
  AudioLines,
  ChevronDown,
  CircleAlert,
  Download,
  Film,
  FolderInput,
  FolderPlus,
  Hash,
  Loader2,
  Clapperboard,
  Images,
  MoreHorizontal,
  PenSquare,
  RotateCw,
  Search,
  Sparkle,
  Trash2,
  Undo2,
  Palette,
  Upload,
  X,
} from 'lucide-react'
import dynamic from 'next/dynamic'
import { Fragment, useCallback, useEffect, useMemo, useRef, useState } from 'react'
import { toast } from 'sonner'

import { Callout } from '@/components/common/callout'
import { ConfirmDialog } from '@/components/common/confirm-dialog'
import { EmptyState, ErrorState, LoadMore } from '@/components/common/error-state'
import { FilterButton } from '@/components/common/filter-button'
import { SkeletonList } from '@/components/common/skeleton-list'
import { stackCarousels, useCarousels } from '@/components/media/carousels'
import {
  folderGoneInCache,
  invalidateLists,
  mediaKeys,
  patchMedia,
  refreshMediaCounts,
  removeMedia,
  restoreInCache,
  shareMediaPages,
} from '@/components/media/media-cache'
import {
  folderAndBelow,
  folderPath,
  isExternalFileDrag,
  LIBRARY,
  LibraryPane,
  LibrarySheet,
  type Place,
  placeLabel,
  ROOT,
} from '@/components/media/media-folders'
import { MediaGrid } from '@/components/media/media-grid'
import { deletedToast } from '@/components/media/media-trash'
import { MEDIA_ACCEPT, useMediaUpload, type UploadItem } from '@/components/media/use-media-upload'
import { Button } from '@/components/ui/button'
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuLabel,
  DropdownMenuSeparator,
  DropdownMenuTrigger,
} from '@/components/ui/dropdown-menu'
import { Input } from '@/components/ui/input'
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from '@/components/ui/select'
import { useLazyDialog } from '@/hooks/use-lazy-dialog'
import { useAiStatus } from '@/lib/ai-status'
import {
  api,
  ApiError,
  type Media,
  mediaUrl,
  type MediaBulk,
  type MediaFolder,
  type MediaKind,
  type MediaLibraryItem,
  type MediaQuery,
  type MediaSort,
} from '@/lib/api'
import { formatBytes, timeAgo } from '@/lib/format'
import { toastApiError } from '@/lib/notify'
import { useRole } from '@/lib/queries'
import { cn } from '@/lib/utils'

const Composer = dynamic(() => import('@/components/posts/composer').then((m) => m.Composer), { ssr: false })
// The drawer and the dialogs load when first needed, so the library itself opens fast.
const MediaDetail = dynamic(() => import('@/components/media/media-detail').then((m) => m.MediaDetail), { ssr: false })
const FolderPickerDialog = dynamic(
  () => import('@/components/media/media-folder-dialogs').then((m) => m.FolderPickerDialog),
  { ssr: false },
)
const FolderNameDialog = dynamic(
  () => import('@/components/media/media-folder-dialogs').then((m) => m.FolderNameDialog),
  { ssr: false },
)
const AiVideoDialog = dynamic(() => import('@/components/media/ai-video-dialog').then((m) => m.AiVideoDialog), {
  ssr: false,
})
const TagsDialog = dynamic(() => import('@/components/media/media-tags-dialog').then((m) => m.TagsDialog), {
  ssr: false,
})

const PAGE = 60
const WEEK = 7 * 24 * 3600 * 1000

/** Each library view: the line under its title and what it says while empty (upload: offers Upload files). */
const VIEWS: Record<string, { about: string; title: string; description: string; upload?: boolean }> = {
  all: {
    about: 'Pictures, videos and audio for your posts. Drop or paste files to upload.',
    title: 'No media yet',
    description:
      'Upload pictures, videos and audio once and use them in any post. Drop files here, paste them or pick them.',
    upload: true,
  },
  recent: {
    about: 'Files uploaded in the last 7 days, newest first.',
    title: 'Nothing uploaded this week',
    description: 'Files you upload show up here for 7 days, so the latest ones are easy to find.',
    upload: true,
  },
  image: {
    about: 'Pictures and GIFs in the library.',
    title: 'No images yet',
    description: 'Upload pictures or GIFs once and use them in any post.',
    upload: true,
  },
  video: {
    about: 'Videos in the library.',
    title: 'No videos yet',
    description: 'Upload a video once and use it in any post.',
    upload: true,
  },
  audio: {
    about: 'Audio files in the library.',
    title: 'No audio yet',
    description: 'Upload music, voice-overs or podcast clips once and use them in any post.',
    upload: true,
  },
  unused: {
    about: 'Files no post uses. Safe to clean up.',
    title: 'Nothing unused',
    description: 'Every file in the library is used in at least one post.',
  },
  duplicates: {
    about: 'The same file uploaded more than once. Keep one copy, the rest move to Deleted.',
    title: 'No duplicates',
    description: 'Every file in the library is uploaded only once.',
  },
  deleted: {
    about: 'Files you deleted. Restore one to bring it back.',
    title: 'Nothing deleted',
    description: 'Files you delete wait here for 30 days, so you can restore them before they go for good.',
  },
  [ROOT]: {
    about: 'Files in no folder.',
    title: 'No files outside folders',
    description: 'Every file is in a folder. Drop files on / to take them out.',
  },
}
const KINDS: Place[] = ['image', 'video', 'audio']
const NO_FOLDERS: MediaFolder[] = []
// Never mutated: every change to a selection makes a new set.
const NO_IDS = new Set<string>()
const NO_ITEMS: MediaLibraryItem[] = []
const NO_STACKS = new Map<string, string[]>()
const PURGE_TOAST = 'media-purge'

const CREATE = [
  {
    href: '/media/edit',
    label: 'Design',
    hint: 'A post graphic or a carousel',
    icon: <Palette />,
    testId: 'media-design',
  },
  {
    href: '/media/video',
    label: 'Video',
    hint: 'Pictures, clips, captions and music',
    icon: <Film />,
    testId: 'media-video',
  },
  {
    href: '/media/audio',
    label: 'Sound',
    hint: 'Trim, fade and join sound clips',
    icon: <AudioLines />,
    testId: 'media-audio',
  },
]

// Action bar labels show from sm up; below, icon buttons keep their names (and tooltips) through aria-label.
const barLabel = (text: string) => <span className="hidden sm:inline">{text}</span>

const files = (n: number) => `${n} ${n === 1 ? 'file' : 'files'}`

/** Downloads files by id; the server names each one (its Content-Disposition), so a carousel's hidden pages too. */
function download(ids: string[]) {
  for (const id of ids) {
    const a = document.createElement('a')

    a.href = mediaUrl(id, { download: true })
    a.download = ''
    a.click()
  }
}

type FolderDialog = { mode: 'new'; parent: string } | { mode: 'rename'; folder: MediaFolder }

/** What the folder picker moves: some files, or a folder. */
type Moving = { ids: string[]; current?: string } | { folder: MediaFolder }

/**
 * The media library: every picture, video and file of the workspace, to upload, find, organize in folders and
 * tags, describe and post. The library sidebar on the left (a sheet on phones), the files on the right.
 */
/** A folder and its parents, outermost first. */
function folderChain(folders: { id: string; name: string; parent_id: string }[], id: string) {
  const byId = new Map(folders.map((f) => [f.id, f]))
  const chain: { id: string; name: string }[] = []

  for (let f = byId.get(id); f && chain.length < 12; f = byId.get(f.parent_id)) {
    chain.unshift({ id: f.id, name: f.name })
  }

  return chain
}

export function MediaPage() {
  const qc = useQueryClient()
  const { canWrite } = useRole()
  const [search, setSearch] = useState('')
  const [q, setQ] = useState('')
  const [chosenPlace, setPlace] = useState<Place>('all')
  const [tag, setTag] = useState('')
  const [sort, setSort] = useState<MediaSort>('created_desc')
  // The selection belongs to one search and tag filter: a file the filter hides is never acted on unseen (Delete
  // forever in Deleted, Move, Restore), and the bar counts what is on screen.
  const filterKey = `${q}\n${tag}`
  const [selection, setSelection] = useState({ key: filterKey, ids: NO_IDS })
  const selected = selection.key === filterKey ? selection.ids : NO_IDS
  const setSelected = useCallback(
    (next: Set<string> | ((prev: Set<string>) => Set<string>)) =>
      setSelection((s) => {
        const prev = s.key === filterKey ? s.ids : NO_IDS

        return { key: filterKey, ids: typeof next === 'function' ? next(prev) : next }
      }),
    [filterKey],
  )
  const [open, setOpen] = useState<MediaLibraryItem | null>(null)
  const [composeWith, setComposeWith] = useState<MediaLibraryItem[] | null>(null)
  const [tagging, setTagging] = useState(false)
  const [moving, setMoving] = useState<Moving | null>(null)
  const [folderDialog, setFolderDialog] = useState<FolderDialog | null>(null)
  const [folderError, setFolderError] = useState<string>()
  const [deletingFolder, setDeletingFolder] = useState<MediaFolder | null>(null)
  const [dragging, setDragging] = useState(false)
  const [since] = useState(() => Date.now() - WEEK)
  const [purgingIds, setPurgingIds] = useState<string[] | null>(null)
  const [keeping, setKeeping] = useState<MediaLibraryItem[] | null>(null)
  const [fresh, setFresh] = useState<Set<string>>(new Set())
  const input = useRef<HTMLInputElement>(null)
  // A new video from a prompt, when the server offers it.
  const video = useLazyDialog()
  const aiVideo = useAiStatus().data?.operations.some((o) => o.id === 'text_to_video') ?? false
  const onUploaded = useCallback((m: Media) => {
    setFresh((prev) => new Set(prev).add(m.id))
    setTimeout(() => setFresh((prev) => new Set([...prev].filter((id) => id !== m.id))), 5000)

    if (m.duplicate_of) {
      const twin = m.duplicate_of

      void api
        .media(twin)
        .then((t) =>
          toast.info(`Already in your library as ${t.original_name}`, {
            description: `${m.original_name} was uploaded anyway.`,
            action: { label: 'Open', onClick: () => setOpen({ ...t, uses: 0 }) },
          }),
        )
        .catch(() => undefined)
    }
  }, [])
  const {
    items: uploads,
    add: addFiles,
    clearDone,
    cancel: cancelUpload,
    retry: retryUpload,
    dismiss: dismissUpload,
  } = useMediaUpload(onUploaded)
  const allUploaded = uploads.length > 0 && uploads.every((x) => x.media)

  // Everything went up: the card goes away and a toast says so (Show brings the new tiles into view).
  useEffect(() => {
    if (!allUploaded) {
      return
    }

    const done = uploads.flatMap((x) => (x.media ? [x.media] : []))

    clearDone()
    toast.success(done.length === 1 ? `Uploaded ${done[0].original_name}` : `Uploaded ${files(done.length)}`, {
      action: {
        label: 'Show',
        onClick: () => {
          setPlace('all')
          setFresh(new Set(done.map((m) => m.id)))
          document.querySelector('[data-testid=media-tile]')?.scrollIntoView({ behavior: 'smooth', block: 'center' })
        },
      },
    })
    // Runs when the last file went up: the card clears, so `allUploaded` turns false again right after.
  }, [allUploaded, uploads, clearDone])

  // Search as you type, a moment after the last key.
  useEffect(() => {
    const t = setTimeout(() => setQ(search.trim()), 250)

    return () => clearTimeout(t)
  }, [search])

  const tree = useQuery({ queryKey: mediaKeys.folders, queryFn: () => api.mediaFolders() })
  const treeData = tree.data
  // Memoised, so the compiler knows they are not changed later (helpers like folderPath read them).
  const folders = useMemo(() => treeData?.data ?? NO_FOLDERS, [treeData])
  const folder = useMemo(() => folders.find((f) => f.id === chosenPlace), [folders, chosenPlace])
  const fixed = ['all', ROOT, 'recent', 'unused', 'deleted', 'duplicates', ...KINDS].includes(chosenPlace)
  // A folder deleted (here or elsewhere) sends the view back to every file.
  const place: Place = fixed || folder || !tree.data ? chosenPlace : 'all'
  const recent = place === 'recent'
  const trash = place === 'deleted'
  const dupes = place === 'duplicates'
  const query: MediaQuery = {
    q: q || undefined,
    kind: KINDS.includes(place) ? (place as MediaKind) : undefined,
    used: place === 'unused' ? 'unused' : undefined,
    folder: place === ROOT ? ROOT : folder?.id,
    tag: tag || undefined,
    deleted: trash || undefined,
    duplicates: dupes || undefined,
    // Recents: the newest first, up to a week old.
    sort: recent ? 'created_desc' : sort,
    limit: PAGE,
  }
  const { data, isLoading, error, refetch, hasNextPage, fetchNextPage, isFetchingNextPage, isFetchNextPageError } =
    useInfiniteQuery({
      queryKey: mediaKeys.library(query),
      queryFn: ({ pageParam, signal }) => api.mediaLibrary({ ...query, offset: pageParam }, signal),
      initialPageParam: 0,
      getNextPageParam: (last) =>
        recent && last.data.some((m) => Date.parse(m.created_at) < since) ? undefined : last.next_offset,
      // Unchanged files keep their identity when one is added or removed, so their tiles don't re-render.
      structuralSharing: shareMediaPages,
    })
  const pages = data?.pages
  const carousels = useCarousels()
  const stacked = useMemo(() => {
    if (!pages) {
      return { items: NO_ITEMS, pages: NO_STACKS }
    }

    // A file added here while paging can come again on the next page: once is enough.
    const seen = new Set<string>()
    const loaded = pages.flatMap((p) => p.data).filter((m) => !seen.has(m.id) && !!seen.add(m.id))

    return stackCarousels(recent ? loaded.filter((m) => Date.parse(m.created_at) >= since) : loaded, carousels)
  }, [pages, recent, since, carousels])
  const items = stacked.items
  const searching = !!q || !!tag
  const picked = useMemo(() => items.filter((m) => selected.has(m.id)), [items, selected])
  // A carousel's tile stands for all its pages: moving, tagging or deleting it takes every page.
  const withPages = (ids: Iterable<string>) => [...ids].flatMap((id) => stacked.pages.get(id) ?? [id])

  // Paste files (a screenshot, a copied picture) anywhere on the page.
  useEffect(() => {
    if (!canWrite) {
      return
    }

    const onPaste = (e: ClipboardEvent) => {
      const target = e.target as HTMLElement | null

      if (target?.closest('input, textarea, [contenteditable=true]')) {
        return
      }

      const pasted = Array.from(e.clipboardData?.files ?? [])

      if (pasted.length) {
        e.preventDefault()
        addFiles(pasted)
      }
    }

    window.addEventListener('paste', onPaste)

    return () => window.removeEventListener('paste', onPaste)
  }, [canWrite, addFiles])

  // Esc clears the selection (unless a dialog or menu took the key first).
  useEffect(() => {
    if (!selected.size) {
      return
    }

    const onKey = (e: KeyboardEvent) => {
      if (
        e.key === 'Escape' &&
        !e.defaultPrevented &&
        !document.querySelector('[role=dialog], [role=alertdialog], [role=menu]')
      ) {
        setSelected(new Set())
      }
    }

    window.addEventListener('keydown', onKey)

    return () => window.removeEventListener('keydown', onKey)
  }, [selected.size, setSelected])

  const { mutate: deleteSelected, isPending: deleting } = useMutation({
    mutationFn: () => api.deleteMediaMany(withPages(selected)),
    onSuccess: (res) => {
      setSelected(new Set())
      // One file is named in the title; more are counted under it.
      deletedToast(qc, res.deleted, res.deleted.length > 1 ? files(res.deleted.length) : undefined)
    },
    onError: (e) => toastApiError(e),
  })
  const { mutate: restoreFiles, isPending: restoring } = useMutation({
    mutationFn: (ids: string[]) => Promise.all(ids.map((id) => api.restoreMedia(id))),
    onSuccess: (res) => {
      setSelected(new Set())
      restoreInCache(qc, res)
      toast.success(`Restored ${files(res.length)}`)
    },
    onError: (e) => toastApiError(e),
  })
  const { mutate: purgeFiles, isPending: purging } = useMutation({
    // Empty Deleted: every page of it, 200 at a time.
    mutationFn: async (ids: string[] | 'all') => {
      const all = ids === 'all' ? [] : [...ids]

      // Pages and batches one after another: each depends on the previous answer, and the server stays calm.
      if (ids === 'all') {
        for (let offset: number | undefined = 0; offset !== undefined;) {
          const page = await api.mediaLibrary({ deleted: true, limit: 200, offset })

          all.push(...page.data.map((m) => m.id))
          offset = page.next_offset ?? undefined
        }
      }

      const deleted: string[] = []

      for (let i = 0; i < all.length; i += 200) {
        deleted.push(...(await api.deleteMediaMany(all.slice(i, i + 200), { permanent: true })).deleted)
      }

      return deleted
    },
    onSuccess: (deleted) => {
      setSelected(new Set())
      removeMedia(qc, deleted)
      // Their twins may not be duplicates any more.
      invalidateLists(qc, (list) => !!list.duplicates)
      refreshMediaCounts(qc)
      toast.success(`Deleted ${files(deleted.length)} for good`, { id: PURGE_TOAST })
    },
    onError: (e) => {
      toast.dismiss(PURGE_TOAST)
      toastApiError(e)
    },
    // Deleting for good takes a moment per file: say so while it runs.
    onMutate: (ids) =>
      void toast.loading(ids === 'all' ? 'Emptying Deleted...' : `Deleting ${files(ids.length)} for good...`, {
        id: PURGE_TOAST,
      }),
  })
  const { mutate: keepOne } = useMutation({
    mutationFn: (m: MediaLibraryItem) => api.keepMedia(m.id),
    onSuccess: (res, m) => {
      deletedToast(qc, res.deleted, `Kept “${m.original_name}”, ${files(res.deleted.length)} moved`)
    },
    onError: (e) => toastApiError(e),
  })
  const { mutate: organizeFiles, isPending: organizing } = useMutation({
    mutationFn: (body: MediaBulk) => api.organizeMedia(body),
    onSuccess: (res, body) => {
      const add = body.add_tags ?? []
      const remove = new Set(body.remove_tags ?? [])
      const target = body.folder_id

      patchMedia(qc, res.updated, (m) => ({
        ...m,
        folder_id: target ?? m.folder_id,
        tags: add.length || remove.size ? [...new Set([...m.tags.filter((t) => !remove.has(t)), ...add])] : m.tags,
      }))
      // Lists the files now belong to (the target folder, a tag added) gain them.
      invalidateLists(
        qc,
        (list) =>
          (target !== undefined && list.folder === (target || 'root')) || (!!list.tag && add.includes(list.tag)),
      )
      refreshMediaCounts(qc)

      if (body.folder_id !== undefined) {
        const to = body.folder_id ? folderPath(folders, body.folder_id) : 'No folder'

        toast.success(`Moved ${files(res.updated.length)} to ${to}`)
        setSelected(new Set())
      } else {
        toast.success(`Updated the tags of ${files(res.updated.length)}`)
      }
    },
    onError: (e) => toastApiError(e),
  })
  const { mutate: saveFolder, isPending: savingFolder } = useMutation({
    mutationFn: ({ d, name }: { d: FolderDialog; name: string }) =>
      d.mode === 'new'
        ? api.createMediaFolder({ name, parent_id: d.parent })
        : api.updateMediaFolder(d.folder.id, { name }),
    onSuccess: (f, { d }) => {
      void qc.invalidateQueries({ queryKey: mediaKeys.folders, exact: true })
      setFolderDialog(null)

      if (d.mode === 'new') {
        setPlace(f.id)
        toast.success(`Created “${f.name}”`)
      }
    },
    onError: (e) => {
      if (e instanceof ApiError && (e.status === 409 || e.status === 422)) {
        setFolderError(e.fields?.name ?? e.message)
      } else {
        toastApiError(e)
      }
    },
  })
  const { mutate: moveFolder } = useMutation({
    mutationFn: ({ id, parent }: { id: string; parent: string }) => api.updateMediaFolder(id, { parent_id: parent }),
    onSuccess: (f) => {
      void qc.invalidateQueries({ queryKey: mediaKeys.folders, exact: true })
      toast.success(`Moved “${f.name}”`)
    },
    onError: (e) => toastApiError(e),
  })
  const { mutate: removeFolder } = useMutation({
    mutationFn: (f: MediaFolder) => api.deleteMediaFolder(f.id),
    onSuccess: (_, f) => {
      if (place === f.id) {
        setPlace(f.parent_id || 'all')
      }

      folderGoneInCache(qc, f)
      toast.success(`Deleted the folder “${f.name}”`)
    },
    onError: (e) => toastApiError(e),
  })

  const openFolderDialog = (d: FolderDialog) => {
    setFolderError(undefined)
    setFolderDialog(d)
  }

  const newFolder = () => openFolderDialog({ mode: 'new', parent: folder?.id ?? '' })

  const anchor = useRef<string | null>(null)
  // Shift extends the selection from the last tile picked to this one.
  const toggle = (m: MediaLibraryItem, range = false) => {
    const from = items.findIndex((x) => x.id === anchor.current)
    const to = items.findIndex((x) => x.id === m.id)

    setSelected((prev) => {
      const next = new Set(prev)

      if (range && from >= 0 && to >= 0) {
        for (const x of items.slice(Math.min(from, to), Math.max(from, to) + 1)) {
          next.add(x.id)
        }
      } else if (next.has(m.id)) {
        next.delete(m.id)
      } else {
        next.add(m.id)
      }

      return next
    })
    anchor.current = m.id
  }

  const postable = useMemo(() => picked.filter((m) => /^(image|video)\//.test(m.mime_type)), [picked])
  const childCount = deletingFolder ? folders.filter((f) => f.parent_id === deletingFolder.id).length : 0
  const deleteTo = deletingFolder?.parent_id ? `“${folderPath(folders, deletingFolder.parent_id)}”` : 'No folder'

  const nav = {
    tree: tree.data,
    place,
    tag,
    canWrite,
    onPlace: (p: Place) => {
      setPlace(p)
      setSelected(new Set())
    },
    onTag: setTag,
    onNewFolder: newFolder,
    onDropMedia: (id: string, ids: string[]) => organizeFiles({ ids, folder_id: id }),
    onDropFolder: (id: string, into: string) => {
      if (!folderAndBelow(folders, id).has(into)) {
        moveFolder({ id, parent: into })
      }
    },
  }

  // Search and sort sit behind one Filter button next to Upload; what is in use shows as chips under the title.
  const filters = (
    <FilterButton
      active={(search.trim() ? 1 : 0) + (sort !== 'created_desc' ? 1 : 0)}
      onClear={() => {
        setSearch('')
        setSort('created_desc')
      }}
    >
      <div className="relative">
        <Search className="pointer-events-none absolute top-1/2 left-2.5 size-3.5 -translate-y-1/2 text-muted-foreground" />
        <Input
          value={search}
          onChange={(e) => setSearch(e.target.value)}
          placeholder="Search files"
          aria-label="Search files"
          className="pl-8"
        />
      </div>
      {!recent && !trash && !dupes && (
        <Select value={sort} onValueChange={(v) => setSort(v as MediaSort)}>
          <SelectTrigger className="w-full" aria-label="Sort">
            <SelectValue />
          </SelectTrigger>
          <SelectContent>
            <SelectItem value="created_desc">Newest first</SelectItem>
            <SelectItem value="created_asc">Oldest first</SelectItem>
            <SelectItem value="size_desc">Largest first</SelectItem>
            <SelectItem value="name_asc">Name</SelectItem>
          </SelectContent>
        </Select>
      )}
    </FilterButton>
  )

  const header = (
    <div className="flex flex-wrap items-center gap-3">
      {/* The title is where you are: All files, a library view or the folder's own name (its parents below). */}
      <div className="min-w-0 flex-1 basis-full sm:basis-auto">
        <h1
          className="truncate text-xl font-semibold tracking-tight"
          data-testid="media-place"
          title={folder ? folder.name : placeLabel(folders, place)}
        >
          {folder ? folder.name : placeLabel(folders, place)}
        </h1>
        {!folder && VIEWS[place] ? (
          <p className="text-sm wrap-anywhere text-muted-foreground sm:truncate" title={VIEWS[place].about}>
            {VIEWS[place].about}
          </p>
        ) : (
          folder && (
            // Where the folder sits: All files / parent / ... / this one; every step but the current one opens it.
            <nav aria-label="Folder path" className="min-w-0 truncate text-sm text-muted-foreground">
              {[{ id: 'all', name: 'All files' }, ...folderChain(folders, folder.id)].map((f, i, all) => (
                <Fragment key={f.id}>
                  {i > 0 && (
                    <span aria-hidden className="mx-1.5 text-muted-foreground/60">
                      /
                    </span>
                  )}
                  {i === all.length - 1 ? (
                    <span aria-current="page" className="text-foreground">
                      {f.name}
                    </span>
                  ) : (
                    <button
                      type="button"
                      className="rounded-sm underline-offset-4 outline-none hover:text-foreground hover:underline focus-visible:ring-2 focus-visible:ring-ring"
                      onClick={() => nav.onPlace(f.id)}
                    >
                      {f.name}
                    </button>
                  )}
                </Fragment>
              ))}
            </nav>
          )
        )}
      </div>
      {folder && canWrite && (
        <DropdownMenu>
          <DropdownMenuTrigger asChild>
            <Button variant="ghost" size="icon" aria-label="Folder actions" className="shrink-0">
              <MoreHorizontal />
            </Button>
          </DropdownMenuTrigger>
          <DropdownMenuContent align="end">
            <DropdownMenuItem onSelect={() => openFolderDialog({ mode: 'new', parent: folder.id })}>
              <FolderPlus /> New folder inside
            </DropdownMenuItem>
            <DropdownMenuItem onSelect={() => openFolderDialog({ mode: 'rename', folder })}>
              <PenSquare /> Rename
            </DropdownMenuItem>
            <DropdownMenuItem onSelect={() => setMoving({ folder })}>
              <FolderInput /> Move to...
            </DropdownMenuItem>
            <DropdownMenuSeparator />
            <DropdownMenuItem variant="destructive" onSelect={() => setDeletingFolder(folder)}>
              <Trash2 /> Delete folder
            </DropdownMenuItem>
          </DropdownMenuContent>
        </DropdownMenu>
      )}
      {filters}
      {canWrite && (
        <>
          <input
            ref={input}
            type="file"
            multiple
            accept={MEDIA_ACCEPT}
            className="sr-only"
            tabIndex={-1}
            aria-hidden
            data-testid="media-upload-input"
            onChange={(e) => {
              addFiles(Array.from(e.target.files ?? []))
              e.target.value = ''
            }}
          />
          {/* Making something new: violet, so it reads apart from filtering and uploading. Each editor is a full
              page load (its own CSP). */}
          <DropdownMenu>
            <DropdownMenuTrigger asChild>
              <Button
                className="shrink-0 border-0 bg-gradient-to-r from-violet-600 to-fuchsia-600 text-white shadow-sm hover:from-violet-500 hover:to-fuchsia-500"
                data-testid="media-create"
              >
                <Sparkle /> <span className="max-sm:sr-only">Create</span> <ChevronDown className="opacity-80" />
              </Button>
            </DropdownMenuTrigger>
            <DropdownMenuContent align="end" className="w-64">
              {CREATE.map((c) => (
                <DropdownMenuItem key={c.href} asChild>
                  {/* oxlint-disable-next-line next/no-html-link-for-pages -- a full load: the editors' own CSP */}
                  <a href={c.href} data-testid={c.testId} className="flex items-start gap-3 py-2">
                    <span className="mt-0.5 flex size-8 shrink-0 items-center justify-center rounded-md bg-brand-soft text-brand [&_svg]:size-4">
                      {c.icon}
                    </span>
                    <span className="flex min-w-0 flex-col whitespace-normal">
                      <span className="font-medium">{c.label}</span>
                      <span className="text-xs text-muted-foreground">{c.hint}</span>
                    </span>
                  </a>
                </DropdownMenuItem>
              ))}
              {/* Made by AI, apart from the editors (as the design editor's AI section). */}
              {aiVideo && (
                <>
                  <DropdownMenuSeparator />
                  <DropdownMenuLabel className="text-xs font-medium text-muted-foreground uppercase">
                    AI
                  </DropdownMenuLabel>
                  <DropdownMenuItem
                    onSelect={() => video.setOpen(true)}
                    data-testid="media-ai-video"
                    className="flex items-start gap-3 py-2"
                  >
                    <span className="mt-0.5 flex size-8 shrink-0 items-center justify-center rounded-md bg-violet-500/15 text-violet-600 dark:text-violet-400 [&_svg]:size-4">
                      <Clapperboard />
                    </span>
                    <span className="flex min-w-0 flex-col whitespace-normal">
                      <span className="font-medium">AI video</span>
                      <span className="text-xs text-muted-foreground">A clip from words or a picture</span>
                    </span>
                  </DropdownMenuItem>
                </>
              )}
            </DropdownMenuContent>
          </DropdownMenu>
          <Button onClick={() => input.current?.click()} data-testid="media-upload" className="shrink-0">
            <Upload /> Upload
          </Button>
        </>
      )}
    </div>
  )

  const editable = useMemo(() => picked.filter((m) => m.mime_type.startsWith('image/')), [picked])
  // Pictures and videos make a video (with the sounds picked as its music); sounds alone open the sound editor.
  const visualPicked = useMemo(() => picked.filter((m) => /^(image|video)\//.test(m.mime_type)), [picked])
  const soundPicked = useMemo(() => picked.filter((m) => m.mime_type.startsWith('audio/')), [picked])
  const actionBar = selected.size > 0 && (
    <div
      role="toolbar"
      aria-label="Selected files"
      data-testid="media-selection"
      className="sticky bottom-[calc(4.5rem+env(safe-area-inset-bottom))] z-20 mx-auto mt-auto flex w-fit max-w-[calc(100%-2rem)] animate-in items-center gap-1 overflow-x-auto rounded-xl border bg-popover p-1.5 text-popover-foreground shadow-lg duration-200 fade-in slide-in-from-bottom-4 md:bottom-6"
    >
      <Button variant="ghost" size="icon" aria-label="Clear selection" onClick={() => setSelected(new Set())}>
        <X />
      </Button>
      <span className="shrink-0 px-1 text-sm font-medium whitespace-nowrap tabular-nums">{selected.size} selected</span>
      <span aria-hidden className="mx-1 h-5 w-px shrink-0 bg-border" />
      {canWrite && trash && (
        <>
          <Button variant="ghost" aria-label="Restore" onClick={() => restoreFiles([...selected])} disabled={restoring}>
            <Undo2 /> {barLabel('Restore')}
          </Button>
          <span aria-hidden className="mx-1 h-5 w-px shrink-0 bg-border" />
          <Button
            variant="destructive"
            aria-label="Delete forever"
            onClick={() => setPurgingIds([...selected])}
            disabled={purging}
          >
            {purging ? <Loader2 className="animate-spin" /> : <Trash2 />} {barLabel('Delete forever')}
          </Button>
        </>
      )}
      {!trash && (
        <>
          {canWrite && (
            <>
              <Button
                variant="ghost"
                aria-label="Move to folder"
                onClick={() => setMoving({ ids: withPages(selected) })}
              >
                <FolderInput /> {barLabel('Move to folder')}
              </Button>
              <Button variant="ghost" aria-label="Tags" onClick={() => setTagging(true)}>
                <Hash /> {barLabel('Tags')}
              </Button>
              <Button
                variant="ghost"
                aria-label="New post"
                onClick={() => setComposeWith(postable)}
                disabled={postable.length === 0}
              >
                <PenSquare /> {barLabel('New post')}
              </Button>
            </>
          )}
          <Button variant="ghost" aria-label="Download" onClick={() => download(withPages(picked.map((m) => m.id)))}>
            <Download /> {barLabel('Download')}
          </Button>
          {canWrite && (visualPicked.length > 0 || soundPicked.length > 0) && (
            <Button variant="ghost" aria-label={visualPicked.length ? 'Make a video' : 'Edit the sound'} asChild>
              <a
                href={
                  visualPicked.length
                    ? `/media/video?ids=${withPages([...visualPicked, ...soundPicked].map((m) => m.id)).join(',')}`
                    : `/media/audio?ids=${soundPicked.map((m) => m.id).join(',')}`
                }
                data-testid="media-make-video"
              >
                {visualPicked.length ? <Film /> : <AudioLines />} {barLabel(visualPicked.length ? 'Video' : 'Sound')}
              </a>
            </Button>
          )}
          {canWrite && (
            <Button variant="ghost" aria-label="Edit" disabled={editable.length === 0} asChild={editable.length > 0}>
              {editable.length > 0 ? (
                <a href={`/media/edit?ids=${editable.map((m) => m.id).join(',')}`}>
                  <Palette /> {barLabel('Edit')}
                </a>
              ) : (
                <>
                  <Palette /> {barLabel('Edit')}
                </>
              )}
            </Button>
          )}
          {canWrite && (
            <>
              <span aria-hidden className="mx-1 h-5 w-px shrink-0 bg-border" />
              <Button variant="destructive" aria-label="Delete" onClick={() => deleteSelected()} disabled={deleting}>
                <Trash2 /> {barLabel('Delete')}
              </Button>
            </>
          )}
        </>
      )}
    </div>
  )

  const toolbar = (
    // Phones: the library button; everywhere: the filters in use as chips (the row is hidden on desktop when empty).
    <div
      className={cn('mt-3 flex flex-wrap items-center gap-2', !tag && !search.trim() && 'md:hidden')}
      data-testid="media-filters"
    >
      <LibrarySheet {...nav} />
      {search.trim() && (
        <Button variant="outline" onClick={() => setSearch('')} aria-label={`Remove the search ${search.trim()}`}>
          “{search.trim()}” <X />
        </Button>
      )}
      {tag && (
        <Button variant="outline" onClick={() => setTag('')} aria-label={`Remove the tag filter ${tag}`}>
          #{tag} <X />
        </Button>
      )}
      {searching && (
        <Button
          variant="ghost"
          onClick={() => {
            setSearch('')
            setTag('')
          }}
        >
          <X /> Clear
        </Button>
      )}
    </div>
  )

  const empty = searching
    ? { title: 'No files match', description: 'Try another name or tag.' }
    : folder
      ? {
          title: 'This folder is empty',
          description: 'Drag files onto the folder on the left, or select files and choose Move to folder.',
        }
      : (VIEWS[place] ?? { title: 'Nothing here', description: 'No files here yet.' })

  const EmptyIcon = (!searching && !folder && LIBRARY.find((l) => l.key === place)?.icon) || Images

  const content = isLoading ? (
    // A view opened the first time loads in a blink: the placeholders only fade in if it takes longer, so they never flash.
    <div className="grid animate-in gap-4 fade-in [animation-delay:300ms] [animation-fill-mode:both]">
      <SkeletonList count={6} className="h-40 rounded-xl" />
    </div>
  ) : error && !data ? (
    <ErrorState error={error} onRetry={() => refetch()} />
  ) : items.length === 0 ? (
    <EmptyState
      icon={<EmptyIcon />}
      title={empty.title}
      description={empty.description}
      action={
        !searching &&
        !folder &&
        VIEWS[place]?.upload &&
        canWrite && (
          <Button onClick={() => input.current?.click()}>
            <Upload /> Upload files
          </Button>
        )
      }
    />
  ) : dupes ? (
    <DuplicateGroups
      items={items}
      folders={folders}
      selected={selected}
      onOpen={setOpen}
      onToggle={canWrite ? toggle : undefined}
      onKeep={canWrite ? setKeeping : undefined}
    />
  ) : (
    <>
      <MediaGrid
        items={items}
        highlight={fresh}
        stacks={trash ? undefined : stacked.pages}
        onSelectMany={canWrite ? (ids) => setSelected(new Set(ids)) : undefined}
        caption={trash ? (m) => `Deletes ${timeAgo(m.purge_at)}` : undefined}
        selected={selected}
        onOpen={setOpen}
        onToggle={canWrite ? toggle : undefined}
        onSelectAll={canWrite ? () => setSelected(new Set(items.map((m) => m.id))) : undefined}
        dragIds={canWrite && !trash ? (m) => withPages(selected.has(m.id) ? selected : [m.id]) : undefined}
        className="gap-4 lg:grid-cols-[repeat(auto-fill,minmax(11.5rem,1fr))]"
      />
      {hasNextPage && (
        <LoadMore
          className="mt-6"
          label="Show more"
          onLoad={() => void fetchNextPage()}
          loading={isFetchingNextPage}
          failed={isFetchNextPageError}
        />
      )}
    </>
  )

  return (
    <div
      className="relative flex min-h-0 flex-1 flex-col md:flex-row"
      data-testid="media-page"
      onDragOver={(e) => {
        if (canWrite && isExternalFileDrag(e.dataTransfer.types)) {
          e.preventDefault()
          setDragging(true)
        }
      }}
      onDragLeave={(e) => {
        if (e.currentTarget === e.target || !e.currentTarget.contains(e.relatedTarget as Node)) {
          setDragging(false)
        }
      }}
      onDrop={(e) => {
        setDragging(false)

        if (!canWrite || !isExternalFileDrag(e.dataTransfer.types)) {
          return
        }

        e.preventDefault()
        addFiles(Array.from(e.dataTransfer.files))
      }}
    >
      <LibraryPane {...nav} />

      <div className="flex min-w-0 flex-1 flex-col">
        <div className="sticky top-0 z-10 border-b bg-background/95 px-4 py-3 backdrop-blur sm:px-6">
          {header}
          {toolbar}
        </div>

        {/* A flex column filling the page: the selection bar sits at its bottom (mt-auto) even under a short grid, and stays
            in view (sticky) under a long one. */}
        <div className="flex flex-1 flex-col px-4 py-4 sm:px-6">
          {trash && items.length > 0 && (
            <Callout
              tone="warning"
              className="mb-4 items-center"
              action={
                canWrite && (
                  <Button variant="destructive" size="sm" onClick={() => setPurgingIds([])} disabled={purging}>
                    {purging ? <Loader2 className="animate-spin" /> : <Trash2 />}{' '}
                    {purging ? 'Emptying...' : 'Empty Deleted'}
                  </Button>
                )
              }
            >
              Files here are deleted for good after 30 days and still count toward your storage until then.
            </Callout>
          )}
          {content}
          {actionBar}
        </div>
      </div>

      {dragging && (
        <div
          aria-hidden
          data-testid="media-drop-overlay"
          className="pointer-events-none fixed inset-0 z-40 flex items-center justify-center bg-background/70 backdrop-blur-sm"
        >
          <div className="rounded-2xl border-2 border-dashed border-brand bg-card px-8 py-6 text-center">
            <Upload className="mx-auto size-6 text-brand" />
            <p className="mt-2 text-sm font-medium">Drop to upload</p>
          </div>
        </div>
      )}

      <UploadCard items={uploads} onCancel={cancelUpload} onRetry={retryUpload} onDismiss={dismissUpload} />

      <MediaDetail
        item={open}
        pages={open ? stacked.pages.get(open.id) : undefined}
        onOpenChange={(o) => !o && setOpen(null)}
        onNewPost={(m) => {
          setOpen(null)
          setComposeWith([m])
        }}
        onOpenMedia={(id) =>
          void api
            .media(id)
            .then((m) => setOpen({ ...m, uses: 0 }))
            .catch((e) => toastApiError(e))
        }
      />
      {composeWith && <Composer open onOpenChange={(o) => !o && setComposeWith(null)} defaultMedia={composeWith} />}
      <ConfirmDialog
        open={!!purgingIds}
        onOpenChange={(o) => !o && setPurgingIds(null)}
        title={purgingIds?.length ? `Delete ${files(purgingIds.length)} for good?` : 'Empty Deleted?'}
        description={
          purgingIds?.length
            ? "Posts that still use them lose them. This can't be undone."
            : `Every file in Deleted (${files(tree.data?.deleted_files ?? 0)}) is removed for good. This can't be undone.`
        }
        confirmLabel={purgingIds?.length ? 'Delete forever' : 'Empty Deleted'}
        variant="destructive"
        onConfirm={() => purgingIds && purgeFiles(purgingIds.length ? purgingIds : 'all')}
      />
      <ConfirmDialog
        open={!!keeping}
        onOpenChange={(o) => !o && setKeeping(null)}
        title={`Keep “${keeping?.[0]?.original_name ?? ''}”?`}
        description={`${(keeping ?? [])
          .slice(1)
          .map((m) => `“${m.original_name}”`)
          .join(', ')} move to Deleted. Posts that use them use the kept file instead.`}
        confirmLabel="Keep one"
        onConfirm={() => keeping?.[0] && keepOne(keeping[0])}
      />
      <ConfirmDialog
        open={!!deletingFolder}
        onOpenChange={(o) => !o && setDeletingFolder(null)}
        title={`Delete the folder “${deletingFolder?.name ?? ''}”?`}
        description={`Its ${files(deletingFolder?.files ?? 0)}${childCount ? ` and ${childCount} ${childCount === 1 ? 'folder' : 'folders'}` : ''} move to ${deleteTo}. No file is deleted.`}
        confirmLabel="Delete folder"
        variant="destructive"
        onConfirm={() => deletingFolder && removeFolder(deletingFolder)}
      />
      {moving && (
        <FolderPickerDialog
          open
          onOpenChange={(o) => !o && setMoving(null)}
          title={'folder' in moving ? `Move “${moving.folder.name}”` : `Move ${files(moving.ids.length)}`}
          folders={folders}
          current={'folder' in moving ? moving.folder.parent_id : moving.current}
          exclude={'folder' in moving ? folderAndBelow(folders, moving.folder.id) : undefined}
          onPick={(to) =>
            'folder' in moving
              ? moveFolder({ id: moving.folder.id, parent: to })
              : organizeFiles({ ids: moving.ids, folder_id: to })
          }
        />
      )}
      {folderDialog && (
        <FolderNameDialog
          open
          onOpenChange={(o) => !o && setFolderDialog(null)}
          title={folderDialog.mode === 'rename' ? 'Rename folder' : 'New folder'}
          description={
            folderDialog.mode === 'new'
              ? folderDialog.parent
                ? `Inside “${folderPath(folders, folderDialog.parent)}”.`
                : 'Folders organize the library; a file stays in every post that uses it.'
              : undefined
          }
          initial={folderDialog.mode === 'rename' ? folderDialog.folder.name : ''}
          confirmLabel={folderDialog.mode === 'rename' ? 'Rename' : 'Create folder'}
          pending={savingFolder}
          error={folderError}
          onSubmit={(name) => saveFolder({ d: folderDialog, name })}
        />
      )}
      {video.used && <AiVideoDialog open={video.open} onOpenChange={video.setOpen} />}
      {tagging && (
        <TagsDialog
          open
          onOpenChange={setTagging}
          items={picked}
          known={(tree.data?.tags ?? []).map((t) => t.tag)}
          pending={organizing}
          onSubmit={(add, remove) =>
            organizeFiles(
              { ids: withPages(selected), add_tags: add, remove_tags: remove },
              { onSuccess: () => setTagging(false) },
            )
          }
        />
      )}
    </div>
  )
}

/**
 * Files on their way up, in one small card at the bottom right: progress and Cancel per file, the reason and
 * Retry or Dismiss for a file that failed. Files that went up leave the card (a toast says so).
 */
function UploadCard({
  items,
  onCancel,
  onRetry,
  onDismiss,
}: {
  items: UploadItem[]
  onCancel: (key: string) => void
  onRetry: (key: string) => void
  onDismiss: (key: string) => void
}) {
  const [open, setOpen] = useState(true)
  const rows = items.filter((x) => !x.media)

  if (!rows.length) {
    return null
  }

  const pending = rows.filter((x) => !x.error).length
  const failed = rows.length - pending

  return (
    <section
      aria-label="Uploads"
      className="fixed right-4 bottom-[max(1rem,env(safe-area-inset-bottom))] z-30 w-[min(22rem,calc(100vw-2rem))] rounded-xl border bg-popover text-popover-foreground shadow-lg"
      data-testid="media-uploads"
    >
      <div className="flex items-center gap-2 px-3 py-2">
        <p className="min-w-0 flex-1 truncate text-sm font-medium" aria-live="polite">
          {pending ? `Uploading ${files(pending)}` : `${files(failed)} not uploaded`}
        </p>
        <Button
          variant="ghost"
          size="icon-xs"
          aria-label={open ? 'Collapse uploads' : 'Expand uploads'}
          aria-expanded={open}
          onClick={() => setOpen(!open)}
        >
          <ChevronDown className={cn('transition-transform', !open && 'rotate-180')} />
        </Button>
      </div>
      {open && (
        <ul className="grid max-h-64 gap-2 overflow-y-auto border-t px-3 py-2">
          {rows.map((x) => (
            <li key={x.key} className="grid gap-1 text-sm">
              <div className="flex items-center gap-2">
                <span className="min-w-0 flex-1 truncate" title={x.name}>
                  {x.name}
                </span>
                {x.error ? (
                  <>
                    {x.file && (
                      <Button
                        variant="ghost"
                        size="icon-xs"
                        aria-label={`Retry ${x.name}`}
                        onClick={() => onRetry(x.key)}
                      >
                        <RotateCw />
                      </Button>
                    )}
                    <Button
                      variant="ghost"
                      size="icon-xs"
                      aria-label={`Dismiss ${x.name}`}
                      onClick={() => onDismiss(x.key)}
                    >
                      <X />
                    </Button>
                  </>
                ) : (
                  <>
                    <span className="shrink-0 text-xs text-muted-foreground tabular-nums">
                      {Math.round(x.progress * 100)}%
                    </span>
                    <Button
                      variant="ghost"
                      size="icon-xs"
                      aria-label={`Cancel ${x.name}`}
                      onClick={() => onCancel(x.key)}
                    >
                      <X />
                    </Button>
                  </>
                )}
              </div>
              {x.error ? (
                <p className="flex items-start gap-1.5 text-xs text-destructive">
                  <CircleAlert className="mt-0.5 size-3.5 shrink-0" aria-hidden /> {x.error}
                </p>
              ) : (
                <div
                  className="h-1 overflow-hidden rounded-full bg-muted"
                  role="progressbar"
                  aria-label={`Uploading ${x.name}`}
                  aria-valuenow={Math.round(x.progress * 100)}
                  aria-valuemin={0}
                  aria-valuemax={100}
                >
                  <div
                    className="h-full rounded-full bg-brand transition-[width]"
                    style={{ width: `${x.progress * 100}%` }}
                  />
                </div>
              )}
            </li>
          ))}
        </ul>
      )}
    </section>
  )
}

/** Duplicates: identical files together, a header per set ("3 copies · 24 KB each") with Keep one. */
function DuplicateGroups({
  items,
  folders,
  selected,
  onOpen,
  onToggle,
  onKeep,
}: {
  items: MediaLibraryItem[]
  folders: MediaFolder[]
  selected: Set<string>
  onOpen: (m: MediaLibraryItem) => void
  onToggle?: (m: MediaLibraryItem) => void
  onKeep?: (group: MediaLibraryItem[]) => void
}) {
  const groups: MediaLibraryItem[][] = []

  for (const m of items) {
    const last = groups.at(-1)

    if (last && last[0].sha256 === m.sha256) {
      last.push(m)
    } else {
      groups.push([m])
    }
  }

  return (
    <div className="grid gap-6" data-testid="media-duplicates">
      {groups.map((g) => (
        <section key={g[0].sha256} aria-label={`${g.length} copies of ${g[0].original_name}`} className="grid gap-2">
          <div className="flex items-center gap-2">
            <h2 className="min-w-0 flex-1 truncate text-sm font-medium">
              {g.length} copies · {formatBytes(g[0].size_bytes)} each
            </h2>
            {onKeep && g.length > 1 && (
              <Button variant="outline" onClick={() => onKeep(g)}>
                Keep one
              </Button>
            )}
          </div>
          <MediaGrid
            items={g}
            selected={selected}
            onOpen={onOpen}
            onToggle={onToggle}
            caption={(m) =>
              `${m.folder_id ? folderPath(folders, m.folder_id) : 'No folder'} · ${m.uses ? `Used in ${m.uses} ${m.uses === 1 ? 'post' : 'posts'}` : 'Not used'}`
            }
          />
        </section>
      ))}
    </div>
  )
}
