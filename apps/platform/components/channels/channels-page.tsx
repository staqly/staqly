'use client'

import { type QueryClient, useMutation, useQueryClient } from '@tanstack/react-query'
import {
  Check,
  Ellipsis,
  ExternalLink,
  Folder,
  FolderInput,
  FolderPlus,
  Pencil,
  PlugZap,
  Plus,
  Power,
  RefreshCw,
  Settings2,
  Share2,
  Trash2,
  TriangleAlert,
} from 'lucide-react'
import dynamic from 'next/dynamic'
import Link from 'next/link'
import { useRouter, useSearchParams } from 'next/navigation'
import { Fragment, memo, useEffect, useId, useMemo, useRef, useState } from 'react'

import { ConnectErrorDialog } from '@/components/channels/connect-error-dialog'
import { GroupIcon, GroupIconPicker } from '@/components/channels/group-icon'
import { ConfirmDialog } from '@/components/common/confirm-dialog'
import { EmptyState, ErrorState } from '@/components/common/error-state'
import { PageContainer, PageHeader } from '@/components/common/page-header'
import { SkeletonList } from '@/components/common/skeleton-list'
import { ChannelStatusBadge } from '@/components/common/status-badge'
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
  DropdownMenuSub,
  DropdownMenuSubContent,
  DropdownMenuSubTrigger,
  DropdownMenuTrigger,
} from '@/components/ui/dropdown-menu'
import { Field, FieldDescription, FieldError, FieldGroup, FieldLabel } from '@/components/ui/field'
import { Input } from '@/components/ui/input'
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from '@/components/ui/select'
import { useLazyDialog } from '@/hooks/use-lazy-dialog'
import { useScrollFade } from '@/hooks/use-scroll-fade'
import { api, errorMessage, type Channel, type ChannelGroup, type Provider } from '@/lib/api'
import { setDragChip } from '@/lib/drag-chip'
import { formatDateTime, pluralize, timeAgo } from '@/lib/format'
import { LINKS_HELP, LINKS_LABELS, type LinksMode } from '@/lib/links'
import { namedErrorToast, namedToast, toastApiError } from '@/lib/notify'
import { ProviderIcon } from '@/lib/providers'
import { qk, refetchPostListsFromStart, useChannelGroups, useChannels, useProviders } from '@/lib/queries'
import { safeHttpUrl } from '@/lib/safe'
import { TONES } from '@/lib/tones'
import { cn } from '@/lib/utils'

import { ChannelAvatar } from './channel-avatars'
import { ConnectLinkButton } from './connect-link-dialog'

// The platform grid and connect forms load when Add channel is first opened.
const AddChannelSheet = dynamic(() => import('./add-channel-sheet').then((m) => m.AddChannelSheet), { ssr: false })

/**
 * A channel the API answered with (PATCH): written into the cached list, keeping the activity only the list
 * carries, and the group counts follow a move. No refetch, so a moved row stays where it was dropped.
 */
function syncChannel(qc: QueryClient, c: Channel) {
  const before = qc.getQueryData<Channel[]>(qk.channels)?.find((x) => x.id === c.id)

  qc.setQueryData<Channel[]>(qk.channels, (list) =>
    list?.map((x) => (x.id === c.id ? { ...x, ...c, activity: c.activity ?? x.activity } : x)),
  )

  if (before && before.group_id !== c.group_id) {
    countInGroups(qc, before.group_id, -1)
    countInGroups(qc, c.group_id, 1)
  }
}

function countInGroups(qc: QueryClient, groupId: string, by: number) {
  qc.setQueryData<ChannelGroup[]>(qk.channelGroups, (groups) =>
    groups?.map((g) => (g.id === groupId ? { ...g, channels: Math.max(0, g.channels + by) } : g)),
  )
}

/** Channel settings: the name, its group and what the channel does with links. A short form, so a modal; one Save. */
function ChannelSettingsDialog({
  channel,
  provider,
  groups,
  open,
  onOpenChange,
}: {
  channel: Channel
  provider?: Provider
  groups: ChannelGroup[]
  open: boolean
  onOpenChange: (o: boolean) => void
}) {
  const qc = useQueryClient()
  const [name, setName] = useState(channel.name)
  const [links, setLinks] = useState<LinksMode>(channel.links)
  const [group, setGroup] = useState(channel.group_id)
  const modes = provider?.limits?.link_modes ?? ['keep', 'remove', 'defang']
  const dirty = name.trim() !== channel.name || links !== channel.links || group !== channel.group_id
  const save = useMutation({
    mutationFn: () =>
      api.updateChannel(channel.id, {
        name: name.trim(),
        settings: { ...channel.settings, links },
        ...(group !== channel.group_id ? { group_id: group } : {}),
      }),
    onSuccess: (c) => {
      syncChannel(qc, c)
      namedToast(c.name, 'was saved')
      onOpenChange(false)
    },
    onError: (e) => toastApiError(e),
  })

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent className="sm:max-w-md">
        <DialogHeader>
          <DialogTitle>Channel settings</DialogTitle>
          <DialogDescription className="flex min-w-0 items-center gap-2">
            <ChannelAvatar channel={channel} className="size-5" />
            <span className="truncate">
              {channel.name}
              {channel.handle ? ` · ${channel.handle}` : ''}
            </span>
          </DialogDescription>
        </DialogHeader>
        <div>
          <form
            id="channel-settings-form"
            className="grid grid-cols-[minmax(0,1fr)] gap-4"
            onSubmit={(e) => {
              e.preventDefault()
              save.mutate()
            }}
          >
            <FieldGroup className="gap-4">
              <Field>
                <FieldLabel htmlFor="channel-name" required>
                  Name
                </FieldLabel>
                <Input id="channel-name" value={name} onChange={(e) => setName(e.target.value)} aria-required />
                <FieldDescription>The name is only for you; the platform keeps its own.</FieldDescription>
              </Field>
              {groups.length > 1 && (
                <Field>
                  <FieldLabel htmlFor="channel-group">Group</FieldLabel>
                  <Select value={group} onValueChange={setGroup}>
                    <SelectTrigger id="channel-group" className="w-full" data-testid="channel-group">
                      <SelectValue />
                    </SelectTrigger>
                    <SelectContent>
                      {groups.map((g) => (
                        <SelectItem key={g.id} value={g.id}>
                          {g.name}
                        </SelectItem>
                      ))}
                    </SelectContent>
                  </Select>
                  <FieldDescription>Every channel is in one group, for example one group per client.</FieldDescription>
                </Field>
              )}
              <Field>
                <FieldLabel htmlFor="channel-links">Links in posts</FieldLabel>
                <Select value={links} onValueChange={(v) => setLinks(v as LinksMode)}>
                  <SelectTrigger id="channel-links" className="w-full" data-testid="channel-links">
                    <SelectValue />
                  </SelectTrigger>
                  <SelectContent>
                    {modes.map((m) => (
                      <SelectItem key={m} value={m}>
                        {LINKS_LABELS[m]}
                      </SelectItem>
                    ))}
                  </SelectContent>
                </Select>
                <FieldDescription>
                  {LINKS_HELP[links]}
                  {!modes.includes('keep') &&
                    ` ${provider?.name ?? 'This platform'} charges more for posts with links and shows them to fewer people, so links never go out as links here.`}{' '}
                  Your posts keep their text as written.
                </FieldDescription>
              </Field>
            </FieldGroup>
          </form>
        </div>
        <DialogFooter>
          <Button type="button" variant="outline" onClick={() => onOpenChange(false)}>
            Close
          </Button>
          <Button type="submit" form="channel-settings-form" disabled={save.isPending || !name.trim() || !dirty}>
            {save.isPending ? 'Saving...' : 'Save changes'}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  )
}

/** When the channel last published (linking to that post), its next scheduled post and the 30-day count. */
function ChannelActivityLine({ activity }: { activity: Channel['activity'] }) {
  if (!activity) {
    return null
  }

  const { last_published_at: last, last_post_id: lastPost, next_publish_at: next, published_30d: recent } = activity

  if (!last && !next) {
    return <span className="text-xs text-muted-foreground">No posts yet</span>
  }

  const parts = [
    last && (
      <span key="last" title={formatDateTime(last)}>
        Last post{' '}
        {lastPost ? (
          <Link
            href={`/posts/${lastPost}`}
            prefetch={false}
            className="text-foreground underline-offset-2 underline hover:no-underline pointer-coarse:-my-3 pointer-coarse:py-3"
          >
            {timeAgo(last)}
          </Link>
        ) : (
          timeAgo(last)
        )}
      </span>
    ),
    next && (
      <span key="next" title={formatDateTime(next)}>
        Next post {timeAgo(next)}
      </span>
    ),
    recent > 0 && <span key="recent">{pluralize(recent, 'post')} in 30 days</span>,
  ].filter(Boolean)

  // One line: "Last post 2 hours ago · 1 post in 30 days" (wraps only on narrow screens).
  return (
    <span
      className="flex flex-wrap items-center gap-x-1.5 text-xs text-muted-foreground sm:flex-nowrap sm:whitespace-nowrap"
      data-testid="channel-activity"
    >
      {parts.map((part, i) => (
        <Fragment key={(part as React.ReactElement).key}>
          {i > 0 && <span aria-hidden>·</span>}
          {part}
        </Fragment>
      ))}
    </span>
  )
}

/** Drag data of a channel row, so a group section only accepts channels. */
const CHANNEL_DRAG = 'application/x-staqly-channel'

/** What follows the pointer while a channel is dragged: a small chip with its picture and name. */
function setChannelDragImage(e: React.DragEvent, channel: Channel) {
  setDragChip(e, channel.name, 'round')
}

/**
 * Why a channel needs reconnecting: a small muted note with a warning icon, two lines at most. The full text shows
 * on hover (title) and with More when it is cut; the status badge names it as its description, so a screen reader
 * reads it with the status.
 */
function ChannelErrorNote({ id, message }: { id: string; message: string }) {
  const [open, setOpen] = useState(false)
  const [cut, setCut] = useState(false)
  const text = useRef<HTMLSpanElement>(null)

  // Whether the two lines hide part of the text, again when the row changes width (phones, a resized window).
  useEffect(() => {
    const el = text.current

    if (!el || open || typeof ResizeObserver === 'undefined') {
      return
    }

    const measure = () => setCut(el.scrollHeight > el.clientHeight + 1)
    const watch = new ResizeObserver(measure)

    watch.observe(el)

    return () => watch.disconnect()
  }, [open])

  return (
    <span
      title={message}
      data-testid="channel-error"
      className="mt-1.5 flex items-start gap-1.5 rounded-md border bg-muted/50 px-2 py-1 text-xs text-muted-foreground"
    >
      <TriangleAlert aria-hidden className={cn('mt-px size-3.5 shrink-0', TONES.warning.text)} />
      <span id={id} ref={text} className={cn('min-w-0 flex-1 wrap-anywhere', !open && 'line-clamp-2')}>
        {message}
      </span>
      {(cut || open) && (
        <button
          type="button"
          aria-expanded={open}
          aria-controls={id}
          onClick={() => setOpen(!open)}
          className="shrink-0 rounded-sm font-medium text-foreground underline-offset-2 outline-ring hover:underline focus-visible:outline-1 pointer-coarse:-my-3 pointer-coarse:py-3"
        >
          {open ? 'Less' : 'More'}
        </button>
      )}
    </span>
  )
}

/** What a row can ask the page for; one stable object for every row. */
interface RowActions {
  settings: (c: Channel) => void
  test: (c: Channel) => void
  toggle: (c: Channel) => void
  move: (c: Channel, group: ChannelGroup) => void
  remove: (c: Channel) => void
  reconnect: (c: Channel) => void
}

/**
 * One connected channel, memoised: it gets the channel, its provider, the groups, whether one of its actions
 * runs, and the page's stable actions (the mutations and the dialogs live once on the page, not per row).
 */
const ChannelRow = memo(function ChannelRow({
  channel,
  provider,
  groups,
  draggable = false,
  busy = false,
  on,
}: {
  channel: Channel
  provider?: Provider
  groups: ChannelGroup[]
  /** With several groups a row can be dragged onto another group; its menu's Move to group does the same. */
  draggable?: boolean
  /** One of its actions is running. */
  busy?: boolean
  on: RowActions
}) {
  const [dragging, setDragging] = useState(false)
  const profile = channel.profile_url ? safeHttpUrl(channel.profile_url) : null
  const noteId = useId()
  const noteShown = channel.status === 'needs_reconnect' && !!channel.last_error

  return (
    // Name and platform on the left (two lines), the post activity in its own column, then the menu. Phones put the
    // activity under the name.
    // Dragging is a mouse extra; keyboards and touch use Move to group in the row menu.
    // oxlint-disable-next-line jsx-a11y/no-noninteractive-element-interactions
    <li
      className={cn(
        'grid grid-cols-[auto_minmax(0,1fr)_auto] items-center gap-x-3 gap-y-1 px-3 py-2.5 sm:grid-cols-[auto_minmax(0,1fr)_auto_auto] sm:gap-x-6',
        draggable && 'cursor-grab active:cursor-grabbing',
        dragging && 'opacity-40',
      )}
      data-testid="channel-row"
      draggable={draggable}
      onDragStart={(e) => {
        e.dataTransfer.setData(CHANNEL_DRAG, channel.id)
        e.dataTransfer.effectAllowed = 'move'
        setChannelDragImage(e, channel)
        setDragging(true)
      }}
      onDragEnd={() => setDragging(false)}
    >
      <ChannelAvatar channel={channel} className="size-10" />
      <span className="min-w-0">
        <span className="flex flex-wrap items-center gap-2">
          <span className="truncate text-sm font-medium">{channel.name}</span>
          <ChannelStatusBadge
            status={channel.status}
            title={(!noteShown && channel.last_error) || undefined}
            describedBy={noteShown ? noteId : undefined}
          />
        </span>
        <span className="mt-0.5 flex flex-wrap items-center gap-x-1.5 text-xs text-muted-foreground">
          {/* The avatar already carries the platform mark: the handle is enough (the platform name without one). */}
          <span className="truncate">{channel.handle || provider?.name || channel.provider}</span>
          {profile && (
            // The label goes on the Button so its tooltip shows it; asChild passes it on to the link.
            <Button
              asChild
              size="icon-xs"
              variant="ghost"
              aria-label={`Open the profile on ${provider?.name ?? 'the platform'}`}
              className="text-muted-foreground hover:text-foreground"
            >
              <a href={profile} target="_blank" rel="noopener noreferrer nofollow">
                <ExternalLink />
              </a>
            </Button>
          )}
        </span>
        {noteShown && <ChannelErrorNote id={noteId} message={channel.last_error} />}
      </span>
      <div className="max-sm:col-start-2 max-sm:row-start-2">
        <ChannelActivityLine activity={channel.activity} />
      </div>
      <div className="flex items-center gap-1.5 max-sm:col-start-3 max-sm:row-start-1">
        {channel.status === 'needs_reconnect' && (
          <Button size="sm" onClick={() => on.reconnect(channel)}>
            <PlugZap /> Reconnect
          </Button>
        )}
        <DropdownMenu>
          <DropdownMenuTrigger asChild>
            <Button size="icon" variant="ghost" aria-label={`Actions for ${channel.name}`}>
              <Ellipsis />
            </Button>
          </DropdownMenuTrigger>
          <DropdownMenuContent align="end">
            <DropdownMenuItem onSelect={() => on.settings(channel)}>
              <Pencil /> Settings
            </DropdownMenuItem>
            <DropdownMenuItem onSelect={() => on.test(channel)} disabled={busy}>
              <RefreshCw /> Test connection
            </DropdownMenuItem>
            {channel.status !== 'needs_reconnect' && (
              <DropdownMenuItem onSelect={() => on.toggle(channel)} disabled={busy}>
                <Power /> {channel.status === 'disabled' ? 'Enable' : 'Disable'}
              </DropdownMenuItem>
            )}
            {channel.status === 'needs_reconnect' && (
              <DropdownMenuItem onSelect={() => on.reconnect(channel)}>
                <PlugZap /> Reconnect
              </DropdownMenuItem>
            )}
            {channel.status !== 'needs_reconnect' &&
              provider?.auth !== 'oauth2' &&
              (provider?.credential_fields?.length ?? 0) > 0 && (
                <DropdownMenuItem onSelect={() => on.reconnect(channel)}>
                  <Settings2 /> Edit connection
                </DropdownMenuItem>
              )}
            {groups.length > 1 && (
              <>
                <DropdownMenuSeparator />
                <DropdownMenuSub>
                  <DropdownMenuSubTrigger>
                    <FolderInput /> Move to group
                  </DropdownMenuSubTrigger>
                  <DropdownMenuSubContent>
                    {groups.map((g) => (
                      <DropdownMenuItem
                        key={g.id}
                        disabled={busy || g.id === channel.group_id}
                        onSelect={() => on.move(channel, g)}
                      >
                        {g.id === channel.group_id ? <Check /> : <span aria-hidden className="size-4" />}
                        {g.name}
                      </DropdownMenuItem>
                    ))}
                  </DropdownMenuSubContent>
                </DropdownMenuSub>
              </>
            )}
            <DropdownMenuSeparator />
            <DropdownMenuItem variant="destructive" onSelect={() => on.remove(channel)}>
              <Trash2 /> Remove
            </DropdownMenuItem>
          </DropdownMenuContent>
        </DropdownMenu>
      </div>
    </li>
  )
})

/** New group, or a new name or icon for one: a short form, so a modal. */
function GroupNameDialog({
  group,
  open,
  onOpenChange,
}: {
  /** Edit this group; without one, create a group. */
  group?: ChannelGroup
  open: boolean
  onOpenChange: (o: boolean) => void
}) {
  const qc = useQueryClient()
  const [name, setName] = useState(group?.name ?? '')
  const [icon, setIcon] = useState(group?.icon ?? '')
  const [error, setError] = useState('')
  const save = useMutation({
    mutationFn: () => {
      // Only what changed, so an unchanged icon isn't sent back.
      const body = { name: name.trim(), ...(icon !== (group?.icon ?? '') ? { icon } : {}) }

      return group ? api.updateChannelGroup(group.id, body) : api.createChannelGroup(body)
    },
    onSuccess: (g) => {
      // A name, an icon or a new empty group: the channels don't change.
      void qc.invalidateQueries({ queryKey: qk.channelGroups })
      namedToast(g.name, group ? 'was saved' : 'was created')
      onOpenChange(false)
    },
    onError: (e) => setError(errorMessage(e)),
  })

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent className="sm:max-w-md">
        <DialogHeader>
          <DialogTitle>{group ? 'Edit group' : 'New group'}</DialogTitle>
          <DialogDescription>Group channels as you work with them, for example one group per client.</DialogDescription>
        </DialogHeader>
        <form
          noValidate
          className="grid gap-4"
          onSubmit={(e) => {
            e.preventDefault()

            if (!name.trim()) {
              setError('Give the group a name, such as Client A.')

              return
            }

            save.mutate()
          }}
        >
          <Field data-invalid={!!error}>
            <FieldLabel htmlFor="group-name" required>
              Name
            </FieldLabel>
            <div className="flex gap-2">
              <Input
                id="group-name"
                maxLength={60}
                placeholder="Client A"
                value={name}
                aria-invalid={!!error}
                aria-required
                onChange={(e) => setName(e.target.value)}
              />
              <GroupIconPicker value={icon} onChange={setIcon} />
            </div>
            <FieldError>{error}</FieldError>
          </Field>
          <DialogFooter>
            <Button type="button" variant="ghost" onClick={() => onOpenChange(false)}>
              Cancel
            </Button>
            <Button type="submit" disabled={save.isPending}>
              {save.isPending ? 'Saving...' : group ? 'Save' : 'Create group'}
            </Button>
          </DialogFooter>
        </form>
      </DialogContent>
    </Dialog>
  )
}

const groupHref = (g: ChannelGroup) => (g.default ? '/channels' : `/channels?group=${g.id}`)

/** Accepts channel rows dragged onto a group (a mouse extra; Move to group in the row menu does the same). */
function useChannelDrop(onDrop: (channelId: string) => void) {
  const [over, setOver] = useState(false)

  return {
    over,
    props: {
      onDragOver: (e: React.DragEvent) => {
        if (e.dataTransfer.types.includes(CHANNEL_DRAG)) {
          e.preventDefault()
          e.dataTransfer.dropEffect = 'move'
          setOver(true)
        }
      },
      onDragLeave: (e: React.DragEvent) => {
        if (!e.currentTarget.contains(e.relatedTarget as Node | null)) {
          setOver(false)
        }
      },
      onDrop: (e: React.DragEvent) => {
        const id = e.dataTransfer.getData(CHANNEL_DRAG)

        setOver(false)

        if (id) {
          e.preventDefault()
          onDrop(id)
        }
      },
    },
  }
}

// The count sits at the right edge; on hover (or with the keyboard in the row) the menu takes its place.
const paneRow =
  'flex min-h-9 items-center gap-2 rounded-lg py-1.5 pr-2.5 pl-2 text-sm transition-colors hover:bg-muted pointer-coarse:min-h-10 pointer-coarse:pr-10'

/** One group in the pane: its icon, name and channel count, a menu, and a drop target for channel rows. */
function GroupRow({
  group,
  count,
  active,
  onEdit,
  onDelete,
  onDropChannel,
}: {
  group: ChannelGroup
  count: number
  active: boolean
  onEdit: () => void
  onDelete: () => void
  onDropChannel: (channelId: string) => void
}) {
  const drop = useChannelDrop(onDropChannel)

  return (
    // oxlint-disable-next-line jsx-a11y/no-noninteractive-element-interactions
    <li
      className={cn('group/row relative rounded-lg', drop.over && 'bg-brand-soft/40 ring-2 ring-brand/50')}
      data-testid="channel-group"
      {...drop.props}
    >
      <Link
        prefetch={false}
        href={groupHref(group)}
        aria-current={active ? 'page' : undefined}
        title={group.name}
        className={cn(paneRow, active && 'bg-muted font-medium')}
      >
        <span aria-hidden className="flex size-4 shrink-0 items-center justify-center text-muted-foreground">
          <GroupIcon group={group} className="size-4" />
        </span>
        <span className="min-w-0 flex-1 truncate">{group.name}</span>
        <span className="shrink-0 text-xs text-muted-foreground tabular-nums group-focus-within/row:invisible group-hover/row:invisible pointer-coarse:visible">
          {count}
        </span>
      </Link>
      <DropdownMenu>
        <DropdownMenuTrigger asChild>
          <Button
            size="icon-sm"
            variant="ghost"
            aria-label={`Actions for the group ${group.name}`}
            className="absolute top-1/2 right-1 -translate-y-1/2 opacity-0 group-focus-within/row:opacity-100 group-hover/row:opacity-100 data-[state=open]:opacity-100 pointer-coarse:opacity-100"
          >
            <Ellipsis />
          </Button>
        </DropdownMenuTrigger>
        <DropdownMenuContent align="start">
          <DropdownMenuItem onSelect={onEdit}>
            <Pencil /> Edit
          </DropdownMenuItem>
          {!group.default && (
            <DropdownMenuItem variant="destructive" onSelect={onDelete}>
              <Trash2 /> Delete group
            </DropdownMenuItem>
          )}
        </DropdownMenuContent>
      </DropdownMenu>
    </li>
  )
}

/** The groups pane, like the collections pane of Posts: every group with its count, Default first. */
function GroupsPane({
  groups,
  counts,
  selected,
  onCreate,
  onEdit,
  onDelete,
  onDropChannel,
}: {
  groups: ChannelGroup[]
  counts: Map<string, number>
  selected: string
  onCreate: () => void
  onEdit: (g: ChannelGroup) => void
  onDelete: (g: ChannelGroup) => void
  onDropChannel: (channelId: string, g: ChannelGroup) => void
}) {
  return (
    <nav
      aria-label="Channel groups"
      data-testid="groups-pane"
      className="hidden min-h-0 flex-col border-r bg-sidebar/40 md:sticky md:top-0 md:flex md:h-svh md:w-75 md:shrink-0"
    >
      <div className="flex items-center justify-between gap-2 border-b px-3 py-3">
        <h2 className="flex items-center gap-2 text-sm font-semibold">
          <Folder className="size-4 text-muted-foreground" aria-hidden /> Groups
        </h2>
        <Button size="sm" variant="outline" onClick={onCreate} data-testid="new-group">
          <Plus /> New group
        </Button>
      </div>
      <ul className="grid min-h-0 flex-1 content-start gap-0.5 overflow-y-auto overscroll-contain p-2">
        {groups.map((g) => (
          <GroupRow
            key={g.id}
            group={g}
            count={counts.get(g.id) ?? 0}
            active={g.id === selected}
            onEdit={() => onEdit(g)}
            onDelete={() => onDelete(g)}
            onDropChannel={(id) => onDropChannel(id, g)}
          />
        ))}
      </ul>
    </nav>
  )
}

/** Phones: the groups as a strip of chips over the channels. */
function GroupsStrip({
  groups,
  selected,
  onCreate,
}: {
  groups: ChannelGroup[]
  selected: string
  onCreate: () => void
}) {
  const ref = useRef<HTMLDivElement>(null)
  const fade = useScrollFade(ref)
  const chip = (g: ChannelGroup, label: React.ReactNode) => (
    <Link
      key={g.id}
      prefetch={false}
      href={groupHref(g)}
      aria-current={g.id === selected ? 'page' : undefined}
      className={cn(
        'inline-flex h-9 max-w-48 shrink-0 items-center gap-1.5 rounded-full border px-3 text-sm whitespace-nowrap pointer-coarse:h-10',
        g.id === selected ? 'border-foreground bg-foreground text-background' : 'bg-background text-foreground',
      )}
    >
      {label}
    </Link>
  )

  return (
    <nav aria-label="Channel groups" className="border-b md:hidden" data-testid="groups-strip">
      <div
        ref={ref}
        data-fade={fade.fade ?? undefined}
        className={cn('scrollbar-none flex items-center gap-1.5 overflow-x-auto px-4 py-2', fade.className)}
      >
        <button
          type="button"
          onClick={onCreate}
          aria-label="New group"
          className="flex size-9 shrink-0 items-center justify-center rounded-full border text-muted-foreground pointer-coarse:size-10"
        >
          <Plus className="size-4" />
        </button>
        {groups.map((g) =>
          chip(
            g,
            <>
              <GroupIcon group={g} className="size-3.5 shrink-0" />
              <span className="truncate">{g.name}</span>
            </>,
          ),
        )}
      </div>
    </nav>
  )
}

/**
 * Connected channels with Add channel, by platform. With more groups than Default, a pane (a strip of chips on
 * phones) lists them like the collections of Posts and one group shows at a time: Default on /channels, another
 * on /channels?group=<id>.
 */
export function ChannelsPage() {
  const channels = useChannels()
  const providers = useProviders()
  const search = useSearchParams()
  const router = useRouter()
  // ?add=1 (the getting-started checklist) opens the platform grid at once.
  const add = useLazyDialog(search.get('add') === '1')
  // A connection that came back with an error (from the OAuth callback page).
  const [connectError, setConnectError] = useState(() => {
    const error = search.get('connect_error')

    return error ? { error, provider: search.get('provider') ?? '' } : null
  })
  const [reconnect, setReconnect] = useState<Channel | null>(null)
  const groupsQuery = useChannelGroups()
  const channelGroups = useMemo(() => groupsQuery.data ?? [], [groupsQuery.data])
  // ?group=<id> shows one group; without it (or for a group that is gone) Default. With Default alone, every channel.
  const groupParam = search.get('group')
  const groupFilter =
    channelGroups.length < 2
      ? 'all'
      : ((channelGroups.find((g) => g.id === groupParam) ?? channelGroups.find((g) => g.default))?.id ?? 'all')
  const [naming, setNaming] = useState<{ group?: ChannelGroup } | null>(null)
  const [deleting, setDeleting] = useState<ChannelGroup | null>(null)
  const qc = useQueryClient()
  const [editing, setEditing] = useState<Channel | null>(null)
  const [removing, setRemoving] = useState<Channel | null>(null)
  const removeGroup = useMutation({
    mutationFn: (g: ChannelGroup) => api.deleteChannelGroup(g.id),
    onSuccess: (_, g) => {
      const home = channelGroups.find((x) => x.default)

      // Its channels move to Default.
      if (home) {
        qc.setQueryData<Channel[]>(qk.channels, (list) =>
          list?.map((c) => (c.group_id === g.id ? { ...c, group_id: home.id } : c)),
        )
      }

      void qc.invalidateQueries({ queryKey: qk.channelGroups })
      namedToast(g.name, 'was deleted', 'Its channels moved to Default.')
      setDeleting(null)

      if (groupFilter === g.id) {
        router.replace('/channels', { scroll: false })
      }
    },
    onError: (e) => toastApiError(e),
  })
  const {
    mutate: moveChannel,
    isPending: moving,
    variables: moved,
  } = useMutation({
    mutationFn: ({ id, group }: { id: string; group: ChannelGroup }) => api.updateChannel(id, { group_id: group.id }),
    onSuccess: (c, { group }) => {
      syncChannel(qc, c)
      namedToast(c.name, `moved to ${group.name}`)
    },
    onError: (e) => toastApiError(e),
  })
  const {
    mutate: testChannel,
    isPending: testing,
    variables: tested,
  } = useMutation({
    mutationFn: (c: Channel) => api.testChannel(c.id),
    onSuccess: (r, c) => {
      // The test may change the channel's state (needs reconnect, or connected again): one small request.
      void qc.invalidateQueries({ queryKey: qk.channels })

      if (r.ok) {
        namedToast(c.name, 'works', r.profile?.handle ? `Signed in as ${r.profile.handle}.` : undefined)
      } else {
        namedErrorToast(c.name, 'needs to be reconnected', r.error || undefined)
      }
    },
    onError: (e) => toastApiError(e),
  })
  const {
    mutate: toggleChannel,
    isPending: toggling,
    variables: toggled,
  } = useMutation({
    mutationFn: (c: Channel) => api.updateChannel(c.id, { status: c.status === 'disabled' ? 'connected' : 'disabled' }),
    onSuccess: (c) => {
      syncChannel(qc, c)
      namedToast(c.name, c.status === 'disabled' ? 'is disabled' : 'is enabled again')
    },
    onError: (e) => toastApiError(e),
  })
  const del = useMutation({
    mutationFn: (c: Channel) => api.deleteChannel(c.id),
    onSuccess: (_, c) => {
      qc.setQueryData<Channel[]>(qk.channels, (list) => list?.filter((x) => x.id !== c.id))
      countInGroups(qc, c.group_id, -1)
      // Every list from its first page: invalidating would reload each loaded page one by one.
      refetchPostListsFromStart(qc)
      namedToast(c.name, 'was removed', 'Scheduled posts lose this channel.')
      setRemoving(null)
    },
    onError: (e) => toastApiError(e),
  })
  // The row whose action runs (its menu items wait); the other rows keep their props.
  const busyId = testing ? tested?.id : toggling ? toggled?.id : moving ? moved?.id : undefined
  // One stable object for every row: it reads the latest mutations from a ref, so rows never re-render for it.
  const latest = useRef({ moveChannel, testChannel, toggleChannel, setOpen: add.setOpen })

  useEffect(() => {
    latest.current = { moveChannel, testChannel, toggleChannel, setOpen: add.setOpen }
  })

  const [rowActions] = useState<RowActions>(() => ({
    settings: setEditing,
    test: (c) => latest.current.testChannel(c),
    toggle: (c) => latest.current.toggleChannel(c),
    move: (c, group) => latest.current.moveChannel({ id: c.id, group }),
    remove: setRemoving,
    reconnect: (c) => {
      setReconnect(c)
      latest.current.setOpen(true)
    },
  }))
  const all = channels.data
  const byGroups = channelGroups.length > 1
  const shown = useMemo(
    () => (all ?? []).filter((c) => groupFilter === 'all' || c.group_id === groupFilter),
    [all, groupFilter],
  )
  const byProvider = new Map<string, Channel[]>()

  for (const c of shown) {
    byProvider.set(c.provider, [...(byProvider.get(c.provider) ?? []), c])
  }

  const groups = [...byProvider.entries()].toSorted(([a], [b]) => a.localeCompare(b))
  const counts = new Map<string, number>()

  for (const c of all ?? []) {
    counts.set(c.group_id, (counts.get(c.group_id) ?? 0) + 1)
  }

  const providerById = useMemo(() => new Map((providers.data ?? []).map((p) => [p.id, p])), [providers.data])
  const providerOf = (id: string) => providerById.get(id)
  const row = (c: Channel) => (
    <ChannelRow
      key={c.id}
      channel={c}
      provider={providerById.get(c.provider)}
      groups={channelGroups}
      draggable={byGroups}
      busy={busyId === c.id}
      on={rowActions}
    />
  )

  const openGroup = channelGroups.find((g) => g.id === groupFilter)

  return (
    <div className="flex min-h-0 flex-1 flex-col md:flex-row" data-testid="channels-workspace">
      {byGroups && (
        <GroupsPane
          groups={channelGroups}
          counts={counts}
          selected={groupFilter}
          onCreate={() => setNaming({})}
          onEdit={(g) => setNaming({ group: g })}
          onDelete={setDeleting}
          onDropChannel={(id, g) => {
            if (all?.find((c) => c.id === id)?.group_id !== g.id) {
              moveChannel({ id, group: g })
            }
          }}
        />
      )}
      <div className="flex min-w-0 flex-1 flex-col">
        {byGroups && <GroupsStrip groups={channelGroups} selected={groupFilter} onCreate={() => setNaming({})} />}
        <PageContainer>
          <PageHeader
            title={openGroup?.name ?? 'Channels'}
            icon={
              openGroup && (openGroup.icon || openGroup.default) ? (
                <GroupIcon group={openGroup} className="size-4" />
              ) : (
                <Share2 className="size-4" />
              )
            }
            description="The accounts your posts go to: connect as many per platform as you need; every post picks its channels."
            actions={
              <>
                {!byGroups && (
                  <Button variant="outline" onClick={() => setNaming({})} data-testid="new-group">
                    <FolderPlus /> New group
                  </Button>
                )}
                <ConnectLinkButton />
                <Button onClick={() => add.setOpen(true)} data-testid="add-channel">
                  <Plus /> Add channel
                </Button>
              </>
            }
          />
          <div className="mt-6">
            {channels.isLoading ? (
              <SkeletonList count={4} className="h-14 rounded-lg" />
            ) : channels.error ? (
              <ErrorState error={channels.error} onRetry={() => channels.refetch()} />
            ) : groups.length === 0 && openGroup ? (
              <p className="rounded-xl border border-dashed px-3 py-4 text-sm text-muted-foreground">
                No channels in this group. Drag one onto it in the pane, or use Move to group in its menu.
              </p>
            ) : groups.length === 0 ? (
              <EmptyState
                icon={<Share2 />}
                title="No channels yet"
                description="Connect the first platform. Your posts can then go there at their time, with retries when the platform is slow."
                action={
                  <Button onClick={() => add.setOpen(true)}>
                    <Plus /> Add channel
                  </Button>
                }
              />
            ) : (
              <div className="grid gap-6">
                {groups.map(([providerId, list]) => (
                  <section
                    key={providerId}
                    aria-label={providerOf(providerId)?.name ?? providerId}
                    className="grid gap-2"
                  >
                    <h2 className="flex items-center gap-2 text-sm font-semibold">
                      <ProviderIcon id={providerId} className="size-4" /> {providerOf(providerId)?.name ?? providerId}
                      <span className="font-normal text-muted-foreground">{list.length}</span>
                    </h2>
                    <ul className="divide-y rounded-xl border" data-testid={`channels-${providerId}`}>
                      {list.map(row)}
                    </ul>
                  </section>
                ))}
              </div>
            )}
          </div>
          {add.used && (
            <AddChannelSheet
              open={add.open}
              onOpenChange={(o) => {
                add.setOpen(o)

                if (!o) {
                  setReconnect(null)
                }
              }}
              reconnect={reconnect}
            />
          )}
          {connectError && (
            <ConnectErrorDialog
              {...connectError}
              onClose={() => {
                setConnectError(null)
                window.history.replaceState(null, '', '/channels')
              }}
            />
          )}
          {editing && (
            <ChannelSettingsDialog
              // A fresh form per channel.
              key={editing.id}
              channel={editing}
              provider={providerById.get(editing.provider)}
              groups={channelGroups}
              open
              onOpenChange={(o) => !o && setEditing(null)}
            />
          )}
          <ConfirmDialog
            open={!!removing}
            onOpenChange={(o) => !o && setRemoving(null)}
            title={`Remove ${removing?.name ?? ''}?`}
            description="Staqly forgets the sign-in and the channel. Posts already published stay on the platform; scheduled posts lose this channel."
            confirmLabel={del.isPending ? 'Removing...' : 'Remove'}
            variant="destructive"
            onConfirm={() => removing && del.mutate(removing)}
          />
          {naming && <GroupNameDialog group={naming.group} open onOpenChange={(o) => !o && setNaming(null)} />}
          <ConfirmDialog
            open={!!deleting}
            onOpenChange={(o) => !o && setDeleting(null)}
            title={`Delete the group ${deleting?.name ?? ''}?`}
            description={`Its ${pluralize(deleting?.channels ?? 0, 'channel')} move to Default. The channels and their posts stay.`}
            confirmLabel={removeGroup.isPending ? 'Deleting...' : 'Delete group'}
            variant="destructive"
            onConfirm={() => deleting && removeGroup.mutate(deleting)}
          />
        </PageContainer>
      </div>
    </div>
  )
}
