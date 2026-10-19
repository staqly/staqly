'use client'

import { useMutation, useQueryClient } from '@tanstack/react-query'
import {
  BarChart3,
  CalendarX2,
  Copy,
  ExternalLink,
  Eye,
  FolderInput,
  Hash,
  Link2,
  MessageSquare,
  MoreHorizontal,
  PenSquare,
  Send,
  Trash2,
  X,
} from 'lucide-react'
import Link from 'next/link'
import { useRouter } from 'next/navigation'
import { memo, useState } from 'react'
import { toast } from 'sonner'

import { ChannelStack } from '@/components/channels/channel-avatars'
import { ConfirmDialog } from '@/components/common/confirm-dialog'
import { MultiSelect } from '@/components/common/multi-select'
import { PostStatusBadge } from '@/components/common/status-badge'
import { Button } from '@/components/ui/button'
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from '@/components/ui/dialog'
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuSeparator,
  DropdownMenuTrigger,
} from '@/components/ui/dropdown-menu'
import { Field, FieldLabel } from '@/components/ui/field'
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from '@/components/ui/select'
import { api, type Channel, type Post } from '@/lib/api'
import { formatDateTime, timeAgo } from '@/lib/format'
import { namedToast, toastApiError } from '@/lib/notify'
import { canReschedule, isLocked, postTitle, publishedTargets } from '@/lib/posts'
import { undoDelayMs, undoToast } from '@/lib/publish-undo'
import { invalidateAfterPostChange, removePostFromLists, syncPostInCache, useCollections, useTags } from '@/lib/queries'
import { safeHttpUrl } from '@/lib/safe'
import { formatSchedule } from '@/lib/schedule'
import { normalizeTag } from '@/lib/tags'
import { cn } from '@/lib/utils'

import { PostStats, ShareDialog } from './post-actions'

/**
 * The columns of a post row, shared by the header. Wide lists (the list's container from 48rem) get every column:
 * checkbox, post, status, when, channels, created, feedback, actions. Narrower ones keep a compact row (channels
 * or the checkbox, the post with its time under it, the status, the actions menu).
 */
const COLUMNS =
  'grid grid-cols-[auto_minmax(0,1fr)_auto_auto] items-center gap-x-3 @3xl:grid-cols-[1.25rem_minmax(0,1fr)_6.5rem_8.5rem_5.5rem_9rem_2.5rem_auto]'

export type PostAction = 'preview' | 'duplicate' | 'delete' | 'publish' | 'unschedule' | 'stats' | 'copy-link'

/** The column titles above the rows (wide lists only). */
export function PostListHeader({ allSelected, onSelectAll }: { allSelected: boolean; onSelectAll?: () => void }) {
  return (
    <div
      aria-hidden
      className={cn(
        COLUMNS,
        'sticky top-0 z-10 hidden border-b bg-background/95 px-4 py-1.5 text-xs text-muted-foreground backdrop-blur @3xl:grid',
      )}
    >
      <span>
        {onSelectAll && (
          <button
            type="button"
            tabIndex={-1}
            onClick={onSelectAll}
            className={cn(
              'flex size-4 items-center justify-center rounded border border-input',
              allSelected && 'border-primary bg-primary text-primary-foreground',
            )}
          >
            {allSelected && <CheckMark />}
          </button>
        )}
      </span>
      <span>Post</span>
      <span>Status</span>
      <span>When</span>
      <span>Channels</span>
      <span>Created</span>
      <span className="sr-only">Feedback</span>
      <span className="w-[8.5rem]" />
    </div>
  )
}

function CheckMark() {
  return (
    <svg viewBox="0 0 16 16" aria-hidden className="size-3" fill="none" stroke="currentColor" strokeWidth={2.5}>
      <path d="M3.5 8.5l3 3 6-7" />
    </svg>
  )
}

/** One post as a row. A memo row: it takes the post, plain flags and stable callbacks only. */
export const PostRow = memo(function PostRow({
  post,
  channels,
  selected,
  selecting,
  onToggle,
  onAction,
}: {
  post: Post
  channels: Channel[] | undefined
  selected: boolean
  selecting: boolean
  /** Undefined: this person cannot change posts (no checkbox, no write actions). */
  onToggle?: (id: string, range: boolean) => void
  onAction: (action: PostAction, post: Post) => void
}) {
  const targets = (post.targets ?? []).map((t) => ({
    id: t.channel_id,
    provider: t.provider,
    name: t.channel_name,
    avatar_url: channels?.find((c) => c.id === t.channel_id)?.avatar_url,
  }))
  const at = post.published_at ?? post.publish_at
  const when = at ? formatSchedule(at) : null
  const excerpt = post.content.split('\n').slice(1).join(' ').trim()
  const showBox = selecting || selected
  const box = onToggle && (
    <button
      type="button"
      role="checkbox"
      aria-checked={selected}
      aria-label={`Select ${postTitle(post, 60)}`}
      onClick={(e) => onToggle(post.id, e.shiftKey)}
      data-testid="post-select"
      className={cn(
        'relative z-10 flex size-4 shrink-0 items-center justify-center rounded border border-input bg-background transition-opacity outline-none focus-visible:opacity-100 focus-visible:ring-2 focus-visible:ring-ring',
        'after:absolute after:-inset-3 pointer-coarse:after:-inset-4',
        selected && 'border-primary bg-primary text-primary-foreground',
        !showBox && 'opacity-0 group-hover:opacity-100 pointer-coarse:opacity-100',
      )}
    >
      {selected && <CheckMark />}
    </button>
  )

  return (
    <li
      data-testid="post-row"
      data-selected={selected || undefined}
      className={cn(
        COLUMNS,
        'group relative px-4 py-2 transition-colors hover:bg-muted/60 pointer-coarse:min-h-12',
        selected && 'bg-brand-soft/40 hover:bg-brand-soft/60',
      )}
    >
      {/* Narrow: the channels, turning into the checkbox on hover or while selecting. Wide: the checkbox column. */}
      <span className="relative flex h-8 min-w-8 items-center justify-center @3xl:h-auto @3xl:min-w-0 @3xl:justify-start">
        <span className={cn('@3xl:hidden', onToggle && showBox && 'invisible', onToggle && 'group-hover:invisible')}>
          <ChannelStack channels={targets} max={2} size="size-7" />
        </span>
        {box && <span className="absolute inset-0 flex items-center justify-center @3xl:static">{box}</span>}
      </span>

      <span className="min-w-0">
        {/* The whole row opens the post (the link stretches over it); buttons sit above it. */}
        <Link
          prefetch={false}
          href={`/posts/${post.id}`}
          className="block truncate text-sm font-medium outline-none after:absolute after:inset-0 focus-visible:after:rounded-md focus-visible:after:ring-2 focus-visible:after:ring-ring focus-visible:after:ring-inset"
          data-testid="post-link"
        >
          {postTitle(post)}
        </Link>
        {excerpt && (
          <span className="hidden truncate text-xs text-muted-foreground @3xl:block" title={excerpt}>
            {excerpt}
          </span>
        )}
        {/* Narrow rows put the time under the title. */}
        <span
          className="mt-0.5 block truncate text-xs text-muted-foreground @3xl:hidden"
          title={formatDateTime(at ?? post.created_at)}
        >
          {when ?? `Created ${timeAgo(post.created_at)}`}
        </span>
      </span>

      <span>
        <PostStatusBadge status={post.status} />
      </span>
      <span
        className={cn(
          'hidden truncate text-xs tabular-nums @3xl:block',
          post.status === 'scheduled' ? 'font-medium text-foreground' : 'text-muted-foreground',
        )}
        title={at ? `${formatDateTime(at)} (${timeAgo(at)})` : undefined}
      >
        {when ?? '-'}
      </span>
      <span className="hidden @3xl:block">
        <ChannelStack channels={targets} size="size-6" />
      </span>
      <span
        className="hidden min-w-0 truncate text-xs text-muted-foreground @3xl:block"
        title={formatDateTime(post.created_at)}
      >
        {timeAgo(post.created_at)}
        {post.created_by && <span className="block truncate">by {post.created_by}</span>}
      </span>
      <span className="hidden @3xl:block">
        {post.open_comments > 0 && (
          <button
            type="button"
            onClick={() => onAction('preview', post)}
            aria-label={`${post.open_comments} open ${post.open_comments === 1 ? 'comment' : 'comments'}`}
            className="relative z-10 inline-flex items-center gap-1 rounded-md px-1 text-xs font-medium text-foreground tabular-nums outline-none hover:bg-muted focus-visible:ring-2 focus-visible:ring-ring"
          >
            <MessageSquare className="size-3.5" /> {post.open_comments}
          </button>
        )}
      </span>

      <RowActions post={post} canWrite={!!onToggle} onAction={onAction} />
    </li>
  )
})

/** Preview, Duplicate and Delete as icons (on hover, always on touch), the rest in a menu. */
function RowActions({
  post,
  canWrite,
  onAction,
}: {
  post: Post
  canWrite: boolean
  onAction: (action: PostAction, post: Post) => void
}) {
  const links = publishedTargets(post).flatMap((t) => {
    const url = safeHttpUrl(t.external_url)

    return url ? [{ ...t, url }] : []
  })
  const quick =
    'hidden @3xl:inline-flex opacity-0 group-hover:opacity-100 focus-visible:opacity-100 pointer-coarse:opacity-100'

  return (
    <span className="relative z-10 flex items-center justify-end gap-0.5 @3xl:w-[8.5rem]">
      <Button
        type="button"
        size="icon"
        variant="ghost"
        className={quick}
        aria-label="Preview and feedback"
        onClick={() => onAction('preview', post)}
      >
        <Eye />
      </Button>
      {canWrite && (
        <>
          <Button
            type="button"
            size="icon"
            variant="ghost"
            className={quick}
            aria-label="Duplicate"
            onClick={() => onAction('duplicate', post)}
          >
            <Copy />
          </Button>
          <Button
            type="button"
            size="icon"
            variant="ghost"
            className={cn(quick, 'hover:text-destructive')}
            aria-label="Delete"
            onClick={() => onAction('delete', post)}
            data-testid="post-row-delete"
          >
            <Trash2 />
          </Button>
        </>
      )}
      <DropdownMenu>
        <DropdownMenuTrigger asChild>
          <Button type="button" size="icon" variant="ghost" aria-label={`More actions for ${postTitle(post, 40)}`}>
            <MoreHorizontal />
          </Button>
        </DropdownMenuTrigger>
        <DropdownMenuContent align="end">
          <DropdownMenuItem asChild>
            <Link href={`/posts/${post.id}`} prefetch={false}>
              <PenSquare /> {isLocked(post) ? 'Open' : 'Edit'}
            </Link>
          </DropdownMenuItem>
          {/* Narrow lists have no icon row: the same actions live here. */}
          <DropdownMenuItem className="@3xl:hidden" onSelect={() => onAction('preview', post)}>
            <Eye /> Preview and feedback
          </DropdownMenuItem>
          {canWrite && (
            <DropdownMenuItem className="@3xl:hidden" onSelect={() => onAction('duplicate', post)}>
              <Copy /> Duplicate
            </DropdownMenuItem>
          )}
          {canWrite && canReschedule(post) && (post.targets?.length ?? 0) > 0 && (
            <DropdownMenuItem onSelect={() => onAction('publish', post)}>
              <Send /> Publish now
            </DropdownMenuItem>
          )}
          {canWrite && post.status === 'scheduled' && (
            <DropdownMenuItem onSelect={() => onAction('unschedule', post)}>
              <CalendarX2 /> Cancel schedule
            </DropdownMenuItem>
          )}
          {links.map((t) => (
            <DropdownMenuItem key={t.channel_id} asChild>
              <a href={t.url} target="_blank" rel="noopener noreferrer">
                <ExternalLink /> Open on {t.channel_name}
              </a>
            </DropdownMenuItem>
          ))}
          {links.length > 0 && (
            <DropdownMenuItem onSelect={() => onAction('stats', post)}>
              <BarChart3 /> Statistics
            </DropdownMenuItem>
          )}
          <DropdownMenuItem onSelect={() => onAction('copy-link', post)}>
            <Link2 /> Copy link
          </DropdownMenuItem>
          {canWrite && (
            <>
              <DropdownMenuSeparator />
              <DropdownMenuItem variant="destructive" className="@3xl:hidden" onSelect={() => onAction('delete', post)}>
                <Trash2 /> Delete
              </DropdownMenuItem>
            </>
          )}
        </DropdownMenuContent>
      </DropdownMenu>
    </span>
  )
}

const label = (text: string) => <span className="hidden sm:inline">{text}</span>

const posts = (n: number) => `${n} ${n === 1 ? 'post' : 'posts'}`

/**
 * One set of dialogs and mutations for every row of a list (rows only call `onAction`), so a long list does not
 * mount a dialog, a menu content or a mutation per post.
 */
export function usePostRowActions() {
  const qc = useQueryClient()
  const router = useRouter()
  const [open, setOpen] = useState<{ action: 'preview' | 'stats' | 'delete'; post: Post } | null>(null)
  const duplicate = useMutation({
    mutationFn: (p: Post) => api.duplicatePost(p.id),
    onSuccess: (p) => {
      invalidateAfterPostChange(qc, p.id, { usage: true })
      namedToast(postTitle(p, 40), 'was duplicated as a draft')
      router.push(`/posts/${p.id}`)
    },
    onError: (e) => toastApiError(e),
  })
  const del = useMutation({
    mutationFn: (p: Post) => api.deletePost(p.id),
    onSuccess: (_, p) => {
      removePostFromLists(qc, p.id)
      invalidateAfterPostChange(qc, undefined, { usage: true })
      namedToast(postTitle(p, 40), 'was deleted')
      setOpen(null)
    },
    onError: (e) => toastApiError(e),
  })
  // Publish now waits for an Undo like the composer's: the post is scheduled a few seconds ahead.
  const publish = useMutation({
    mutationFn: (p: Post) => api.schedulePost(p.id, new Date(Date.now() + undoDelayMs()).toISOString()),
    onSuccess: (p) => {
      syncPostInCache(qc, p)
      undoToast(p, () => void api.post(p.id).then((fresh) => syncPostInCache(qc, fresh)))
    },
    onError: (e) => toastApiError(e),
  })
  const unschedule = useMutation({
    mutationFn: (p: Post) => api.cancelPost(p.id),
    onSuccess: (p) => {
      syncPostInCache(qc, p)
      namedToast(postTitle(p, 40), 'is a draft again')
    },
    onError: (e) => toastApiError(e),
  })
  const { mutate: dup } = duplicate
  const { mutate: pub } = publish
  const { mutate: unsched } = unschedule

  // setOpen and the mutate functions are stable: rows can take this callback without re-rendering.
  const [onAction] = useState(() => (action: PostAction, post: Post) => {
    switch (action) {
      case 'preview':
      case 'stats':
      case 'delete':
        setOpen({ action, post })
        break
      case 'duplicate':
        dup(post)
        break
      case 'publish':
        pub(post)
        break
      case 'unschedule':
        unsched(post)
        break
      case 'copy-link':
        void navigator.clipboard
          ?.writeText(`${window.location.origin}/posts/${post.id}`)
          .then(() => toast.success('Link copied'))
          .catch(() => toast.error('Could not copy the link'))
        break
    }
  })
  const close = (o: boolean) => !o && setOpen(null)
  const dialogs = (
    <>
      {open?.action === 'preview' && <ShareDialog post={open.post} open onOpenChange={close} />}
      {open?.action === 'stats' && (
        <Dialog open onOpenChange={close}>
          <DialogContent className="sm:max-w-xl">
            <DialogHeader>
              <DialogTitle>Post statistics</DialogTitle>
              <DialogDescription>{postTitle(open.post, 60)}</DialogDescription>
            </DialogHeader>
            <PostStats post={open.post} active />
          </DialogContent>
        </Dialog>
      )}
      <ConfirmDialog
        open={open?.action === 'delete'}
        onOpenChange={close}
        title="Delete this post?"
        description={
          open && publishedTargets(open.post).length > 0
            ? 'It is removed from Staqly only: what was published stays on the platforms.'
            : 'It will not be published.'
        }
        confirmLabel={del.isPending ? 'Deleting...' : 'Delete'}
        variant="destructive"
        onConfirm={() => open && del.mutate(open.post)}
      />
    </>
  )

  return { onAction, dialogs }
}

/** Runs one call per post and reports how many worked; the rest are toasted once. */
async function eachPost(list: Post[], run: (p: Post) => Promise<Post | void>) {
  const done: Post[] = []
  let failed = 0

  for (const p of list) {
    try {
      // One at a time on purpose: a long selection shouldn't burst into the per-minute request limit.
      // oxlint-disable-next-line no-await-in-loop -- sequential by design
      const out = await run(p)

      done.push(out ?? p)
    } catch {
      failed++
    }
  }

  return { done, failed }
}

/**
 * The floating bar for selected posts: Duplicate, Move to collection, Tags, Cancel schedule and Delete, each for all
 * of them. Posts an action does not apply to (a published post can't move or change tags; only scheduled ones can be
 * taken off the schedule) are skipped, and the toast says how many.
 */
export function PostSelectionBar({
  selected,
  canWrite,
  onClear,
}: {
  selected: Post[]
  canWrite: boolean
  onClear: () => void
}) {
  const qc = useQueryClient()
  const [dialog, setDialog] = useState<'delete' | 'move' | 'tags' | null>(null)
  const editable = selected.filter((p) => !isLocked(p))
  const scheduled = selected.filter((p) => p.status === 'scheduled')
  const anyPublished = selected.some((p) => publishedTargets(p).length > 0)
  const skipped = (n: number) => (n > 0 ? `${posts(n)} skipped` : undefined)
  const finish = (verb: string, total: number, ok: number, failed: number) => {
    invalidateAfterPostChange(qc, undefined, { usage: true })
    toast.success(`${posts(ok)} ${verb}`, {
      description: [skipped(total - ok - failed), failed > 0 ? `${failed} failed` : ''].filter(Boolean).join(', '),
    })
    setDialog(null)
    onClear()
  }
  const remove = useMutation({
    mutationFn: () => api.deletePosts(selected.map((p) => p.id)),
    onSuccess: (res) => {
      for (const id of res.deleted) {
        removePostFromLists(qc, id)
      }

      finish('deleted', selected.length, res.deleted.length, 0)
    },
    onError: (e) => toastApiError(e),
  })
  const duplicate = useMutation({
    mutationFn: () => eachPost(selected, (p) => api.duplicatePost(p.id)),
    onSuccess: ({ done, failed }) => finish('duplicated as drafts', selected.length, done.length, failed),
  })
  const unschedule = useMutation({
    mutationFn: () => eachPost(scheduled, (p) => api.cancelPost(p.id)),
    onSuccess: ({ done, failed }) => {
      done.forEach((p) => syncPostInCache(qc, p))
      finish('taken off the schedule', selected.length, done.length, failed)
    },
  })
  const move = useMutation({
    mutationFn: (collection: string) => eachPost(editable, (p) => api.updatePost(p.id, { collection })),
    onSuccess: ({ done, failed }) => finish('moved', selected.length, done.length, failed),
  })
  const retag = useMutation({
    mutationFn: ({ add, drop }: { add: string[]; drop: string[] }) =>
      eachPost(editable, (p) =>
        api.updatePost(p.id, { tags: [...new Set([...p.tags.filter((t) => !drop.includes(t)), ...add])] }),
      ),
    onSuccess: ({ done, failed }) => {
      done.forEach((p) => syncPostInCache(qc, p))
      finish('updated', selected.length, done.length, failed)
    },
  })
  const busy = remove.isPending || duplicate.isPending || unschedule.isPending || move.isPending || retag.isPending

  return (
    <>
      <div
        role="toolbar"
        aria-label="Selected posts"
        data-testid="posts-selection"
        className="sticky bottom-[calc(4.5rem+env(safe-area-inset-bottom))] z-20 mx-auto mt-auto mb-2 flex w-fit max-w-[calc(100%-2rem)] animate-in items-center gap-1 overflow-x-auto rounded-xl border bg-popover p-1.5 text-popover-foreground shadow-lg duration-200 fade-in slide-in-from-bottom-4 md:bottom-6"
      >
        <Button variant="ghost" size="icon" aria-label="Clear selection" onClick={onClear}>
          <X />
        </Button>
        <span className="shrink-0 px-1 text-sm font-medium whitespace-nowrap tabular-nums">
          {selected.length} selected
        </span>
        {canWrite && (
          <>
            <span aria-hidden className="mx-1 h-5 w-px shrink-0 bg-border" />
            <Button variant="ghost" aria-label="Duplicate" disabled={busy} onClick={() => duplicate.mutate()}>
              <Copy /> {label('Duplicate')}
            </Button>
            <Button
              variant="ghost"
              aria-label="Move to collection"
              disabled={busy || editable.length === 0}
              onClick={() => setDialog('move')}
            >
              <FolderInput /> {label('Move')}
            </Button>
            <Button
              variant="ghost"
              aria-label="Tags"
              disabled={busy || editable.length === 0}
              onClick={() => setDialog('tags')}
            >
              <Hash /> {label('Tags')}
            </Button>
            <Button
              variant="ghost"
              aria-label="Cancel schedule"
              disabled={busy || scheduled.length === 0}
              onClick={() => unschedule.mutate()}
            >
              <CalendarX2 /> {label('Cancel schedule')}
            </Button>
            <span aria-hidden className="mx-1 h-5 w-px shrink-0 bg-border" />
            <Button
              variant="destructive"
              aria-label="Delete"
              disabled={busy}
              onClick={() => setDialog('delete')}
              data-testid="posts-bulk-delete"
            >
              <Trash2 /> {label('Delete')}
            </Button>
          </>
        )}
      </div>
      <ConfirmDialog
        open={dialog === 'delete'}
        onOpenChange={(o) => !o && setDialog(null)}
        title={`Delete ${posts(selected.length)}?`}
        description={
          anyPublished
            ? 'Published posts are removed from Staqly only: what went out stays on the platforms. The rest will not be published.'
            : 'They will not be published.'
        }
        confirmLabel={remove.isPending ? 'Deleting...' : `Delete ${posts(selected.length)}`}
        variant="destructive"
        onConfirm={() => remove.mutate()}
      />
      {dialog === 'move' && (
        <MoveDialog
          count={editable.length}
          skipped={selected.length - editable.length}
          pending={move.isPending}
          onClose={() => setDialog(null)}
          onMove={(c) => move.mutate(c)}
        />
      )}
      {dialog === 'tags' && (
        <TagsDialog
          count={editable.length}
          current={[...new Set(editable.flatMap((p) => p.tags))]}
          pending={retag.isPending}
          onClose={() => setDialog(null)}
          onApply={(add, drop) => retag.mutate({ add, drop })}
        />
      )}
    </>
  )
}

function MoveDialog({
  count,
  skipped,
  pending,
  onClose,
  onMove,
}: {
  count: number
  skipped: number
  pending: boolean
  onClose: () => void
  onMove: (collection: string) => void
}) {
  const collections = useCollections()
  const [to, setTo] = useState('')

  return (
    <Dialog open onOpenChange={(o) => !o && onClose()}>
      <DialogContent className="sm:max-w-md">
        <DialogHeader>
          <DialogTitle>Move {posts(count)}</DialogTitle>
          <DialogDescription>
            {skipped > 0
              ? `${posts(skipped)} already published or publishing stay where they are.`
              : 'Pick where they go.'}
          </DialogDescription>
        </DialogHeader>
        <Field>
          <FieldLabel htmlFor="move-to">Collection</FieldLabel>
          <Select value={to} onValueChange={setTo}>
            <SelectTrigger id="move-to">
              <SelectValue placeholder="Choose a collection" />
            </SelectTrigger>
            <SelectContent>
              {(collections.data ?? []).map((c) => (
                <SelectItem key={c.id} value={c.id}>
                  {c.name}
                </SelectItem>
              ))}
            </SelectContent>
          </Select>
        </Field>
        <DialogFooter>
          <Button variant="outline" onClick={onClose}>
            Cancel
          </Button>
          <Button disabled={!to || pending} onClick={() => onMove(to)}>
            {pending ? 'Moving...' : 'Move'}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  )
}

function TagsDialog({
  count,
  current,
  pending,
  onClose,
  onApply,
}: {
  count: number
  current: string[]
  pending: boolean
  onClose: () => void
  onApply: (add: string[], drop: string[]) => void
}) {
  const known = useTags()
  const [add, setAdd] = useState<string[]>([])
  const [drop, setDrop] = useState<string[]>([])

  return (
    <Dialog open onOpenChange={(o) => !o && onClose()}>
      <DialogContent className="sm:max-w-md">
        <DialogHeader>
          <DialogTitle>Tags of {posts(count)}</DialogTitle>
          <DialogDescription>Add tags to all of them, or take tags off.</DialogDescription>
        </DialogHeader>
        <Field>
          <FieldLabel htmlFor="tags-add">Add tags</FieldLabel>
          <MultiSelect
            id="tags-add"
            label="Add tags"
            value={add}
            onChange={setAdd}
            placeholder="Add tags"
            options={(known.data ?? []).map((t) => ({ value: t.name, label: `#${t.name}` }))}
            creatable={{ normalize: normalizeTag }}
          />
        </Field>
        <Field>
          <FieldLabel htmlFor="tags-drop">Remove tags</FieldLabel>
          <MultiSelect
            id="tags-drop"
            label="Remove tags"
            value={drop}
            onChange={setDrop}
            placeholder={current.length ? 'Remove tags' : 'No tags on these posts'}
            disabled={current.length === 0}
            options={current.map((t) => ({ value: t, label: `#${t}` }))}
          />
        </Field>
        <DialogFooter>
          <Button variant="outline" onClick={onClose}>
            Cancel
          </Button>
          <Button disabled={(add.length === 0 && drop.length === 0) || pending} onClick={() => onApply(add, drop)}>
            {pending ? 'Saving...' : 'Apply'}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  )
}
