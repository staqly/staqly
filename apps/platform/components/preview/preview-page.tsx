'use client'

import { Logo } from '@staqly/brand/logo'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import {
  CalendarClock,
  CheckCircle2,
  ExternalLink,
  Link2Off,
  MessageSquarePlus,
  MessageSquareText,
  PenLine,
  Quote,
  X,
} from 'lucide-react'
import { useCallback, useEffect, useMemo, useState, useSyncExternalStore } from 'react'

import { ChannelAvatar } from '@/components/channels/channel-avatars'
import { FeedbackThreads } from '@/components/preview/feedback-threads'
import { Button } from '@/components/ui/button'
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from '@/components/ui/dialog'
import { Field, FieldError, FieldLabel } from '@/components/ui/field'
import { Input } from '@/components/ui/input'
import { Skeleton } from '@/components/ui/skeleton'
import { Textarea } from '@/components/ui/textarea'
import { anchorFromRange, type Mark, marksFor, segments, threadsOf } from '@/lib/annotations'
import {
  API_PREFIX,
  ApiError,
  api,
  type CommentAnchor,
  errorMessage,
  type PostComment,
  type PublicPreview,
} from '@/lib/api'
import { formatDateTime } from '@/lib/format'
import { safeHttpUrl } from '@/lib/safe'
import { TONES } from '@/lib/tones'
import { cn } from '@/lib/utils'

type Target = PublicPreview['targets'][number]
type Media = PublicPreview['media'][number]

const REVIEWER_KEY = 'staqly:reviewer'

function subscribeHash(onChange: () => void) {
  window.addEventListener('hashchange', onChange)

  return () => window.removeEventListener('hashchange', onChange)
}

const readToken = () => window.location.hash.replace(/^#/, '')

function mediaSrc(token: string, m: Media) {
  return `${API_PREFIX}/v1/previews/${encodeURIComponent(token)}/media/${encodeURIComponent(m.id)}`
}

function MediaGrid({ token, media }: { token: string; media: Media[] }) {
  if (!media.length) {
    return null
  }

  return (
    <div className={cn('mt-3 grid gap-1.5 overflow-hidden rounded-xl', media.length > 1 && 'grid-cols-2')}>
      {media.map((m) =>
        m.mime_type.startsWith('video/') ? (
          // oxlint-disable-next-line jsx-a11y/media-has-caption -- the author's own video; captions are not ours to add
          <video key={m.id} src={mediaSrc(token, m)} controls className="aspect-video w-full bg-black object-contain" />
        ) : m.mime_type.startsWith('image/') ? (
          <img key={m.id} src={mediaSrc(token, m)} alt="" className="aspect-square w-full bg-muted object-cover" />
        ) : null,
      )}
    </div>
  )
}

function statusLine(p: PublicPreview) {
  if (p.status === 'published' && p.published_at) {
    return { Icon: CheckCircle2, text: `Published ${formatDateTime(p.published_at)}`, tone: 'text-brand-strong' }
  }

  if (p.publish_at && (p.status === 'scheduled' || p.status === 'publishing')) {
    return {
      Icon: CalendarClock,
      text: `Scheduled for ${formatDateTime(p.publish_at)}`,
      tone: TONES.info.text,
    }
  }

  return { Icon: PenLine, text: 'Draft', tone: 'text-muted-foreground' }
}

const DRAFT = 'draft'

/** A text of the post with its commented passages marked; a click on a passage opens its comment. */
function AnnotatedText({
  text,
  channelId,
  part,
  marks,
  activeId,
  onOpenThread,
  className,
}: {
  text: string
  channelId: string
  part: number
  marks: Mark[]
  activeId: string | null
  onOpenThread: (id: string) => void
  className?: string
}) {
  return (
    <p data-annotate="" data-channel={channelId} data-part={part} className={className}>
      {segments(text, marks).map((seg) => {
        if (!seg.ids.length) {
          return seg.text
        }

        const threadIds = seg.ids.filter((id) => id !== DRAFT)
        const strong = seg.ids.includes(DRAFT) || seg.ids.includes(activeId ?? '') || seg.ids.length > 1

        return (
          <mark
            key={seg.start}
            data-threads={threadIds.join(' ')}
            aria-describedby={threadIds.map((id) => `thread-${id}`).join(' ') || undefined}
            className={cn(
              'rounded-xs text-inherit transition-colors outline-none focus-visible:ring-2 focus-visible:ring-ring',
              strong ? 'bg-brand/35' : 'bg-brand-soft',
              threadIds.length > 0 && 'cursor-pointer',
            )}
            {...(threadIds.length > 0 && {
              role: 'button',
              tabIndex: 0,
              onClick: () => onOpenThread(threadIds[0]!),
              onKeyDown: (e: React.KeyboardEvent) => {
                if (e.key === 'Enter' || e.key === ' ') {
                  e.preventDefault()
                  onOpenThread(threadIds[0]!)
                }
              },
            })}
          >
            {seg.text}
          </mark>
        )
      })}
    </p>
  )
}

/** One channel's version, drawn like a post in a feed; its texts can be selected and commented on. */
function TargetCard({
  token,
  target,
  preview,
  marksOf,
  activeId,
  onOpenThread,
}: {
  token: string
  target: Target
  preview: PublicPreview
  marksOf: (channelId: string, part: number) => Mark[]
  activeId: string | null
  onOpenThread: (id: string) => void
}) {
  const link = safeHttpUrl(target.external_url)
  const avatar = safeHttpUrl(target.avatar_url)

  return (
    <article
      className="rounded-2xl border bg-card p-4 shadow-xs sm:p-5"
      aria-label={`${target.provider_name}: ${target.channel_name}`}
    >
      <header className="flex items-center gap-3">
        {/* The same picture as in the dashboard: the platform's, else (a Slack workspace) its first letter. */}
        <ChannelAvatar
          channel={{ id: target.channel_id, provider: target.provider, name: target.channel_name, avatar_url: avatar }}
          className="size-10"
        />
        <div className="min-w-0 flex-1 leading-tight">
          <p className="truncate font-semibold">{target.channel_name}</p>
          <p className="truncate text-sm text-muted-foreground">
            {target.handle || target.provider_name}
            {target.handle && <span> · {target.provider_name}</span>}
          </p>
        </div>
        {link && (
          <Button asChild size="icon-sm" variant="ghost" aria-label={`Open on ${target.provider_name}`}>
            <a href={link} target="_blank" rel="noopener noreferrer">
              <ExternalLink />
            </a>
          </Button>
        )}
      </header>
      {target.content ? (
        <AnnotatedText
          text={target.content}
          channelId={target.channel_id}
          part={0}
          marks={marksOf(target.channel_id, 0)}
          activeId={activeId}
          onOpenThread={onOpenThread}
          className="mt-3 text-[15px] leading-relaxed wrap-anywhere whitespace-pre-wrap"
        />
      ) : (
        <p className="mt-3 text-[15px] text-muted-foreground italic">No text</p>
      )}
      {/* The tags as the platform gets them, after the text (kept apart so comments anchored in the text stay put). */}
      {target.hashtags && (
        <p className="mt-3 text-[15px] leading-relaxed wrap-anywhere text-sky-700 dark:text-sky-400">
          {target.hashtags}
        </p>
      )}
      <MediaGrid token={token} media={preview.media} />
      {preview.thread.length > 0 && (
        <ol className="mt-4 grid gap-3 border-l-2 border-border pl-4" aria-label="Comments under the post">
          {preview.thread.map((part, i) => (
            <li key={`${part.delay_seconds}:${part.content}`} className="text-sm">
              <p className="text-xs text-muted-foreground">
                Comment {i + 1}
                {part.delay_seconds > 0 && ` · ${Math.round(part.delay_seconds / 60)} min after the one before`}
              </p>
              <AnnotatedText
                text={part.content}
                channelId={target.channel_id}
                part={i + 1}
                marks={marksOf(target.channel_id, i + 1)}
                activeId={activeId}
                onOpenThread={onOpenThread}
                className="mt-1 leading-relaxed wrap-anywhere whitespace-pre-wrap"
              />
              <MediaGrid token={token} media={part.media} />
            </li>
          ))}
        </ol>
      )}
    </article>
  )
}

interface Reviewer {
  name: string
  email: string
}

function readReviewer(): Reviewer {
  try {
    const saved = JSON.parse(localStorage.getItem(REVIEWER_KEY) ?? '{}') as Partial<Reviewer>

    return { name: saved.name ?? '', email: saved.email ?? '' }
  } catch {
    return { name: '', email: '' }
  }
}

function saveReviewer(r: Reviewer) {
  try {
    localStorage.setItem(REVIEWER_KEY, JSON.stringify(r))
  } catch {
    // Not remembered; asked again next time.
  }
}

/** Who is commenting: asked once on this device, before the first comment, reply or resolve. */
function ReviewerDialog({
  open,
  initial,
  onOpenChange,
  onSave,
}: {
  open: boolean
  initial: Reviewer
  onOpenChange: (o: boolean) => void
  onSave: (r: Reviewer) => void
}) {
  const [name, setName] = useState(initial.name)
  const [email, setEmail] = useState(initial.email)
  const [errors, setErrors] = useState<Record<string, string>>({})

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent className="sm:max-w-md">
        <DialogHeader>
          <DialogTitle>Who is commenting?</DialogTitle>
          <DialogDescription>
            Your name shows next to your comments. It is remembered in this browser.
          </DialogDescription>
        </DialogHeader>
        <form
          noValidate
          className="grid gap-4"
          onSubmit={(e) => {
            e.preventDefault()
            const next: Record<string, string> = {}

            if (!name.trim()) {
              next.name = 'Say who you are, such as Ana from the client team.'
            }

            if (email.trim() && !/^[^\s@]+@[^\s@]+\.[^\s@]+$/.test(email.trim())) {
              next.email = 'Not an email address.'
            }

            setErrors(next)

            if (!Object.keys(next).length) {
              onSave({ name: name.trim(), email: email.trim() })
            }
          }}
        >
          <Field data-invalid={!!errors.name}>
            <FieldLabel htmlFor="preview-name" required>
              Your name
            </FieldLabel>
            <Input
              id="preview-name"
              maxLength={80}
              autoComplete="name"
              value={name}
              aria-required
              aria-invalid={!!errors.name}
              onChange={(e) => setName(e.target.value)}
            />
            <FieldError>{errors.name}</FieldError>
          </Field>
          <Field data-invalid={!!errors.email}>
            <FieldLabel htmlFor="preview-email">Email (optional)</FieldLabel>
            <Input
              id="preview-email"
              type="email"
              autoComplete="email"
              value={email}
              aria-invalid={!!errors.email}
              onChange={(e) => setEmail(e.target.value)}
            />
            <FieldError>{errors.email}</FieldError>
            <p className="text-xs text-muted-foreground">Only the team sees your email.</p>
          </Field>
          <DialogFooter>
            <Button type="button" variant="ghost" onClick={() => onOpenChange(false)}>
              Cancel
            </Button>
            <Button type="submit">Continue</Button>
          </DialogFooter>
        </form>
      </DialogContent>
    </Dialog>
  )
}

function CommentForm({
  draft,
  reviewer,
  pending,
  error,
  clearDraft,
  onSend,
  onChangeReviewer,
}: {
  draft: CommentAnchor | null
  reviewer: Reviewer
  pending: boolean
  error: string
  clearDraft: () => void
  onSend: (body: string) => Promise<unknown>
  onChangeReviewer: () => void
}) {
  const [body, setBody] = useState('')
  const [bodyError, setBodyError] = useState('')

  return (
    <form
      noValidate
      className="grid gap-3"
      onSubmit={(e) => {
        e.preventDefault()

        if (!body.trim()) {
          setBodyError('Write a comment.')

          return
        }

        setBodyError('')
        onSend(body.trim()).then(
          () => setBody(''),
          () => undefined,
        )
      }}
    >
      {draft && (
        <div className="flex items-start gap-2 rounded-lg border-l-2 border-brand bg-muted/50 px-2.5 py-1.5 text-xs">
          <Quote className="mt-0.5 size-3.5 shrink-0 text-muted-foreground" />
          <span className="line-clamp-3 flex-1 italic">{draft.quote}</span>
          <Button
            type="button"
            size="icon-xs"
            variant="ghost"
            aria-label="Comment on the whole post"
            onClick={clearDraft}
          >
            <X />
          </Button>
        </div>
      )}
      <Field data-invalid={!!bodyError}>
        <FieldLabel htmlFor="preview-comment" required>
          {draft ? 'Comment on the selection' : 'Comment'}
        </FieldLabel>
        <Textarea
          id="preview-comment"
          rows={3}
          maxLength={2000}
          value={body}
          aria-required
          aria-invalid={!!bodyError}
          placeholder="What would you change?"
          onChange={(e) => setBody(e.target.value)}
        />
        <FieldError>{bodyError}</FieldError>
      </Field>
      {error && (
        <p className="text-sm text-destructive" role="alert">
          {error}
        </p>
      )}
      <div className="flex flex-wrap items-center justify-between gap-3">
        {reviewer.name ? (
          <p className="min-w-0 text-xs text-muted-foreground">
            As <span className="font-medium text-foreground">{reviewer.name}</span> ·{' '}
            <button
              type="button"
              className="underline underline-offset-2 hover:text-foreground pointer-coarse:-my-3 pointer-coarse:py-3"
              onClick={onChangeReviewer}
            >
              Change
            </button>
          </p>
        ) : (
          <span />
        )}
        <Button type="submit" disabled={pending}>
          {pending ? 'Sending...' : 'Post comment'}
        </Button>
      </div>
    </form>
  )
}

/** Where a selection would go: the text it's in and the floating button's place. */
interface Pending {
  anchor: CommentAnchor
  top: number
  left: number
}

/** The commentable text a node is in. */
const host = (n: Node | null) => (n instanceof Element ? n : n?.parentElement)?.closest<HTMLElement>('[data-annotate]')

/** The selection inside one commentable text of the page, or null. */
function readSelection(): Pending | null {
  const sel = window.getSelection()

  if (!sel || sel.rangeCount === 0 || sel.isCollapsed) {
    return null
  }

  const range = sel.getRangeAt(0)
  const el = host(range.startContainer)

  if (!el || el !== host(range.endContainer)) {
    return null
  }

  const a = anchorFromRange(el, range)

  if (!a) {
    return null
  }

  // Where the button goes (older engines have no Range rectangles: then by the text itself).
  const rect = 'getBoundingClientRect' in range ? range.getBoundingClientRect() : el.getBoundingClientRect()

  return {
    anchor: {
      channel_id: el.dataset.channel ?? '',
      part: Number(el.dataset.part ?? 0),
      quote: a.quote,
      anchor_start: a.start,
      anchor_end: a.end,
    },
    top: rect.bottom + 6,
    left: Math.max(8, Math.min(rect.left, window.innerWidth - 220)),
  }
}

/** The read-only page a preview link opens: the post per channel, its status, and the comment threads. */
export function PreviewPage() {
  // null until the browser reads the address (the static HTML and hydration): the skeleton, not "not available".
  const hash = useSyncExternalStore(subscribeHash, readToken, () => null)
  const token = hash ?? ''
  const qc = useQueryClient()
  const preview = useQuery({
    queryKey: ['preview', token],
    queryFn: () => api.openPreview(token),
    enabled: !!token,
    retry: false,
    // Several people comment at once: their threads show up within half a minute.
    refetchInterval: 30_000,
  })
  const p = preview.data
  const [draft, setDraft] = useState<CommentAnchor | null>(null)
  const [pending, setPending] = useState<Pending | null>(null)
  const [activeId, setActiveId] = useState<string | null>(null)
  const [reviewer, setReviewer] = useState<Reviewer>({ name: '', email: '' })
  const [asking, setAsking] = useState<null | ((r: Reviewer) => void)>(null)
  const [error, setError] = useState('')

  useEffect(() => {
    // oxlint-disable-next-line react-hooks-js/set-state-in-effect -- one read of browser storage after mount
    setReviewer(readReviewer())
  }, [])

  // A selection inside the post offers "Comment" next to it (mouse, touch and keyboard selections alike). The button
  // is placed on the screen, so it is placed again when the page scrolls or resizes under the selection.
  useEffect(() => {
    let frame = 0
    const onChange = () => {
      cancelAnimationFrame(frame)
      frame = requestAnimationFrame(() => setPending(readSelection()))
    }
    const move = { passive: true, capture: true } as const

    document.addEventListener('selectionchange', onChange)
    window.addEventListener('scroll', onChange, move)
    window.addEventListener('resize', onChange)

    return () => {
      cancelAnimationFrame(frame)
      document.removeEventListener('selectionchange', onChange)
      window.removeEventListener('scroll', onChange, move)
      window.removeEventListener('resize', onChange)
    }
  }, [])

  const startComment = useCallback((anchor: CommentAnchor) => {
    setDraft(anchor)
    setPending(null)
    window.getSelection()?.removeAllRanges()
    requestAnimationFrame(() => document.getElementById('preview-comment')?.focus())
  }, [])

  // Ctrl+Alt+M (Cmd+Option+M on a Mac): comment on the selected words.
  useEffect(() => {
    const onKey = (e: KeyboardEvent) => {
      if (e.altKey && (e.ctrlKey || e.metaKey) && e.code === 'KeyM') {
        const sel = readSelection()

        if (sel) {
          e.preventDefault()
          startComment(sel.anchor)
        }
      }
    }

    document.addEventListener('keydown', onKey)

    return () => document.removeEventListener('keydown', onKey)
  }, [startComment])

  const threads = useMemo(() => threadsOf(p?.comments ?? []), [p?.comments])
  const marksOf = useCallback(
    (channelId: string, part: number): Mark[] => {
      const marks = marksFor(threads, channelId, part)

      if (draft && draft.channel_id === channelId && draft.part === part) {
        marks.push({ id: DRAFT, start: draft.anchor_start, end: draft.anchor_end })
      }

      return marks
    },
    [threads, draft],
  )
  const openThread = useCallback((id: string) => {
    setActiveId(id)
    const el = document.getElementById(`thread-${id}`)

    el?.scrollIntoView({ block: 'nearest', behavior: 'smooth' })
    el?.focus({ preventScroll: true })
  }, [])

  // Every write needs the reviewer's name: asked once, then remembered.
  const asReviewer = (run: (r: Reviewer) => void) => {
    if (reviewer.name) {
      run(reviewer)
    } else {
      setAsking(() => run)
    }
  }
  const refresh = () => qc.invalidateQueries({ queryKey: ['preview', token] })
  const fail = (e: unknown) =>
    setError(e instanceof ApiError && e.fields ? Object.values(e.fields).join(' ') : errorMessage(e))
  const send = useMutation({
    mutationFn: (v: { r: Reviewer; body: string; parentId?: string; anchor?: CommentAnchor | null }) =>
      api.addPreviewComment(token, {
        name: v.r.name,
        email: v.r.email || undefined,
        body: v.body,
        ...(v.parentId ? { parent_id: v.parentId } : (v.anchor ?? {})),
      }),
    onSuccess: (_, v) => {
      setError('')

      if (!v.parentId) {
        setDraft(null)
      }

      void refresh()
    },
    onError: fail,
  })
  const resolve = useMutation({
    mutationFn: (v: { r: Reviewer; id: string; resolved: boolean }) =>
      api.setPreviewCommentResolved(token, v.id, v.r.name, v.resolved),
    onSuccess: () => {
      setError('')
      void refresh()
    },
    onError: fail,
  })
  const withReviewer = (body: string, parentId?: string) =>
    new Promise<unknown>((resolveP, reject) =>
      asReviewer((r) =>
        send.mutateAsync({ r, body, parentId, anchor: parentId ? null : draft }).then(resolveP, reject),
      ),
    )
  const placeOf = (root: PostComment) => {
    const channel = p?.targets.find((t) => t.channel_id === root.channel_id)?.channel_name
    const where = root.part > 0 ? `In comment ${root.part}` : 'In the post'

    return channel ? `${where} on ${channel}` : where
  }

  return (
    <div className="min-h-svh bg-background text-foreground">
      <header className="border-b bg-card/60 backdrop-blur">
        <div className="mx-auto flex w-full max-w-6xl flex-wrap items-center gap-x-4 gap-y-2 px-4 py-3 sm:px-6">
          <Logo className="text-lg" markClassName="size-7" />
          <span className="text-sm text-muted-foreground">
            Post preview{p?.workspace ? ` from ${p.workspace}` : ''}
          </span>
          {p &&
            (() => {
              const s = statusLine(p)

              return (
                <span className={cn('ml-auto inline-flex items-center gap-1.5 text-sm font-medium', s.tone)}>
                  <s.Icon className="size-4" /> {s.text}
                </span>
              )
            })()}
        </div>
      </header>

      <main className="mx-auto w-full max-w-6xl px-4 py-6 sm:px-6 sm:py-8">
        {/* A failed refresh keeps the post and the comment being typed; only a link that never opened is gone. */}
        {hash === '' || (preview.error && !p) ? (
          <div className="mx-auto grid max-w-md justify-items-center gap-3 rounded-2xl border bg-card p-8 text-center">
            <Link2Off className="size-8 text-muted-foreground" />
            <h1 className="text-lg font-semibold">This preview is not available</h1>
            <p className="text-sm text-muted-foreground">
              The link may have expired or been revoked. Ask whoever shared it for a new one.
            </p>
          </div>
        ) : preview.isLoading || !p ? (
          <div className="grid gap-4 lg:grid-cols-[minmax(0,1fr)_22rem]">
            <Skeleton className="h-64 rounded-2xl" />
            <Skeleton className="h-64 rounded-2xl" />
          </div>
        ) : (
          <div className="grid items-start gap-6 lg:grid-cols-[minmax(0,1fr)_24rem]">
            <section className="grid gap-4" aria-labelledby="preview-heading">
              <h1 id="preview-heading" className="sr-only">
                The post on each channel
              </h1>
              {p.targets.length === 0 ? (
                <article className="rounded-2xl border bg-card p-5">
                  <AnnotatedText
                    text={p.content}
                    channelId=""
                    part={0}
                    marks={marksOf('', 0)}
                    activeId={activeId}
                    onOpenThread={openThread}
                    className="leading-relaxed wrap-anywhere whitespace-pre-wrap"
                  />
                  <MediaGrid token={token} media={p.media} />
                </article>
              ) : (
                p.targets.map((t) => (
                  <TargetCard
                    key={t.channel_id}
                    token={token}
                    target={t}
                    preview={p}
                    marksOf={marksOf}
                    activeId={activeId}
                    onOpenThread={openThread}
                  />
                ))
              )}
              {p.expires_at && (
                <p className="text-xs text-muted-foreground">This link works until {formatDateTime(p.expires_at)}.</p>
              )}
            </section>

            <aside
              className="grid gap-4 rounded-2xl border bg-card p-4 sm:p-5 lg:sticky lg:top-6 lg:max-h-[calc(100svh-3rem)] lg:overflow-y-auto"
              aria-labelledby="comments-heading"
            >
              <div>
                <h2 id="comments-heading" className="flex items-center gap-2 font-semibold">
                  <MessageSquareText className="size-4 text-brand" /> Comments
                </h2>
                <p className="mt-1 text-xs text-muted-foreground">
                  Select words in the post and choose Comment (or press Ctrl+Alt+M), or add a general comment.
                </p>
              </div>
              <CommentForm
                draft={draft}
                reviewer={reviewer}
                pending={send.isPending}
                error={error}
                clearDraft={() => setDraft(null)}
                onSend={(body) => withReviewer(body)}
                onChangeReviewer={() => setAsking(() => () => undefined)}
              />
              {threads.length > 0 && (
                <div className="border-t pt-4">
                  <FeedbackThreads
                    threads={threads}
                    activeId={activeId}
                    onActivate={setActiveId}
                    placeOf={placeOf}
                    onReply={(id, body) => withReviewer(body, id)}
                    onResolve={(id, resolved) => asReviewer((r) => resolve.mutate({ r, id, resolved }))}
                    pending={send.isPending || resolve.isPending}
                  />
                </div>
              )}
            </aside>
          </div>
        )}
      </main>
      {pending && (
        <div className="fixed z-50" style={{ top: pending.top, left: pending.left }}>
          <Button
            type="button"
            size="sm"
            className="shadow-md"
            // Keep the selection: the button must not take it away on press.
            onMouseDown={(e) => e.preventDefault()}
            onClick={() => startComment(pending.anchor)}
            data-testid="comment-selection"
          >
            <MessageSquarePlus /> Comment
          </Button>
        </div>
      )}
      {asking && (
        <ReviewerDialog
          open
          initial={reviewer}
          onOpenChange={(o) => !o && setAsking(null)}
          onSave={(r) => {
            saveReviewer(r)
            setReviewer(r)
            const run = asking

            setAsking(null)
            run(r)
          }}
        />
      )}
      <footer className="pb-8 text-center text-xs text-muted-foreground">Shared with Staqly</footer>
    </div>
  )
}
