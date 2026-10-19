'use client'

import { useMutation, useQueryClient } from '@tanstack/react-query'
import {
  AlertTriangle,
  CalendarClock,
  CheckCircle2,
  ChevronDown,
  Trash2,
  MessageSquarePlus,
  Clock3,
  ChevronUp,
  ExternalLink,
  Repeat2,
  Globe,
  ImagePlus,
  RotateCcw,
  Send,
  X,
  Pencil,
} from 'lucide-react'
import Link from 'next/link'
import { useMemo, useRef, useState } from 'react'
import { flushSync } from 'react-dom'

import { xPostsWarning } from '@/components/billing/x-posts'
import { ChannelAvatar } from '@/components/channels/channel-avatars'
import { ProviderFields, visibleFields } from '@/components/channels/provider-fields'
import { Callout } from '@/components/common/callout'
import { DateTimeFields, formatShort } from '@/components/common/date-time-picker'
import { MultiSelect } from '@/components/common/multi-select'
import { PostStatusBadge, TargetStatusBadge } from '@/components/common/status-badge'
import { useEditedMedia } from '@/components/media-editor/edited-media'
import { LibraryPickerButton } from '@/components/media/library-picker'
import { PostStats } from '@/components/posts/post-actions'
import { Button } from '@/components/ui/button'
import { ButtonGroup } from '@/components/ui/button-group'
import { Collapsible, CollapsibleContent, CollapsibleTrigger } from '@/components/ui/collapsible'
import { FieldError } from '@/components/ui/field'
import { Input } from '@/components/ui/input'
import { Label, RequiredMark } from '@/components/ui/label'
import { Popover, PopoverContent, PopoverTrigger } from '@/components/ui/popover'
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from '@/components/ui/select'
import { Sheet, SheetContent, SheetDescription, SheetHeader, SheetTitle } from '@/components/ui/sheet'
import { Tabs, TabsContent, TabsList, TabsTrigger } from '@/components/ui/tabs'
import { useNow } from '@/hooks/use-now'
import {
  api,
  mediaThumbUrl,
  type Channel,
  type ChannelGroup,
  type Media,
  type Post,
  type PostInput,
  type PostTarget,
  type Provider,
  type ProviderField,
} from '@/lib/api'
import { apiFieldErrors } from '@/lib/form-errors'
import { formatDateTime, timeAgo } from '@/lib/format'
import { applyLinks, linksNotice } from '@/lib/links'
import { namedToast, toastApiError } from '@/lib/notify'
import { charCount, isLocked, postTitle } from '@/lib/posts'
import { ProviderIcon } from '@/lib/providers'
import { undoDelayMs, undoToast } from '@/lib/publish-undo'
import {
  invalidateAfterPostChange,
  refetchPostListsFromStart,
  syncPostInCache,
  useChannelGroups,
  useChannels,
  useCollections,
  useProviders,
  useMode,
  useTags,
  useXPosts,
} from '@/lib/queries'
import { safeHttpUrl } from '@/lib/safe'
import { formatSchedule, formatTime, nextFullHour } from '@/lib/schedule'
import { normalizeTag } from '@/lib/tags'
import { cn } from '@/lib/utils'

import { PostEditor } from './post-editor'

/** How often a published post comes back: a copy is scheduled that much later (the API's repeat_every). */
const REPEAT_OPTIONS = [
  ['never', "Don't repeat"],
  ['1d', 'Every day'],
  ['2d', 'Every 2 days'],
  ['3d', 'Every 3 days'],
  ['4d', 'Every 4 days'],
  ['5d', 'Every 5 days'],
  ['6d', 'Every 6 days'],
  ['1w', 'Every week'],
  ['2w', 'Every 2 weeks'],
  ['1mo', 'Every month'],
] as const

interface Override {
  /** null: the channel uses the shared text. */
  content: string | null
  settings: Record<string, string>
}

interface Count {
  channel: Channel
  /** Characters the channel receives: after its links mode took links out or made them plain text. */
  n: number
  max: number
  /** The links the channel's links mode changes, as written. */
  links: string[]
}

/** A channel's count for this text, as the API will count it at publish time. */
function countFor(channel: Channel, text: string, provider?: Provider): Count {
  const out = applyLinks(channel.links, text)

  return { channel, n: charCount(out.text), max: provider?.limits?.max_chars ?? 0, links: out.found }
}

/** Under the editor: every picked channel that changes this text's links, named, with the links. */
function LinksNotices({ counts }: { counts: Count[] }) {
  const changed = counts.filter((c) => c.links.length)

  if (!changed.length) {
    return null
  }

  return (
    <ul className="grid gap-1" data-testid="links-notices">
      {changed.map(({ channel, links }) => (
        <li key={channel.id} className="flex items-start gap-1.5 text-sm text-destructive">
          <AlertTriangle className="mt-0.5 size-4 shrink-0" aria-hidden />
          <span className="min-w-0 wrap-anywhere">
            {linksNotice(channel.links, channel.name)} ({links.join(', ')})
          </span>
        </li>
      ))}
    </ul>
  )
}

/**
 * The counter in the editor's corner: the tightest channel's count with a tick, or a warning past a limit or
 * when a channel takes links out; the popover lists every picked channel.
 */
function Counter({ counts }: { counts: Count[] }) {
  if (!counts.length) {
    return null
  }

  const limited = counts.filter((c) => c.max > 0)
  const tight = limited.toSorted((a, b) => a.max - a.n - (b.max - b.n))[0]
  const over = limited.some((c) => c.n > c.max) || counts.some((c) => c.links.length > 0)
  const Icon = over ? AlertTriangle : CheckCircle2

  return (
    <Popover>
      <PopoverTrigger asChild>
        <Button
          type="button"
          variant="outline"
          size="sm"
          className={cn('tabular-nums', over && 'border-destructive text-destructive')}
          aria-label="Characters per channel"
          data-testid="counter"
        >
          <Icon className={over ? 'text-destructive' : 'text-emerald-600 dark:text-emerald-400'} />
          {tight ? `${tight.n}/${tight.max}` : counts[0]!.n}
          <ChevronDown className="opacity-60" />
        </Button>
      </PopoverTrigger>
      <PopoverContent align="end" className="w-auto min-w-64 p-2" data-testid="counters">
        <ul className="grid gap-1 text-sm">
          {counts.map(({ channel, n, max, links }) => (
            <li
              key={channel.id}
              className={cn(
                'grid gap-1 px-1 py-0.5',
                links.length > 0 && 'rounded-md border border-destructive px-2 py-1.5 text-destructive',
              )}
            >
              <span className="flex items-center gap-2">
                <ChannelAvatar channel={channel} className="size-5" />
                <span className="min-w-0 flex-1 truncate">{channel.name}</span>
                <span
                  className={cn(
                    'tabular-nums',
                    (max > 0 && n > max) || links.length ? 'text-destructive' : 'text-muted-foreground',
                  )}
                >
                  {max > 0 ? `${n}/${max}` : n}
                </span>
              </span>
              {links.length > 0 && (
                <span className="max-w-72 text-xs wrap-anywhere">{linksNotice(channel.links, channel.name)}</span>
              )}
            </li>
          ))}
        </ul>
      </PopoverContent>
    </Popover>
  )
}

/** Every connected channel as an avatar; picked ones are in colour with a ring, the rest greyed out. */
const NO_GROUPS: ChannelGroup[] = []

function ChannelPicker({
  channels,
  providers,
  selected,
  onChange,
  disabled,
  invalid,
}: {
  channels: Channel[]
  providers: Provider[]
  selected: Set<string>
  onChange: (ids: string[]) => void
  /** A post that went out: the channels show, nothing changes. */
  disabled?: boolean
  invalid?: boolean
}) {
  // With channel groups, the picker lists the channels under their group's name, in the groups' order.
  const groups = useChannelGroups().data ?? NO_GROUPS
  const groupIndex = new Map(groups.map((g, i) => [g.id, i]))
  const groupName = (c: Channel) => (groups.length > 1 ? groups.find((g) => g.id === c.group_id)?.name : undefined)

  if (!channels.length) {
    return (
      <p className="text-sm text-muted-foreground">
        No channels yet.{' '}
        <Link href="/channels" className="text-foreground underline underline-offset-2 hover:no-underline">
          Connect a channel
        </Link>{' '}
        to publish somewhere; a post without channels stays a draft.
      </p>
    )
  }

  const picked = channels.filter((c) => selected.has(c.id))
  const platform = (c: Channel) => providers.find((p) => p.id === c.provider)?.name ?? c.provider

  return (
    <MultiSelect
      id="post-channels"
      label="Channels"
      placeholder="Pick where the post goes"
      className="h-auto min-h-9 w-full py-1"
      value={[...selected]}
      onChange={onChange}
      disabled={disabled}
      invalid={invalid}
      summary={
        picked.length ? (
          <span className="flex flex-wrap gap-1.5">
            {picked.map((c) => (
              <span
                key={c.id}
                className="inline-flex items-center gap-1.5 rounded-full border bg-muted/50 py-0.5 pr-2.5 pl-0.5 text-sm"
              >
                <ChannelAvatar channel={c} className="size-5" />
                {c.name}
              </span>
            ))}
          </span>
        ) : undefined
      }
      options={channels
        .toSorted((a, b) => (groupIndex.get(a.group_id) ?? 0) - (groupIndex.get(b.group_id) ?? 0))
        .map((c) => {
          const off = c.status !== 'connected'

          return {
            value: c.id,
            label: c.name,
            group: groupName(c),
            hint: off
              ? `${platform(c)}, ${c.status === 'disabled' ? 'turned off' : 'needs reconnecting'}`
              : [platform(c), c.handle].filter(Boolean).join(' · '),
            icon: <ChannelAvatar channel={c} className="size-6" />,
            disabled: off,
            testId: `pick-channel-${c.id}`,
          }
        })}
    />
  )
}

/** Uploaded pictures and files as thumbnails with a remove button (and, for pictures, Edit in the media editor). */
function MediaStrip({
  media,
  onRemove,
  editable,
}: {
  media: Media[]
  onRemove?: (id: string) => void
  editable?: boolean
}) {
  if (!media.length) {
    return null
  }

  return (
    <div className="flex flex-wrap gap-2 px-3 pb-2">
      {media.map((m) => (
        <span key={m.id} className="relative size-16 overflow-hidden rounded-lg border bg-muted">
          {m.mime_type.startsWith('image/') ? (
            // eslint-disable-next-line @next/next/no-img-element
            <img
              src={mediaThumbUrl(m.id, 160)}
              alt={m.alt_text || m.original_name}
              className="size-full object-cover"
            />
          ) : (
            <span className="flex size-full items-center justify-center p-1 text-center text-[10px] wrap-anywhere text-muted-foreground">
              {m.original_name}
            </span>
          )}
          {editable && m.mime_type.startsWith('image/') && (
            // A new tab: this draft stays open; the edited picture replaces this one here when it is saved.
            <a
              href={`/media/edit?ids=${encodeURIComponent(m.id)}&from=composer`}
              target="_blank"
              rel="noopener"
              aria-label={`Edit ${m.original_name}`}
              data-testid="edit-attachment"
              className="absolute bottom-0.5 left-0.5 flex size-5 items-center justify-center rounded-full bg-background/90 text-foreground shadow-xs hover:bg-background pointer-coarse:size-10"
            >
              <Pencil className="size-3" />
            </a>
          )}
          {onRemove && (
            <button
              type="button"
              aria-label={`Remove ${m.original_name}`}
              onClick={() => onRemove(m.id)}
              className="absolute top-0.5 right-0.5 flex size-5 items-center justify-center rounded-full bg-background/90 text-foreground shadow-xs hover:bg-background pointer-coarse:size-10"
            >
              <X className="size-3" />
            </button>
          )}
        </span>
      ))}
    </div>
  )
}

/** The "Insert media" button: uploads and hands each file back. */
function MediaButton({ onAdd }: { onAdd: (m: Media) => void }) {
  const input = useRef<HTMLInputElement>(null)
  const upload = useMutation({
    mutationFn: (file: File) => api.uploadMedia(file),
    onSuccess: onAdd,
    onError: (e) => toastApiError(e),
  })

  return (
    <>
      <input
        ref={input}
        type="file"
        accept="image/*,video/*"
        multiple
        className="sr-only"
        tabIndex={-1}
        aria-hidden
        onChange={(e) => {
          for (const f of Array.from(e.target.files ?? [])) {
            upload.mutate(f)
          }

          e.target.value = ''
        }}
      />
      <Button
        type="button"
        variant="outline"
        size="sm"
        onClick={() => input.current?.click()}
        disabled={upload.isPending}
      >
        <ImagePlus /> {upload.isPending ? 'Uploading...' : 'Insert media'}
      </Button>
      <LibraryPickerButton onAdd={onAdd} />
    </>
  )
}

/** How one channel of a saved post went: state, the link once published, the error and Retry when failed. */
function TargetStatus({ post, target }: { post: Post; target: PostTarget }) {
  const qc = useQueryClient()
  const retry = useMutation({
    mutationFn: () => api.retryTarget(post.id, target.channel_id),
    onSuccess: (p) => {
      syncPostInCache(qc, p)
      invalidateAfterPostChange(qc, p.id)
      namedToast(target.channel_name, 'is being retried')
    },
    onError: (e) => toastApiError(e),
  })
  const url = target.external_url ? safeHttpUrl(target.external_url) : undefined

  return (
    <div className="flex flex-wrap items-center gap-x-2 gap-y-1 text-xs" data-testid="post-target">
      <TargetStatusBadge status={target.status} />
      {url && (
        <a
          href={url}
          target="_blank"
          rel="noopener noreferrer nofollow"
          className="inline-flex max-w-full min-w-0 items-center gap-1 text-brand underline underline-offset-2 pointer-coarse:-my-3 pointer-coarse:py-3"
        >
          <span className="truncate">{url}</span>
          <ExternalLink className="size-3 shrink-0" aria-hidden />
        </a>
      )}
      {target.published_at && (
        <span className="text-muted-foreground" title={formatDateTime(target.published_at)}>
          {timeAgo(target.published_at)}
        </span>
      )}
      {target.status === 'failed' && (
        <Button
          type="button"
          size="sm"
          variant="outline"
          className="ml-auto"
          onClick={() => retry.mutate()}
          disabled={retry.isPending}
        >
          <RotateCcw /> Retry
        </Button>
      )}
      {target.error && <span className="basis-full text-destructive wrap-anywhere">{target.error}</span>}
      {(target.replies?.length ?? 0) > 0 && (
        <ol className="grid basis-full gap-1 border-l pl-3" aria-label={`Comments on ${target.channel_name}`}>
          {target.replies!.map((r) => {
            const href = r.external_url ? safeHttpUrl(r.external_url) : undefined

            return (
              <li key={r.index} className="flex flex-wrap items-center gap-x-2 gap-y-0.5" data-testid="post-reply">
                <span className="text-muted-foreground">Comment {r.index + 1}</span>
                <span
                  className={cn(
                    r.status === 'published' && 'text-emerald-600 dark:text-emerald-400',
                    r.status === 'failed' && 'text-destructive',
                    (r.status === 'pending' || r.status === 'skipped') && 'text-muted-foreground',
                  )}
                >
                  {
                    {
                      pending: 'Waiting',
                      publishing: 'Publishing',
                      published: 'Published',
                      failed: 'Failed',
                      skipped: 'Not sent (this platform takes no comments)',
                    }[r.status]
                  }
                </span>
                {href && (
                  <a
                    href={href}
                    target="_blank"
                    rel="noopener noreferrer nofollow"
                    className="inline-flex items-center gap-1 text-brand underline underline-offset-2 pointer-coarse:-my-3 pointer-coarse:py-3"
                  >
                    Open <ExternalLink className="size-3" aria-hidden />
                  </a>
                )}
                {r.error && <span className="basis-full text-destructive wrap-anywhere">{r.error}</span>}
              </li>
            )
          })}
        </ol>
      )}
    </div>
  )
}

/** One comment under the post: its text, media and how long after the previous part it goes out. */
interface ThreadPart {
  uid: string
  content: string
  media: Media[]
  delay: number // seconds after the previous part
}

// Quick picks, in seconds; any other minute count goes in the custom field (the server takes up to a day).
const DELAY_PICKS = [60, 120, 300, 600, 900, 1800, 3600, 7200]
const MAX_DELAY_MINUTES = 1440

function delayLabel(seconds: number, short = false) {
  if (!seconds) {
    return 'Right after'
  }

  const m = Math.round(seconds / 60)
  const text = m % 60 === 0 ? `${m / 60} h` : m > 60 ? `${Math.floor(m / 60)} h ${m % 60} min` : `${m} min`

  return short ? text.replace(' min', 'm').replace(' h', 'h').replaceAll(' ', '') : `${text} later`
}

/** The non-default settings of a channel in a few words, for its folded card ("Who can reply: Accounts you follow"). */
/** The platform as people know it in a settings heading ("X.com advanced settings"). */
function platformLabel(provider: string, name?: string) {
  return provider === 'x' ? 'X.com' : (name ?? provider)
}

function settingsSummary(fields: ProviderField[], values: Record<string, string>, all: Channel[]) {
  return visibleFields(fields, values)
    .flatMap((f) => {
      const v = (values[f.key] ?? '').trim()

      if (!v || v === (f.default ?? '')) {
        return []
      }

      if (f.kind === 'boolean') {
        return v === 'true' ? [f.label] : []
      }

      if (f.kind === 'select') {
        return [`${f.label}: ${f.options?.find((o) => o.value === v)?.label ?? v}`]
      }

      if (f.kind === 'channels') {
        const names = v.split(',').map((id) => all.find((c) => c.id === id.trim())?.name ?? id)

        return [`${f.label}: ${names.join(', ')}`]
      }

      return [f.kind === 'textarea' ? f.label : `${f.label}: ${v}`]
    })
    .join(' · ')
}

/** When a comment goes out after the one before: quick picks, Right after, or any number of minutes. */
function DelayPicker({ value, onChange, label }: { value: number; onChange: (s: number) => void; label: string }) {
  const [open, setOpen] = useState(false)
  const [custom, setCustom] = useState('')
  const minutes = Number(custom)
  const valid = Number.isInteger(minutes) && minutes >= 1 && minutes <= MAX_DELAY_MINUTES
  const pick = (s: number) => {
    onChange(s)
    setOpen(false)
    setCustom('')
  }

  return (
    <Popover open={open} onOpenChange={setOpen}>
      <PopoverTrigger asChild>
        <Button type="button" variant="outline" size="sm" aria-label={label} className="h-7 text-xs">
          <Clock3 className="text-muted-foreground" />
          {delayLabel(value)}
          <ChevronDown className="opacity-60" />
        </Button>
      </PopoverTrigger>
      <PopoverContent align="start" className="w-64 p-2">
        <div className="grid grid-cols-4 gap-1" role="group" aria-label="Delay">
          <Button
            type="button"
            size="sm"
            variant={value === 0 ? 'secondary' : 'ghost'}
            className="col-span-4"
            aria-pressed={value === 0}
            onClick={() => pick(0)}
          >
            Right after
          </Button>
          {DELAY_PICKS.map((s) => (
            <Button
              key={s}
              type="button"
              size="sm"
              variant={value === s ? 'secondary' : 'ghost'}
              aria-pressed={value === s}
              aria-label={delayLabel(s)}
              tooltip={false}
              onClick={() => pick(s)}
            >
              {delayLabel(s, true)}
            </Button>
          ))}
        </div>
        <form
          className="mt-2 flex gap-1.5 border-t pt-2"
          onSubmit={(e) => {
            // React events cross the popover's portal: without this the composer's own form would submit too.
            e.preventDefault()
            e.stopPropagation()

            if (valid) {
              pick(minutes * 60)
            }
          }}
        >
          <Input
            type="number"
            inputMode="numeric"
            min={1}
            max={MAX_DELAY_MINUTES}
            placeholder="Minutes, up to 1440"
            aria-label="Custom delay in minutes"
            value={custom}
            onChange={(e) => setCustom(e.target.value)}
            className="h-8"
          />
          <Button type="submit" size="sm" className="h-8" disabled={!valid}>
            Set
          </Button>
        </form>
      </PopoverContent>
    </Popover>
  )
}

/**
 * The comments under a post (a thread): each part is published as a reply to the one before, after its delay.
 * Shared by every channel that can reply (X); your own automation receives them with the post.
 */
function ThreadEditor({
  parts,
  onChange,
  countsFor,
  errors,
  locked,
}: {
  parts: ThreadPart[]
  onChange: (parts: ThreadPart[]) => void
  countsFor: (text: string) => Count[]
  errors: Record<string, string | undefined>
  locked: boolean
}) {
  const set = (i: number, patch: Partial<ThreadPart>) =>
    onChange(parts.map((p, j) => (j === i ? { ...p, ...patch } : p)))
  const move = (i: number, by: number) => {
    const next = [...parts]
    const [part] = next.splice(i, 1)

    next.splice(i + by, 0, part!)
    onChange(next)
  }

  return (
    <div className="grid gap-3" data-testid="thread">
      {parts.map((part, i) => {
        const err = errors[`thread.${i}.content`] ?? errors[`thread.${i}.media`]

        return (
          <div key={part.uid} className="relative grid gap-1.5 pl-6" data-testid={`thread-part-${i}`}>
            {/* One line from the post down through every comment (it bridges the gap-3 above each one) and ends
                at the last comment's branch; the branch meets the middle of the 28 px label row. */}
            <span
              aria-hidden
              className={cn('absolute -top-3 left-2 w-px bg-border', i === parts.length - 1 ? 'h-6.5' : 'bottom-0')}
            />
            <span aria-hidden className="absolute top-3.5 left-2 h-px w-3 bg-border" />
            <div className="flex flex-wrap items-center gap-2">
              <span id={`thread-${part.uid}-label`} className="text-sm font-medium">
                Comment {i + 1}
              </span>
              {!locked && (
                <>
                  <DelayPicker
                    value={part.delay}
                    onChange={(d) => set(i, { delay: d })}
                    label={`When comment ${i + 1} goes out`}
                  />
                  <span className="ml-auto flex items-center">
                    <Button
                      type="button"
                      size="icon"
                      variant="ghost"
                      aria-label={`Move comment ${i + 1} up`}
                      tooltip="Move up"
                      disabled={i === 0}
                      onClick={() => move(i, -1)}
                    >
                      <ChevronUp />
                    </Button>
                    <Button
                      type="button"
                      size="icon"
                      variant="ghost"
                      aria-label={`Move comment ${i + 1} down`}
                      tooltip="Move down"
                      disabled={i === parts.length - 1}
                      onClick={() => move(i, 1)}
                    >
                      <ChevronDown />
                    </Button>
                    <Button
                      type="button"
                      size="icon"
                      variant="ghost"
                      aria-label={`Remove comment ${i + 1}`}
                      tooltip="Remove this comment"
                      className="text-destructive hover:text-destructive"
                      onClick={() => onChange(parts.filter((_, j) => j !== i))}
                    >
                      <Trash2 />
                    </Button>
                  </span>
                </>
              )}
            </div>
            <div className={cn('rounded-xl border bg-background', err && 'border-destructive')}>
              <PostEditor
                id={`thread-${part.uid}`}
                labelledBy={`thread-${part.uid}-label`}
                value={part.content}
                onChange={(content) => set(i, { content })}
                placeholder="Write a comment"
                invalid={!!err}
                testId={`thread-content-${i}`}
                className="post-editor--short border-0 shadow-none"
                editable={!locked}
                below={
                  <MediaStrip
                    media={part.media}
                    onRemove={locked ? undefined : (id) => set(i, { media: part.media.filter((m) => m.id !== id) })}
                  />
                }
                start={locked ? undefined : <MediaButton onAdd={(m) => set(i, { media: [...part.media, m] })} />}
                end={<Counter counts={countsFor(part.content)} />}
              />
            </div>
            <FieldError>{err}</FieldError>
          </div>
        )
      })}
      {!locked && (
        <div className={cn(parts.length > 0 && 'pl-6')}>
          <Button
            type="button"
            variant="outline"
            size="sm"
            onClick={() => onChange([...parts, { uid: crypto.randomUUID(), content: '', media: [], delay: 0 }])}
            data-testid="add-comment"
          >
            <MessageSquarePlus /> Add comment
          </Button>
        </div>
      )}
    </div>
  )
}

/** "Wed 21 Oct, 16:00": the picked time on the footer's chip. */
function chipLabel(d: Date) {
  return `${d.toLocaleDateString(undefined, { weekday: 'short', day: 'numeric', month: 'short' })}, ${formatTime(d)}`
}

/**
 * The calendar half of the footer's split button: a popover with the day, the time, quick picks and Repeat;
 * Schedule applies them (and sends the post when it can go). Focus returns to the button on close.
 */
function ScheduleMenu({
  value,
  repeat,
  disabled,
  onApply,
}: {
  value: Date | null
  repeat: string
  disabled?: boolean
  onApply: (at: Date, repeat: string) => void
}) {
  const [open, setOpen] = useState(false)
  const [at, setAt] = useState<Date | null>(null)
  const [rep, setRep] = useState('')
  const [opening, setOpening] = useState(0)
  const now = useNow(open, 30_000)
  const past = at !== null && at.getTime() <= now
  const tz = new Intl.DateTimeFormat().resolvedOptions().timeZone

  return (
    <Popover
      open={open}
      onOpenChange={(o) => {
        if (o) {
          setAt(value ?? nextFullHour(Date.now()))
          setRep(repeat)
          setOpening((n) => n + 1)
        }

        setOpen(o)
      }}
    >
      <PopoverTrigger asChild>
        <Button
          type="button"
          size="icon"
          aria-label="Schedule for later"
          aria-haspopup="dialog"
          data-testid="date-time-picker"
          disabled={disabled}
          className="size-8 border-l border-l-primary-foreground/25 pointer-coarse:size-11"
        >
          <CalendarClock />
        </Button>
      </PopoverTrigger>
      <PopoverContent
        side="top"
        align="end"
        collisionPadding={8}
        className="w-auto max-w-[calc(100vw-1rem)] gap-0 p-0 text-sm"
        aria-label="Pick a date and time"
      >
        <DateTimeFields key={opening} id="post-publish-at" value={at} onChange={setAt} />
        <div className="flex flex-wrap items-center gap-2 border-t px-3 py-2">
          <Select value={rep || 'never'} onValueChange={(v) => setRep(v === 'never' ? '' : v)}>
            <SelectTrigger aria-label="Repeat" className="w-40" data-testid="post-repeat">
              <Repeat2 className="text-muted-foreground" />
              <SelectValue />
            </SelectTrigger>
            <SelectContent>
              {REPEAT_OPTIONS.map(([v, label]) => (
                <SelectItem key={v} value={v}>
                  {label}
                </SelectItem>
              ))}
            </SelectContent>
          </Select>
          <span className="text-xs text-muted-foreground">Times are in {tz}.</span>
        </div>
        <div className="flex items-center justify-between gap-3 border-t px-3 py-2">
          <p
            className={cn('min-w-0 truncate text-sm', past && 'text-destructive')}
            aria-live="polite"
            data-testid="date-time-summary"
          >
            {past ? 'Pick a time in the future' : at ? formatShort(at) : 'No time picked'}
          </p>
          <div className="flex shrink-0 gap-1.5">
            <Button type="button" size="sm" variant="ghost" onClick={() => setOpen(false)}>
              Cancel
            </Button>
            <Button
              type="button"
              size="sm"
              disabled={!at || past}
              onClick={() => {
                setOpen(false)
                onApply(at!, rep)
              }}
            >
              Schedule
            </Button>
          </div>
        </div>
      </PopoverContent>
    </Popover>
  )
}

/**
 * Write or edit a post: the channels in a multi-select with their avatars, a tab row (all channels, then each
 * picked one, from two channels on) that switches the editor between the shared text and one channel's own
 * text, the editor with Insert media and a counter per channel, the platform's settings folded under it.
 * Footer: the collection, the time, Repeat, tags, Save draft and Schedule or Publish now.
 */
export function Composer({
  open,
  onOpenChange,
  collectionId,
  post,
  defaultPublishAt,
  defaultMedia,
  onSaved,
}: {
  open: boolean
  onOpenChange: (open: boolean) => void
  /** The collection a new post starts in. */
  collectionId?: string
  /** Edit this post instead of creating one. */
  post?: Post
  /** A time to start with (the calendar's "+" on a slot). */
  defaultPublishAt?: string
  /** Files to start with (the media library's "New post with this"). */
  defaultMedia?: Media[]
  onSaved?: (post: Post) => void
}) {
  return (
    <Sheet open={open} onOpenChange={onOpenChange}>
      <SheetContent className="gap-0 data-[side=right]:w-full data-[side=right]:sm:max-w-2xl data-[side=right]:lg:max-w-4xl">
        {open && (
          <ComposerForm
            collectionId={collectionId}
            post={post}
            defaultPublishAt={defaultPublishAt}
            defaultMedia={defaultMedia}
            onDone={(p) => {
              onOpenChange(false)
              onSaved?.(p)
            }}
          />
        )}
      </SheetContent>
    </Sheet>
  )
}

/**
 * The composer's form, also used for a saved post (PostSheet): then it shows each channel's state, link, error
 * and Retry, opens on `initialTab`, puts `headerActions` in the header and is read only once the post went out.
 */
export function ComposerForm({
  collectionId,
  post,
  defaultPublishAt,
  defaultMedia,
  initialTab,
  headerActions,
  onDone,
}: {
  collectionId?: string
  post?: Post
  defaultPublishAt?: string
  defaultMedia?: Media[]
  /** A channel id to open on (the calendar's card of that channel). */
  initialTab?: string
  headerActions?: React.ReactNode
  onDone: (post: Post) => void
}) {
  const qc = useQueryClient()
  const collections = useCollections()
  const channels = useChannels()
  const providers = useProviders()
  const [collection, setCollection] = useState(post?.collection_id ?? collectionId ?? '')
  const [content, setContent] = useState(post?.content ?? '')
  const [media, setMedia] = useState<Media[]>(post?.media_items ?? defaultMedia ?? [])

  useEditedMedia(setMedia)
  const [thread, setThread] = useState<ThreadPart[]>(() =>
    (post?.thread ?? []).map((p) => ({
      uid: crypto.randomUUID(),
      content: p.content,
      media: p.media_items ?? [],
      delay: p.delay_seconds ?? 0,
    })),
  )
  const [selected, setSelected] = useState<Set<string>>(() => new Set((post?.targets ?? []).map((t) => t.channel_id)))
  const [overrides, setOverrides] = useState<Record<string, Override>>(() =>
    Object.fromEntries(
      (post?.targets ?? []).map((t) => [t.channel_id, { content: t.content ?? null, settings: { ...t.settings } }]),
    ),
  )
  const [tags, setTags] = useState<string[]>(post?.tags ?? [])
  const [repeatEvery, setRepeatEvery] = useState<string>(post?.repeat_every ?? '')
  const knownTags = useTags()
  const [publishAt, setPublishAt] = useState<Date | null>(() => {
    const iso = post?.publish_at ?? defaultPublishAt

    return iso ? new Date(iso) : null
  })
  const [errors, setErrors] = useState<Record<string, string | undefined>>({})
  const picked = useMemo(() => (channels.data ?? []).filter((c) => selected.has(c.id)), [channels.data, selected])
  const providerOf = (ch: Channel) => providers.data?.find((p) => p.id === ch.provider)
  const textFor = (ch: Channel) => overrides[ch.id]?.content ?? content

  const setOverride = (id: string, patch: Partial<Override>) =>
    setOverrides((o) => {
      const cur: Override = o[id] ?? { content: null, settings: {} }

      return { ...o, [id]: { ...cur, ...patch } }
    })

  function input(status: 'draft' | 'scheduled'): PostInput {
    return {
      collection,
      content,
      media: media.map((m) => m.id),
      targets: picked.map((ch) => ({
        channel_id: ch.id,
        content: overrides[ch.id]?.content?.trim() ? overrides[ch.id]!.content : null,
        settings: overrides[ch.id]?.settings ?? {},
      })),
      publish_at: publishAt ? publishAt.toISOString() : null,
      tags,
      repeat_every: repeatEvery as PostInput['repeat_every'],
      thread: thread
        .filter((p) => p.content.trim() || p.media.length)
        .map((p) => ({ content: p.content, media: p.media.map((m) => m.id), delay_seconds: p.delay })),
      status,
    }
  }

  const save = useMutation({
    mutationFn: async (mode: 'draft' | 'schedule' | 'now') => {
      const body = input(mode === 'schedule' ? 'scheduled' : 'draft')
      let saved: Post

      if (post) {
        const { collection: _c, status: _s, ...patch } = body

        saved = await api.updatePost(post.id, { ...patch, expected_version: post.version })

        if (mode === 'schedule' && publishAt) {
          saved = await api.schedulePost(post.id, publishAt.toISOString())
        }
      } else {
        saved = await api.createPost(body)
      }

      // Publish now from the dashboard waits a few seconds with an Undo: the post is scheduled for that moment and
      // the queue sends it then (agents and the API still publish at once).
      if (mode === 'now') {
        saved = await api.schedulePost(saved.id, new Date(Date.now() + undoDelayMs()).toISOString())
      }

      return { saved, mode }
    },
    onSuccess: ({ saved, mode }) => {
      if (post) {
        syncPostInCache(qc, saved)
      } else {
        refetchPostListsFromStart(qc, saved.id)
      }

      invalidateAfterPostChange(qc, saved.id, { usage: !post })

      if (mode === 'now') {
        undoToast(saved, () => {
          syncPostInCache(qc, saved)
          invalidateAfterPostChange(qc, saved.id)
        })
      } else {
        namedToast(
          postTitle(saved, 40),
          mode === 'schedule' ? 'is scheduled' : post ? 'was saved' : 'was saved as a draft',
        )
      }

      onDone(saved)
    },
    onError: (e) => {
      const { errors: fields, complete } = apiFieldErrors<string>(e, (k) => k)
      const keys = Object.keys(fields)

      setErrors(fields)

      // Only a platform setting is wrong: show it where it lives.
      if (keys.length && keys.every((k) => /^targets\.[^.]+\.settings\./.test(k))) {
        setSection('settings')
      }

      if (!complete) {
        toastApiError(e)
      }
    },
  })

  const [tab, setTab] = useState<string>(initialTab ?? 'all')
  const locked = !!post && isLocked(post)
  // Post (the content), Settings (each channel's platform settings) and, once it was sent off, Delivery.
  const [section, setSection] = useState<'post' | 'settings' | 'delivery' | 'stats'>('post')
  const targetOf = (id: string) => post?.targets?.find((t) => t.channel_id === id)
  // Per-channel text only makes sense with two channels or more; with one, the shared text is that channel's.
  const multi = picked.length > 1
  const active = multi && tab !== 'all' ? picked.find((c) => c.id === tab) : undefined
  const shownTab = active?.id ?? 'all'
  const ownText = active ? (overrides[active.id]?.content ?? null) : null
  const countsFor = (ch?: Channel): Count[] => (ch ? [ch] : picked).map((c) => countFor(c, textFor(c), providerOf(c)))
  const [openSettings, setOpenSettings] = useState<Record<string, boolean>>({})
  const { isLocal } = useMode()
  const xPosts = useXPosts({ enabled: !isLocal })
  const xOut = xPosts.data && !xPosts.data.unlimited && xPosts.data.left <= 0 ? xPostsWarning(xPosts.data) : ''
  const settingsErrors = (id: string) =>
    Object.fromEntries(
      Object.entries(errors)
        .filter(([k]) => k.startsWith(`targets.${id}.settings.`))
        .map(([k, v]) => [k.slice(`targets.${id}.settings.`.length), v]),
    )
  const settingsChannels = locked ? [] : picked.filter((c) => (providerOf(c)?.settings_fields ?? []).length > 0)
  const changedSettings = settingsChannels.filter((c) =>
    settingsSummary(providerOf(c)?.settings_fields ?? [], overrides[c.id]?.settings ?? {}, channels.data ?? []),
  ).length
  const settingsInvalid = Object.keys(errors).some((k) => /^targets\.[^.]+\.settings\./.test(k))
  const deliveryTargets = post && post.status !== 'draft' ? (post.targets ?? []) : []
  const failedTargets = deliveryTargets.filter((t) => t.status === 'failed').length
  const hasStats = deliveryTargets.some((t) => t.status === 'published')
  const shownSection =
    (section === 'settings' && settingsChannels.length === 0) ||
    (section === 'delivery' && !deliveryTargets.length) ||
    (section === 'stats' && !hasStats)
      ? 'post'
      : section
  const formRef = useRef<HTMLFormElement>(null)
  // Scheduled or failed: saving is the main action, Publish now the other one. Never two saves side by side.
  const live = !!post && post.status !== 'draft'
  const now = useNow()
  const canSend = picked.length > 0 && content.trim().length > 0 && !!collection
  const busy = save.isPending
  const inPast = publishAt !== null && publishAt.getTime() <= now

  return (
    // Like every drawer: the header and the footer stay put, the fields scroll between them.
    <form
      ref={formRef}
      onSubmit={(e) => {
        e.preventDefault()
        // A draft's primary sends it (now or at the picked time); a post already on its way saves, keeping its time.
        save.mutate(publishAt ? 'schedule' : live ? 'draft' : 'now')
      }}
      className="flex min-h-0 flex-1 flex-col"
      noValidate
      data-testid="composer"
    >
      <SheetHeader className="border-b pt-[max(1rem,env(safe-area-inset-top))] pr-12 pointer-coarse:pr-14">
        <div className="flex flex-wrap items-start justify-between gap-x-4 gap-y-2">
          <div className="min-w-0 flex-1 basis-64">
            <SheetTitle className="truncate" title={post ? postTitle(post, 200) : undefined}>
              {post ? postTitle(post) : 'New post'}
            </SheetTitle>
            {post ? (
              <SheetDescription asChild>
                <div className="mt-1 flex flex-wrap items-center gap-x-3 gap-y-1 text-xs text-muted-foreground">
                  <PostStatusBadge status={post.status} />
                  <span>{post.collection_name}</span>
                  {post.publish_at && (
                    <span title={formatDateTime(post.publish_at)}>
                      {post.status === 'published' ? 'Published' : 'Goes out'}{' '}
                      <span className="font-semibold text-foreground">{formatSchedule(post.publish_at)}</span>
                    </span>
                  )}
                  <span title={formatDateTime(post.created_at)}>created {timeAgo(post.created_at)}</span>
                  {post.source && <span>via {post.source}</span>}
                </div>
              </SheetDescription>
            ) : (
              <SheetDescription>Pick the channels, write once, change it per channel if you like.</SheetDescription>
            )}
          </div>
          {headerActions && (
            <div className="flex shrink-0 items-center gap-1.5" data-testid="post-actions">
              {headerActions}
            </div>
          )}
        </div>
      </SheetHeader>
      <Tabs
        value={shownSection}
        onValueChange={(v) => setSection(v as typeof section)}
        className="flex min-h-0 flex-1 flex-col gap-0"
      >
        {(settingsChannels.length > 0 || deliveryTargets.length > 0) && (
          <div className="overflow-x-auto px-4 pt-3 pb-1">
            <TabsList aria-label="Post sections">
              <TabsTrigger value="post">Post</TabsTrigger>
              {settingsChannels.length > 0 && (
                <TabsTrigger value="settings" data-testid="tab-settings">
                  Settings
                  {settingsInvalid ? (
                    <span className="size-2 rounded-full bg-destructive" aria-label="has an error" role="img" />
                  ) : changedSettings > 0 ? (
                    <span className="rounded-full bg-muted px-1.5 text-[11px] leading-4 tabular-nums">
                      {changedSettings}
                    </span>
                  ) : null}
                </TabsTrigger>
              )}
              {deliveryTargets.length > 0 && (
                <TabsTrigger value="delivery" data-testid="tab-delivery">
                  Delivery
                  {failedTargets > 0 && (
                    <span
                      className="rounded-full bg-destructive/15 px-1.5 text-[11px] leading-4 text-destructive tabular-nums"
                      aria-label={`${failedTargets} failed`}
                    >
                      {failedTargets}
                    </span>
                  )}
                </TabsTrigger>
              )}
              {hasStats && (
                <TabsTrigger value="stats" data-testid="tab-stats">
                  Statistics
                </TabsTrigger>
              )}
            </TabsList>
          </div>
        )}
        <div className="grid min-h-0 flex-1">
          {/* Every section stays mounted (hidden when inactive), so switching never loses what was typed. */}
          <div className="grid min-h-0 content-start overflow-y-auto overscroll-contain p-4">
            <TabsContent value="post" forceMount className="grid content-start gap-4 data-[state=inactive]:hidden">
              <div className="grid gap-2">
                <Label htmlFor="post-channels" required>
                  Channels
                </Label>
                <ChannelPicker
                  channels={channels.data ?? []}
                  providers={providers.data ?? []}
                  selected={selected}
                  onChange={(ids) => setSelected(new Set(ids))}
                  disabled={locked}
                  invalid={!!errors.targets}
                />
                <FieldError>{errors.targets}</FieldError>
                {/* Posts to X are metered: say so before publishing when this period's are used up. */}
                {!locked && xOut && picked.some((c) => c.provider === 'x') && (
                  <Callout tone="danger" size="xs" data-testid="x-posts-exhausted">
                    {xOut}{' '}
                    <Link href="/settings/billing" className="font-medium underline underline-offset-2">
                      Billing
                    </Link>
                  </Callout>
                )}
              </div>

              <div className="grid gap-2">
                <Label htmlFor="post-tags">Tags</Label>
                <MultiSelect
                  id="post-tags"
                  label="Tags"
                  placeholder="Add tags to find and filter posts"
                  className="w-full"
                  value={tags}
                  onChange={setTags}
                  disabled={locked}
                  options={(knownTags.data ?? []).map((t) => ({ value: t.name, label: `#${t.name}` }))}
                  creatable={{ normalize: normalizeTag }}
                />
              </div>

              {!post && (
                <div className="grid gap-2">
                  <Label htmlFor="post-collection" required>
                    Collection
                  </Label>
                  <Select value={collection} onValueChange={setCollection}>
                    <SelectTrigger
                      id="post-collection"
                      className="w-full sm:w-64 [&>span]:truncate"
                      aria-invalid={!!errors.collection}
                    >
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
                  <FieldError>{errors.collection}</FieldError>
                </div>
              )}

              {multi && (
                <div
                  role="tablist"
                  aria-label="Edit the text for"
                  className="flex flex-wrap items-center gap-1.5 border-b pb-3"
                >
                  <span className="mr-1 text-xs text-muted-foreground">Text for</span>
                  <button
                    type="button"
                    role="tab"
                    aria-selected={shownTab === 'all'}
                    onClick={() => setTab('all')}
                    className={cn(
                      'inline-flex h-8 items-center gap-1.5 rounded-full border px-3 text-sm outline-none focus-visible:ring-2 focus-visible:ring-ring pointer-coarse:h-10',
                      shownTab === 'all' ? 'border-primary bg-primary/10 font-medium' : 'hover:bg-muted',
                    )}
                  >
                    <Globe className="size-4" /> All channels
                  </button>
                  {picked.map((ch) => (
                    <button
                      key={ch.id}
                      type="button"
                      role="tab"
                      aria-selected={shownTab === ch.id}
                      aria-label={`Text for ${ch.name}`}
                      title={`Text for ${ch.name}`}
                      onClick={() => setTab(ch.id)}
                      data-testid={`tab-channel-${ch.id}`}
                      className={cn(
                        'relative rounded-full p-0.5 ring-2 outline-none focus-visible:ring-ring pointer-coarse:p-1.5',
                        shownTab === ch.id ? 'ring-primary' : 'ring-transparent hover:ring-border',
                      )}
                    >
                      <ChannelAvatar channel={ch} className="size-7" />
                      {overrides[ch.id]?.content != null && (
                        <span className="absolute -top-0.5 -right-0.5 size-2.5 rounded-full bg-primary ring-2 ring-background" />
                      )}
                      {(() => {
                        const st = targetOf(ch.id)?.status

                        return st && st !== 'pending' ? (
                          <span
                            aria-hidden
                            className={cn(
                              'absolute -right-0.5 -bottom-0.5 size-2.5 rounded-full ring-2 ring-background',
                              st === 'published' && 'bg-emerald-500',
                              st === 'failed' && 'bg-destructive',
                              st === 'publishing' && 'bg-sky-500',
                            )}
                          />
                        ) : null
                      })()}
                    </button>
                  ))}
                </div>
              )}

              <div className="grid gap-1.5">
                <span id="post-content-label" className="flex items-center gap-2 text-sm font-medium">
                  {active ? (
                    `Text for ${active.name}`
                  ) : (
                    <>
                      Post <RequiredMark />
                    </>
                  )}
                </span>
                {active && post && targetOf(active.id) && <TargetStatus post={post} target={targetOf(active.id)!} />}
                {active && !locked && (
                  <p className="flex flex-wrap items-center gap-2 text-xs text-muted-foreground">
                    {ownText != null
                      ? `${active.name} gets its own text.`
                      : `${active.name} uses the text for all channels. Change it here to give it its own.`}
                    {ownText != null && (
                      <Button
                        type="button"
                        variant="ghost"
                        size="sm"
                        onClick={() => setOverride(active.id, { content: null })}
                      >
                        <RotateCcw /> Use the shared text
                      </Button>
                    )}
                  </p>
                )}
                {/* One editor per tab stays mounted (the hidden ones keep their own document, lists and marks). */}
                {[undefined, ...picked].map((ch) => {
                  const key = ch?.id ?? 'all'
                  const err = ch ? errors[`targets.${ch.id}.content`] : errors.content

                  return (
                    <div
                      key={key}
                      hidden={key !== shownTab}
                      className={cn('post-composer rounded-xl border bg-background', err && 'border-destructive')}
                    >
                      <PostEditor
                        id={ch ? `override-${ch.id}` : 'post-content'}
                        labelledBy="post-content-label"
                        value={ch ? (overrides[ch.id]?.content ?? content) : content}
                        onChange={(text) =>
                          ch ? setOverride(ch.id, { content: text === content ? null : text }) : setContent(text)
                        }
                        placeholder="What do you want to say?"
                        invalid={!!err}
                        required={!ch}
                        testId={ch ? `override-${ch.id}` : 'post-content'}
                        className="border-0 shadow-none"
                        editable={!locked}
                        below={
                          <MediaStrip
                            media={media}
                            editable={!locked}
                            onRemove={locked ? undefined : (id) => setMedia((ms) => ms.filter((m) => m.id !== id))}
                          />
                        }
                        start={locked ? undefined : <MediaButton onAdd={(m) => setMedia((ms) => [...ms, m])} />}
                        end={<Counter counts={countsFor(ch)} />}
                      />
                    </div>
                  )
                })}
                <LinksNotices counts={countsFor(active)} />
                <FieldError>{active ? errors[`targets.${active.id}.content`] : errors.content}</FieldError>
              </div>

              {/* Comments belong to the post as a whole: written on the shared tab, shown read only per channel. */}
              {!active && (
                <ThreadEditor
                  parts={thread}
                  onChange={setThread}
                  countsFor={(text) => picked.map((c) => countFor(c, text, providerOf(c)))}
                  errors={errors}
                  locked={locked}
                />
              )}
            </TabsContent>

            <TabsContent value="settings" forceMount className="grid content-start gap-3 data-[state=inactive]:hidden">
              <p className="text-sm text-muted-foreground">
                Each channel&apos;s own options on its platform. Defaults are fine for most posts.
              </p>
              {settingsChannels.map((ch) => {
                const fields = providerOf(ch)?.settings_fields ?? []
                const values = overrides[ch.id]?.settings ?? {}
                const summary = settingsSummary(fields, values, channels.data ?? [])
                const hasError = Object.keys(settingsErrors(ch.id)).length > 0
                // One channel opens by default; with several each folds, and a setting with an error always shows.
                const open = hasError || (openSettings[ch.id] ?? settingsChannels.length === 1)

                return (
                  <Collapsible
                    key={ch.id}
                    open={open}
                    onOpenChange={(o) => setOpenSettings((s) => ({ ...s, [ch.id]: o }))}
                    asChild
                  >
                    <section
                      aria-label={`${ch.name} settings`}
                      className="rounded-xl border"
                      data-testid={`settings-${ch.id}`}
                    >
                      <CollapsibleTrigger className="flex w-full min-w-0 items-center gap-2 rounded-xl px-3 py-2.5 text-left text-sm outline-none hover:bg-muted/40 focus-visible:ring-2 focus-visible:ring-ring">
                        <ProviderIcon id={ch.provider} className="size-4 shrink-0" />
                        <span className="shrink-0 font-medium">
                          {platformLabel(ch.provider, providerOf(ch)?.name)} advanced settings
                        </span>
                        {!open && summary && (
                          <span className="min-w-0 truncate text-xs text-muted-foreground">{summary}</span>
                        )}
                        <span className="ml-auto flex min-w-0 items-center gap-1.5 text-xs text-muted-foreground">
                          <ChannelAvatar channel={ch} className="size-5" />
                          <span className="truncate">{ch.name}</span>
                          <ChevronDown className={cn('size-4 shrink-0 transition-transform', open && 'rotate-180')} />
                        </span>
                      </CollapsibleTrigger>
                      <CollapsibleContent className="border-t p-3">
                        <ProviderFields
                          idPrefix={`settings-${ch.id}`}
                          fields={fields}
                          values={values}
                          onChange={(k, v) =>
                            setOverride(ch.id, { settings: { ...overrides[ch.id]?.settings, [k]: v } })
                          }
                          errors={settingsErrors(ch.id)}
                          channelOptions={(channels.data ?? [])
                            .filter((o) => o.provider === ch.provider && o.id !== ch.id)
                            .map((o) => ({ value: o.id, label: o.name, hint: o.handle ?? undefined }))}
                        />
                      </CollapsibleContent>
                    </section>
                  </Collapsible>
                )
              })}
            </TabsContent>

            <TabsContent value="delivery" forceMount className="grid content-start gap-3 data-[state=inactive]:hidden">
              {post && deliveryTargets.length > 0 && (
                <ul className="grid gap-2" aria-label="Delivery per channel">
                  {deliveryTargets.map((t) => (
                    <li key={t.channel_id} className="grid gap-1 rounded-xl border p-3">
                      <span className="text-sm font-medium">{t.channel_name}</span>
                      <TargetStatus post={post} target={t} />
                    </li>
                  ))}
                </ul>
              )}
            </TabsContent>

            {post && hasStats && (
              <TabsContent value="stats" className="grid content-start gap-3">
                <PostStats post={post} active={shownSection === 'stats'} />
              </TabsContent>
            )}
          </div>
        </div>
      </Tabs>
      <div className="flex flex-wrap items-center gap-2 border-t p-4 pb-[max(1rem,env(safe-area-inset-bottom))]">
        {locked ? (
          <p className="ml-auto text-sm text-muted-foreground">
            {post!.status === 'publishing'
              ? 'Going out now. The channels above show how it goes.'
              : 'This post went out and can no longer be edited. Duplicate it to post it again.'}
          </p>
        ) : (
          <div className="ml-auto flex flex-wrap items-center justify-end gap-2">
            {publishAt && (
              <span
                className={cn(
                  'inline-flex h-8 max-w-full items-center gap-1.5 rounded-full border pr-1 pl-3 text-sm tabular-nums',
                  inPast && 'border-destructive text-destructive',
                )}
                data-testid="publish-at-chip"
                title={formatShort(publishAt)}
              >
                <CalendarClock className="size-4 shrink-0 text-muted-foreground" />
                <span className="truncate">{chipLabel(publishAt)}</span>
                {repeatEvery && <Repeat2 className="size-4 shrink-0 text-muted-foreground" aria-label="Repeats" />}
                <Button
                  type="button"
                  variant="ghost"
                  size="icon-xs"
                  className="rounded-full"
                  aria-label="Clear schedule"
                  onClick={() => setPublishAt(null)}
                >
                  <X />
                </Button>
              </span>
            )}
            {live ? (
              <Button
                type="button"
                variant="outline"
                onClick={() => save.mutate('now')}
                disabled={busy || !canSend}
                data-testid="publish-now"
              >
                <Send /> Publish now
              </Button>
            ) : (
              <Button
                type="button"
                variant="outline"
                onClick={() => save.mutate('draft')}
                disabled={busy || !collection || !content.trim()}
              >
                Save draft
              </Button>
            )}
            <ButtonGroup aria-label="Publish">
              {live ? (
                <Button type="submit" disabled={busy || !canSend || inPast} data-testid="save-post">
                  {busy ? 'Saving...' : 'Save changes'}
                </Button>
              ) : publishAt ? (
                <Button type="submit" disabled={busy || !canSend || inPast} data-testid="schedule-post">
                  {busy ? 'Saving...' : 'Schedule'}
                </Button>
              ) : (
                <Button type="submit" disabled={busy || !canSend} data-testid="publish-now">
                  <Send /> {busy ? 'Publishing...' : 'Publish now'}
                </Button>
              )}
              <ScheduleMenu
                value={publishAt}
                repeat={repeatEvery}
                disabled={busy}
                onApply={(at, rep) => {
                  flushSync(() => {
                    setPublishAt(at)
                    setRepeatEvery(rep)
                  })

                  if (canSend) {
                    formRef.current?.requestSubmit()
                  }
                }}
              />
            </ButtonGroup>
          </div>
        )}
        {(errors.publish_at || inPast) && (
          <FieldError className="basis-full text-right">{errors.publish_at ?? 'Pick a time in the future'}</FieldError>
        )}
      </div>
    </form>
  )
}
