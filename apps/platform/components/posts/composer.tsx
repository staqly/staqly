'use client'

import { useMutation, useQueryClient } from '@tanstack/react-query'
import {
  AlertCircle,
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
  Settings2,
  X,
} from 'lucide-react'
import Link from 'next/link'
import { useMemo, useRef, useState } from 'react'

import { ChannelAvatar } from '@/components/channels/channel-avatars'
import { ProviderFields } from '@/components/channels/provider-fields'
import { DateTimePicker } from '@/components/common/date-time-picker'
import { MultiSelect } from '@/components/common/multi-select'
import { PostStatusBadge, TargetStatusBadge } from '@/components/common/status-badge'
import { Button } from '@/components/ui/button'
import { Collapsible, CollapsibleContent, CollapsibleTrigger } from '@/components/ui/collapsible'
import { FieldError } from '@/components/ui/field'
import { Label, RequiredMark } from '@/components/ui/label'
import { Popover, PopoverContent, PopoverTrigger } from '@/components/ui/popover'
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from '@/components/ui/select'
import { Sheet, SheetContent, SheetDescription, SheetHeader, SheetTitle } from '@/components/ui/sheet'
import { useNow } from '@/hooks/use-now'
import {
  api,
  mediaThumbUrl,
  type Channel,
  type Media,
  type Post,
  type PostInput,
  type PostTarget,
  type Provider,
} from '@/lib/api'
import { apiFieldErrors } from '@/lib/form-errors'
import { formatDateTime, timeAgo } from '@/lib/format'
import { namedToast, toastApiError } from '@/lib/notify'
import { charCount, isLocked, postTitle } from '@/lib/posts'
import {
  invalidateAfterPostChange,
  refetchPostListsFromStart,
  syncPostInCache,
  useChannels,
  useCollections,
  useProviders,
  useTags,
} from '@/lib/queries'
import { safeHttpUrl } from '@/lib/safe'
import { formatSchedule } from '@/lib/schedule'
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
  n: number
  max: number
}

/**
 * The counter in the editor's corner: the tightest channel's count with a tick (or a warning past a limit);
 * the popover lists every picked channel.
 */
function Counter({ counts }: { counts: Count[] }) {
  if (!counts.length) {
    return null
  }

  const limited = counts.filter((c) => c.max > 0)
  const tight = limited.toSorted((a, b) => a.max - a.n - (b.max - b.n))[0]
  const over = limited.some((c) => c.n > c.max)
  const Icon = over ? AlertCircle : CheckCircle2

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
          {counts.map(({ channel, n, max }) => (
            <li key={channel.id} className="flex items-center gap-2 px-1 py-0.5">
              <ChannelAvatar channel={channel} className="size-5" />
              <span className="min-w-0 flex-1 truncate">{channel.name}</span>
              <span className={cn('tabular-nums', max > 0 && n > max ? 'text-destructive' : 'text-muted-foreground')}>
                {max > 0 ? `${n}/${max}` : n}
              </span>
            </li>
          ))}
        </ul>
      </PopoverContent>
    </Popover>
  )
}

/** Every connected channel as an avatar; picked ones are in colour with a ring, the rest greyed out. */
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
      options={channels.map((c) => {
        const off = c.status !== 'connected'

        return {
          value: c.id,
          label: c.name,
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

/** Uploaded pictures and files as thumbnails with a remove button. */
function MediaStrip({ media, onRemove }: { media: Media[]; onRemove?: (id: string) => void }) {
  if (!media.length) {
    return null
  }

  return (
    <div className="flex flex-wrap gap-2 px-3 pb-2">
      {media.map((m) => (
        <span key={m.id} className="relative size-16 overflow-hidden rounded-lg border bg-muted">
          {m.mime_type.startsWith('image/') ? (
            // eslint-disable-next-line @next/next/no-img-element
            <img src={mediaThumbUrl(m.id, 160)} alt={m.original_name} className="size-full object-cover" />
          ) : (
            <span className="flex size-full items-center justify-center p-1 text-center text-[10px] wrap-anywhere text-muted-foreground">
              {m.original_name}
            </span>
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

const DELAYS: [number, string][] = [
  [0, 'Right after'],
  [60, '1 min later'],
  [120, '2 min later'],
  [300, '5 min later'],
  [600, '10 min later'],
  [900, '15 min later'],
  [1800, '30 min later'],
  [3600, '1 hour later'],
  [7200, '2 hours later'],
]

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
            {/* The thread line from the post (or the comment above) down to this comment. */}
            <span aria-hidden className="absolute top-0 bottom-0 left-2 w-px bg-border" />
            <span aria-hidden className="absolute top-5 left-2 h-px w-3 bg-border" />
            <div className="flex flex-wrap items-center gap-2">
              <span id={`thread-${part.uid}-label`} className="text-sm font-medium">
                Comment {i + 1}
              </span>
              {!locked && (
                <>
                  <Select value={String(part.delay)} onValueChange={(v) => set(i, { delay: Number(v) })}>
                    <SelectTrigger aria-label={`When comment ${i + 1} goes out`} className="h-7 w-36 text-xs">
                      <Clock3 className="text-muted-foreground" />
                      <SelectValue />
                    </SelectTrigger>
                    <SelectContent>
                      {DELAYS.map(([v, label]) => (
                        <SelectItem key={v} value={String(v)}>
                          {label}
                        </SelectItem>
                      ))}
                    </SelectContent>
                  </Select>
                  <span className="ml-auto flex items-center">
                    <Button
                      type="button"
                      size="icon"
                      variant="ghost"
                      aria-label={`Move comment ${i + 1} up`}
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
  onSaved?: (post: Post) => void
}) {
  return (
    <Sheet open={open} onOpenChange={onOpenChange}>
      <SheetContent className="gap-0 data-[side=right]:w-full data-[side=right]:sm:max-w-2xl data-[side=right]:lg:max-w-5xl">
        {open && (
          <ComposerForm
            collectionId={collectionId}
            post={post}
            defaultPublishAt={defaultPublishAt}
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
  initialTab,
  headerActions,
  onDone,
}: {
  collectionId?: string
  post?: Post
  defaultPublishAt?: string
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
  const [media, setMedia] = useState<Media[]>(post?.media_items ?? [])
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

      if (mode === 'now') {
        saved = await api.publishPost(saved.id)
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
      namedToast(
        postTitle(saved, 40),
        mode === 'now'
          ? 'is being published'
          : mode === 'schedule'
            ? 'is scheduled'
            : post
              ? 'was saved'
              : 'was saved as a draft',
      )
      onDone(saved)
    },
    onError: (e) => {
      const { errors: fields, complete } = apiFieldErrors<string>(e, (k) => k)

      setErrors(fields)

      if (!complete) {
        toastApiError(e)
      }
    },
  })

  const [tab, setTab] = useState<string>(initialTab ?? 'all')
  const locked = !!post && isLocked(post)
  const targetOf = (id: string) => post?.targets?.find((t) => t.channel_id === id)
  // Per-channel text only makes sense with two channels or more; with one, the shared text is that channel's.
  const multi = picked.length > 1
  const active = multi && tab !== 'all' ? picked.find((c) => c.id === tab) : undefined
  const shownTab = active?.id ?? 'all'
  const activeProvider = active ? providerOf(active) : undefined
  const ownText = active ? (overrides[active.id]?.content ?? null) : null
  const countsFor = (ch?: Channel): Count[] =>
    (ch ? [ch] : picked).map((c) => ({
      channel: c,
      n: charCount(textFor(c)),
      max: providerOf(c)?.limits?.max_chars ?? 0,
    }))
  const settingsErrors = (id: string) =>
    Object.fromEntries(
      Object.entries(errors)
        .filter(([k]) => k.startsWith(`targets.${id}.settings.`))
        .map(([k, v]) => [k.slice(`targets.${id}.settings.`.length), v]),
    )
  const now = useNow()
  const canSend = picked.length > 0 && content.trim().length > 0 && !!collection
  const busy = save.isPending
  const inPast = publishAt !== null && publishAt.getTime() <= now

  return (
    // Like every drawer: the header and the footer stay put, the fields scroll between them.
    <form
      onSubmit={(e) => {
        e.preventDefault()
        save.mutate(publishAt ? 'schedule' : 'now')
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
                      {post.status === 'published' ? 'Published' : 'Goes out'} {formatSchedule(post.publish_at)}
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
      <div className="grid min-h-0 flex-1">
        <div className="grid min-h-0 content-start gap-4 overflow-y-auto overscroll-contain p-4">
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
          </div>

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
                        onRemove={locked ? undefined : (id) => setMedia((ms) => ms.filter((m) => m.id !== id))}
                      />
                    }
                    start={locked ? undefined : <MediaButton onAdd={(m) => setMedia((ms) => [...ms, m])} />}
                    end={<Counter counts={countsFor(ch)} />}
                  />
                </div>
              )
            })}
            <FieldError>{active ? errors[`targets.${active.id}.content`] : errors.content}</FieldError>
          </div>

          {/* Comments belong to the post as a whole: written on the shared tab, shown read only per channel. */}
          {!active && (
            <ThreadEditor
              parts={thread}
              onChange={setThread}
              countsFor={(text) =>
                picked.map((c) => ({ channel: c, n: charCount(text), max: providerOf(c)?.limits?.max_chars ?? 0 }))
              }
              errors={errors}
              locked={locked}
            />
          )}

          {/* A saved post: how each channel went, under the text. */}
          {post && !active && (post.targets?.length ?? 0) > 0 && (
            <ul className="grid gap-2" aria-label="Channels">
              {post.targets!.map((t) => (
                <li key={t.channel_id} className="grid gap-1 rounded-xl border p-3">
                  <span className="text-sm font-medium">{t.channel_name}</span>
                  <TargetStatus post={post} target={t} />
                </li>
              ))}
            </ul>
          )}

          {active && activeProvider?.settings_fields?.length ? (
            <Collapsible className="rounded-xl border">
              <CollapsibleTrigger className="group flex w-full items-center gap-2 px-3 py-2.5 text-sm font-medium outline-none focus-visible:ring-2 focus-visible:ring-ring">
                <Settings2 className="size-4" /> {activeProvider.name} settings
                <ChevronDown className="ml-auto size-4 transition-transform group-data-[state=open]:rotate-180" />
              </CollapsibleTrigger>
              <CollapsibleContent className="border-t p-3">
                <ProviderFields
                  idPrefix={`settings-${active.id}`}
                  fields={activeProvider.settings_fields}
                  values={overrides[active.id]?.settings ?? {}}
                  onChange={(k, v) =>
                    setOverride(active.id, { settings: { ...overrides[active.id]?.settings, [k]: v } })
                  }
                  errors={settingsErrors(active.id)}
                />
              </CollapsibleContent>
            </Collapsible>
          ) : null}
        </div>
      </div>
      <div className="flex flex-wrap items-center gap-2 border-t p-4 pb-[max(1rem,env(safe-area-inset-bottom))]">
        {/* Left: where and when it goes, and its tags. Right: the actions. */}
        <div className="flex min-w-0 flex-wrap items-center gap-2">
          {!post && (
            <div className="flex w-full min-w-0 items-center gap-2 sm:w-auto">
              <Label htmlFor="post-collection" className="shrink-0">
                Collection
              </Label>
              <Select value={collection} onValueChange={setCollection}>
                <SelectTrigger
                  id="post-collection"
                  className="w-full min-w-0 sm:w-36 [&>span]:truncate"
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
            </div>
          )}
          {!locked && (
            <>
              <DateTimePicker
                id="post-publish-at"
                value={publishAt}
                onChange={setPublishAt}
                allowClear
                invalid={!!errors.publish_at || inPast}
              />
              <Select value={repeatEvery || 'never'} onValueChange={(v) => setRepeatEvery(v === 'never' ? '' : v)}>
                <SelectTrigger aria-label="Repeat" className="w-36" data-testid="post-repeat">
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
              <MultiSelect
                label="Tags"
                placeholder="Add tags"
                className="w-32"
                value={tags}
                onChange={setTags}
                options={(knownTags.data ?? []).map((t) => ({ value: t.tag, label: `#${t.tag}` }))}
                creatable={{ normalize: (v) => v.trim().replace(/^#/, '').toLowerCase().replace(/\s+/g, '-') }}
              />
            </>
          )}
        </div>
        {locked ? (
          <p className="ml-auto text-sm text-muted-foreground">
            {post!.status === 'publishing'
              ? 'Going out now. The channels above show how it goes.'
              : 'This post went out and can no longer be edited. Duplicate it to post it again.'}
          </p>
        ) : (
          <div className="ml-auto flex flex-wrap items-center justify-end gap-2">
            <Button
              type="button"
              variant="outline"
              onClick={() => save.mutate('draft')}
              disabled={busy || !collection || !content.trim()}
            >
              {post ? 'Save changes' : 'Save draft'}
            </Button>
            {publishAt ? (
              <Button type="submit" disabled={busy || !canSend || inPast} data-testid="schedule-post">
                {busy ? 'Saving...' : post?.status === 'scheduled' ? 'Update' : 'Schedule'}
              </Button>
            ) : (
              <Button type="submit" disabled={busy || !canSend} data-testid="publish-now">
                <Send /> {busy ? 'Publishing...' : 'Publish now'}
              </Button>
            )}
          </div>
        )}
        {(errors.collection || errors.publish_at || inPast) && (
          <FieldError className="basis-full text-right">
            {errors.collection ?? errors.publish_at ?? 'Pick a time in the future'}
          </FieldError>
        )}
      </div>
    </form>
  )
}
