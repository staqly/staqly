'use client'

import {
  ChevronDown,
  ChevronRight,
  Clock,
  Copy,
  AudioLines,
  Film,
  Folder,
  FolderOpen,
  Image as ImageIcon,
  Images,
  Plus,
  Sparkles,
  Trash2,
} from 'lucide-react'
import { useRef, useState } from 'react'

import { Button } from '@/components/ui/button'
import { Sheet, SheetContent, SheetDescription, SheetHeader, SheetTitle } from '@/components/ui/sheet'
import type { MediaFolder, MediaFolders } from '@/lib/api'
import { setDragChip } from '@/lib/drag-chip'
import { folderTree } from '@/lib/media'
import { type Tone, TONES } from '@/lib/tones'
import { cn } from '@/lib/utils'

/** What a drag carries: library files (their ids) or a folder (its id). */
export const MEDIA_DRAG = 'application/x-staqly-media'
export const FOLDER_DRAG = 'application/x-staqly-folder'

/** The library's fixed places; a folder is a place too (its id). */
export const LIBRARY = [
  { key: 'all', label: 'All files', icon: Images },
  { key: 'recent', label: 'Recents', icon: Clock },
  { key: 'image', label: 'Images', icon: ImageIcon },
  { key: 'video', label: 'Videos', icon: Film },
  { key: 'audio', label: 'Audio', icon: AudioLines },
  { key: 'unused', label: 'Unused', icon: Sparkles },
  { key: 'duplicates', label: 'Duplicates', icon: Copy },
  { key: 'deleted', label: 'Deleted', icon: Trash2 },
] as const

/** The top of the folder tree: the files in no folder, and where files and folders go to leave theirs. */
export const ROOT = 'root'

/** Where the grid looks: one of LIBRARY's keys, ROOT, or a folder (its id). */
export type Place = (typeof LIBRARY)[number]['key'] | (string & {})

/** The page's upload overlay is for files dragged in from outside, never for a tile or folder moved inside. */
export function isExternalFileDrag(types: readonly string[]) {
  return types.includes('Files') && !types.includes(MEDIA_DRAG) && !types.includes(FOLDER_DRAG)
}

const DOT_TONES: Tone[] = ['danger', 'attention', 'warning', 'success', 'info', 'accent', 'neutral']

/** Finder-like tag dots: a stable colour per tag. */
function tagDot(tag: string) {
  let h = 0

  for (const c of tag) {
    h = (h * 31 + c.charCodeAt(0)) >>> 0
  }

  return TONES[DOT_TONES[h % DOT_TONES.length]].dot
}

/** A folder and every folder inside it. */
export function folderAndBelow(folders: MediaFolder[], id: string) {
  const out = new Set([id])
  let grew = true

  while (grew) {
    grew = false

    for (const f of folders) {
      if (out.has(f.parent_id) && !out.has(f.id)) {
        out.add(f.id)
        grew = true
      }
    }
  }

  return out
}

/** "Clients / ACME": a folder's path, for titles and toasts. */
export function folderPath(folders: MediaFolder[], id: string) {
  const byId = new Map(folders.map((f) => [f.id, f]))
  const parts: string[] = []

  for (let f = byId.get(id); f && parts.length < 12; f = byId.get(f.parent_id)) {
    parts.unshift(f.name)
  }

  return parts.join(' / ')
}

/** The label of a place: a library entry's name or a folder's path. */
export function placeLabel(folders: MediaFolder[], place: Place) {
  return place === ROOT ? '/' : (LIBRARY.find((l) => l.key === place)?.label ?? folderPath(folders, place))
}

/** While dragging files: a compact chip (the first thumbnail and the name, or "3 files") under the pointer. */
export function setMediaDragImage(e: React.DragEvent, names: string[]) {
  setDragChip(e, names.length === 1 ? names[0]! : `${names.length} files`, 'tile')
}

const dragKind = (e: React.DragEvent) =>
  e.dataTransfer.types.includes(MEDIA_DRAG) ? 'media' : e.dataTransfer.types.includes(FOLDER_DRAG) ? 'folder' : null

export const ROW =
  'flex h-7 w-full cursor-pointer items-center gap-2 rounded-md px-2 text-left text-sm outline-none select-none focus-visible:ring-2 focus-visible:ring-ring focus-visible:ring-inset pointer-coarse:h-10'
const rowState = (active: boolean, drop: boolean) =>
  cn(
    active ? 'bg-accent font-medium text-accent-foreground' : 'hover:bg-accent/60',
    drop && 'bg-brand/15 ring-2 ring-brand ring-inset',
  )
const COUNT = 'ml-auto shrink-0 text-xs text-muted-foreground tabular-nums'

/** A Finder-like section: a small uppercase header that folds it (chevron on hover), an optional action. */
function Section({
  id,
  title,
  action,
  children,
}: {
  id: string
  title: string
  action?: React.ReactNode
  children: React.ReactNode
}) {
  const [open, setOpen] = useState(true)

  return (
    <section aria-labelledby={`${id}-heading`} className="grid gap-0.5">
      <div className="group/section flex h-7 items-center gap-1 pr-1 pl-2 pointer-coarse:h-10">
        <h3 id={`${id}-heading`} className="min-w-0 flex-1">
          <button
            type="button"
            aria-expanded={open}
            onClick={() => setOpen(!open)}
            className="flex w-full items-center gap-1 rounded-sm text-[11px] font-semibold pointer-coarse:min-h-10 tracking-wide text-muted-foreground uppercase outline-none focus-visible:ring-2 focus-visible:ring-ring focus-visible:ring-inset"
          >
            {title}
            <ChevronDown
              aria-hidden
              className={cn(
                'size-3 opacity-0 transition group-focus-within/section:opacity-100 group-hover/section:opacity-100 pointer-coarse:opacity-100',
                !open && '-rotate-90',
              )}
            />
          </button>
        </h3>
        {action}
      </div>
      {open && children}
    </section>
  )
}

type NavProps = {
  tree?: MediaFolders
  place: Place
  tag: string
  canWrite: boolean
  onPlace: (p: Place) => void
  onTag: (t: string) => void
  onNewFolder: () => void
  /** Files dropped on a folder ('' = no folder, dropped on All files). */
  onDropMedia: (folderId: string, ids: string[]) => void
  onDropFolder: (folderId: string, into: string) => void
}

/**
 * The library's sidebar, grouped like Finder's: LIBRARY (all files, recents, by type, unused, duplicates, deleted), FOLDERS (a tree:
 * arrow keys move, Right and Left open and close, Enter shows; files and folders dropped on one move into it,
 * files dropped on All files leave their folder) and TAGS (filter). The desktop pane and the phone sheet show it.
 */
function LibraryNav({ tree, place, tag, canWrite, onPlace, onTag, onNewFolder, onDropMedia, onDropFolder }: NavProps) {
  const folders = tree?.data ?? []
  const [collapsed, setCollapsed] = useState<Set<string>>(new Set())
  // The tree row Tab lands on: the one last moved to, else the open folder, else the first.
  const [moved, setMoved] = useState<string | null>(null)
  const [dropKey, setDropKey] = useState<string | null>(null)
  const [allTags, setAllTags] = useState(false)
  const list = useRef<HTMLUListElement>(null)
  // Root first, the folders inside it one level down.
  const rows: (MediaFolder & { depth: number; hasChildren: boolean })[] = [
    {
      id: ROOT,
      parent_id: '',
      name: '/',
      files: tree?.root_files ?? 0,
      created_at: '',
      depth: 0,
      // ponytail: the top level is always open, so it has no chevron and sits flush left.
      hasChildren: false,
    },
  ]
  let hideBelow = Infinity

  for (const f of folderTree(folders)) {
    if (f.depth + 1 > hideBelow) {
      continue
    }

    hideBelow = collapsed.has(f.id) ? f.depth + 1 : Infinity
    rows.push({ ...f, depth: f.depth + 1 })
  }

  const focusKey = [moved, place].find((k) => rows.some((r) => r.id === k)) ?? rows[0]?.id
  const tags = tree?.tags ?? []
  const shownTags = allTags ? tags : tags.slice(0, 8)

  const setOpen = (id: string, open: boolean) =>
    setCollapsed((prev) => {
      const next = new Set(prev)

      if (open) {
        next.delete(id)
      } else {
        next.add(id)
      }

      return next
    })

  const focus = (id: string) => {
    setMoved(id)
    list.current?.querySelector<HTMLElement>(`[data-key="${CSS.escape(id)}"]`)?.focus()
  }

  const onKeyDown = (e: React.KeyboardEvent, i: number) => {
    const row = rows[i]
    const open = row.hasChildren && !collapsed.has(row.id)
    let to: string | undefined

    switch (e.key) {
      case 'ArrowDown':
        to = rows[i + 1]?.id
        break
      case 'ArrowUp':
        to = rows[i - 1]?.id
        break
      case 'Home':
        to = rows[0].id
        break
      case 'End':
        to = rows[rows.length - 1].id
        break
      case 'ArrowRight':
        if (row.hasChildren && !open) {
          setOpen(row.id, true)
        } else if (open) {
          to = rows[i + 1]?.id
        }

        break
      case 'ArrowLeft':
        if (open) {
          setOpen(row.id, false)
        } else if (row.id !== ROOT) {
          to = row.parent_id || ROOT
        }

        break
      case 'Enter':
      case ' ':
        onPlace(row.id)
        break
      default:
        return
    }

    e.preventDefault()

    if (to) {
      focus(to)
    }
  }

  /** Drop handlers for a row; `target` is the folder id ('' = no folder). Folders drop on folders and on Root. */
  const dropTarget = (key: string, target: string) =>
    canWrite
      ? {
          onDragOver: (e: React.DragEvent) => {
            const kind = dragKind(e)

            if (kind === 'media' || (kind === 'folder' && (target || key === ROOT))) {
              e.preventDefault()
              e.dataTransfer.dropEffect = 'move'
              setDropKey(key)
            }
          },
          onDragLeave: () => setDropKey((k) => (k === key ? null : k)),
          onDrop: (e: React.DragEvent) => {
            setDropKey(null)

            const media = e.dataTransfer.getData(MEDIA_DRAG)
            const folder = e.dataTransfer.getData(FOLDER_DRAG)

            if (media) {
              e.preventDefault()
              onDropMedia(target, JSON.parse(media) as string[])
            } else if (folder && (target || key === ROOT) && folder !== target) {
              e.preventDefault()
              onDropFolder(folder, target)
            }
          },
        }
      : {}

  return (
    <div className="grid gap-4" data-testid="media-library-nav">
      <Section id="media-library" title="Library">
        <ul className="grid gap-0.5">
          {LIBRARY.filter((l) => l.key !== 'duplicates' || !!tree?.duplicate_files || place === l.key).map((l) => (
            <li key={l.key}>
              <button
                type="button"
                aria-current={place === l.key ? 'page' : undefined}
                onClick={() => onPlace(l.key)}
                className={cn(
                  ROW,
                  rowState(place === l.key, dropKey === l.key),
                  l.key === 'deleted' && TONES.danger.text,
                )}
                data-testid={`media-place-${l.key}`}
                // Files dropped on All files leave their folder.
                {...(l.key === 'all' ? dropTarget('all', '') : {})}
              >
                <l.icon
                  className={cn('size-4 shrink-0', l.key === 'deleted' ? 'text-current' : 'text-muted-foreground')}
                  aria-hidden
                />
                <span className="min-w-0 flex-1 truncate">{l.label}</span>
                {l.key === 'all' && tree && <span className={COUNT}>{tree.total_files}</span>}
                {l.key === 'deleted' && !!tree?.deleted_files && <span className={COUNT}>{tree.deleted_files}</span>}
                {l.key === 'duplicates' && !!tree?.duplicate_files && (
                  <span className={COUNT}>{tree.duplicate_files}</span>
                )}
              </button>
            </li>
          ))}
        </ul>
      </Section>

      <Section
        id="media-folders"
        title="Folders"
        action={
          canWrite && (
            <Button
              variant="ghost"
              size="icon-xs"
              aria-label="New folder"
              className="text-muted-foreground"
              onClick={onNewFolder}
              data-testid="media-new-folder"
            >
              <Plus />
            </Button>
          )
        }
      >
        <ul ref={list} role="tree" aria-label="Folders" className="grid gap-0.5" data-testid="media-folder-tree">
          {rows.map((r, i) => {
            const open = r.hasChildren && !collapsed.has(r.id)

            return (
              <li
                key={r.id}
                role="treeitem"
                data-key={r.id}
                aria-level={r.depth + 1}
                aria-selected={r.id === place}
                aria-expanded={r.hasChildren ? open : undefined}
                tabIndex={r.id === focusKey ? 0 : -1}
                draggable={canWrite && r.id !== ROOT}
                onDragStart={(e) => {
                  e.dataTransfer.setData(FOLDER_DRAG, r.id)
                  e.dataTransfer.effectAllowed = 'move'
                }}
                {...dropTarget(r.id, r.id === ROOT ? '' : r.id)}
                onClick={() => {
                  setMoved(r.id)
                  onPlace(r.id)
                }}
                onKeyDown={(e) => onKeyDown(e, i)}
                style={r.id === ROOT ? undefined : { paddingLeft: `${0.25 + (r.depth - 1) * 0.875}rem` }}
                className={cn(ROW, 'gap-1.5', rowState(r.id === place, dropKey === r.id))}
              >
                {r.id !== ROOT && (
                  <span
                    aria-hidden
                    className="flex size-4 shrink-0 items-center justify-center text-muted-foreground"
                    onClick={(e) => {
                      if (r.hasChildren) {
                        e.stopPropagation()
                        setOpen(r.id, !open)
                      }
                    }}
                  >
                    {r.hasChildren && (
                      <ChevronRight className={cn('size-3.5 transition-transform', open && 'rotate-90')} />
                    )}
                  </span>
                )}
                {r.id === place ? (
                  <FolderOpen className="size-4 shrink-0 text-brand" aria-hidden />
                ) : (
                  <Folder className="size-4 shrink-0 text-muted-foreground" aria-hidden />
                )}
                <span className="min-w-0 flex-1 truncate">{r.name}</span>
                <span className={COUNT}>{r.files}</span>
              </li>
            )
          })}
        </ul>
      </Section>

      {tags.length > 0 && (
        <Section id="media-tags" title="Tags">
          <ul className="grid gap-0.5" data-testid="media-tags">
            {shownTags.map((t) => (
              <li key={t.tag}>
                <button
                  type="button"
                  aria-pressed={tag === t.tag}
                  onClick={() => onTag(tag === t.tag ? '' : t.tag)}
                  className={cn(ROW, rowState(tag === t.tag, false))}
                >
                  <span aria-hidden className={cn('mx-1 size-2 shrink-0 rounded-full', tagDot(t.tag))} />
                  <span className="min-w-0 flex-1 truncate">{t.tag}</span>
                  <span className={COUNT}>{t.files}</span>
                </button>
              </li>
            ))}
            {tags.length > 8 && (
              <li>
                <button
                  type="button"
                  onClick={() => setAllTags(!allTags)}
                  className={cn(ROW, 'text-xs text-muted-foreground hover:bg-accent/60')}
                >
                  {allTags ? 'Show fewer tags' : `Show all tags (${tags.length})`}
                </button>
              </li>
            )}
          </ul>
        </Section>
      )}
    </div>
  )
}

/** Tablets and desktops: the sidebar beside the files. */
export function LibraryPane(props: NavProps) {
  return (
    <nav
      aria-label="Library"
      data-testid="media-pane"
      className="hidden min-h-0 border-r bg-sidebar/40 md:sticky md:top-0 md:block md:h-svh md:w-60 md:shrink-0 md:overflow-y-auto md:overscroll-contain md:px-2 md:py-3"
    >
      <LibraryNav {...props} />
    </nav>
  )
}

/** Phones: a button with the open place that opens the sidebar in a sheet; picking something closes it. */
export function LibrarySheet(props: NavProps) {
  const [open, setOpen] = useState(false)
  const folders = props.tree?.data ?? []

  return (
    <>
      <Button variant="outline" className="max-w-56 md:hidden" onClick={() => setOpen(true)}>
        <FolderOpen />
        <span className="truncate">
          {placeLabel(folders, props.place)}
          {props.tag && ` · #${props.tag}`}
        </span>
        <ChevronDown className="text-muted-foreground" />
      </Button>
      <Sheet open={open} onOpenChange={setOpen}>
        <SheetContent side="left" className="w-72 gap-0 md:hidden" data-testid="media-library-sheet">
          <SheetHeader className="border-b">
            <SheetTitle>Library</SheetTitle>
            <SheetDescription className="sr-only">Pick a place, a folder or a tag.</SheetDescription>
          </SheetHeader>
          <nav aria-label="Library" className="min-h-0 flex-1 overflow-y-auto p-2">
            <LibraryNav
              {...props}
              onPlace={(p) => {
                props.onPlace(p)
                setOpen(false)
              }}
              onTag={(t) => {
                props.onTag(t)
                setOpen(false)
              }}
              onNewFolder={() => {
                setOpen(false)
                props.onNewFolder()
              }}
            />
          </nav>
        </SheetContent>
      </Sheet>
    </>
  )
}
