'use client'

import { ProviderIcon } from '@staqly/brand/providers'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import {
  Bookmark,
  Clock,
  Copy,
  ExternalLink,
  Eye,
  Globe,
  Heart,
  Link2,
  type LucideIcon,
  MessageCircle,
  MousePointerClick,
  Quote,
  RefreshCw,
  Repeat2,
  Share2,
  Timer,
  Trash2,
  UserPlus,
} from 'lucide-react'
import { useRouter, useSearchParams } from 'next/navigation'
import { useMemo, useState } from 'react'

import { Callout } from '@/components/common/callout'
import { ConfirmDialog } from '@/components/common/confirm-dialog'
import { CopyButton } from '@/components/common/copy-button'
import { ErrorState } from '@/components/common/error-state'
import { NativeSelect } from '@/components/common/native-select'
import { FeedbackThreads } from '@/components/preview/feedback-threads'
import { Button } from '@/components/ui/button'
import { Dialog, DialogContent, DialogDescription, DialogHeader, DialogTitle } from '@/components/ui/dialog'
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuLabel,
  DropdownMenuTrigger,
} from '@/components/ui/dropdown-menu'
import { Skeleton } from '@/components/ui/skeleton'
import { threadsOf } from '@/lib/annotations'
import { api, type Post, type PostComment, type PreviewLink, type TargetMetrics } from '@/lib/api'
import { formatDateTime, timeAgo } from '@/lib/format'
import { namedToast, toastApiError } from '@/lib/notify'
import { postTitle, wentOut } from '@/lib/posts'
import { invalidateAfterPostChange, qk, refetchPostListsFromStart, removePostFromLists } from '@/lib/queries'
import { safeHttpUrl } from '@/lib/safe'

import { postHref } from './post-href'

const METRIC_LABELS: [Exclude<keyof NonNullable<TargetMetrics['metrics']>, 'reactions'>, string, LucideIcon][] = [
  ['impressions', 'Views', Eye],
  ['likes', 'Likes', Heart],
  ['replies', 'Replies', MessageCircle],
  ['reposts', 'Reposts', Repeat2],
  ['shares', 'Shares', Share2],
  ['quotes', 'Quotes', Quote],
  ['bookmarks', 'Bookmarks', Bookmark],
  ['link_clicks', 'Link clicks', MousePointerClick],
  ['watch_minutes', 'Watch time', Clock],
  ['avg_view_seconds', 'Average view', Timer],
  ['subscribers_gained', 'Subscribers gained', UserPlus],
]

/** Durations read as time: 1,284 min watched becomes 21 h 24 min, a 72 s average view 1:12. */
function metricValue(key: string, n: number) {
  if (key === 'watch_minutes') {
    return n >= 60 ? `${Math.floor(n / 60).toLocaleString('en-US')} h ${n % 60} min` : `${n} min`
  }

  if (key === 'avg_view_seconds') {
    return `${Math.floor(n / 60)}:${String(n % 60).padStart(2, '0')}`
  }

  return n.toLocaleString('en-US')
}

/** What a platform calls a number when it isn't the usual word (Slack has reactions, not likes). */
function metricLabel(provider: string, key: string, label: string) {
  return provider === 'slack' && key === 'likes' ? 'Reactions' : label
}

// Slack names its emoji; the common ones as themselves, the rest (custom ones too) by name.
const EMOJI: Record<string, string> = {
  '+1': '👍',
  thumbsup: '👍',
  '-1': '👎',
  thumbsdown: '👎',
  heart: '❤️',
  tada: '🎉',
  joy: '😂',
  fire: '🔥',
  eyes: '👀',
  rocket: '🚀',
  clap: '👏',
  raised_hands: '🙌',
  pray: '🙏',
  white_check_mark: '✅',
  heavy_check_mark: '✔️',
  '100': '💯',
  smile: '😄',
  slightly_smiling_face: '🙂',
  thinking_face: '🤔',
  star: '⭐',
}

// Not under ['posts']: those keys hold post lists, which the post cache helpers rewrite.
const commentsKey = (id: string) => ['post-feedback', id] as const
const NO_COMMENTS: PostComment[] = []

/** Open on the platform: one published channel opens straight away, several get a short menu. */
function OpenOnPlatform({ post }: { post: Post }) {
  // Nothing to open while the post still waits to go out (as in the list's menu).
  const waiting = post.status === 'scheduled' || post.status === 'publishing'
  const links = (waiting ? [] : (post.targets ?? [])).flatMap((t) => {
    const url = t.status === 'published' ? safeHttpUrl(t.external_url) : undefined

    return url ? [{ ...t, url }] : []
  })

  if (links.length === 0) {
    return null
  }

  if (links.length === 1) {
    return (
      <Button asChild size="icon-sm" variant="ghost" aria-label={`Open post on ${links[0]!.channel_name}`}>
        <a href={links[0]!.url} target="_blank" rel="noopener noreferrer" data-testid="post-open">
          <Globe />
        </a>
      </Button>
    )
  }

  return (
    <DropdownMenu>
      <DropdownMenuTrigger asChild>
        <Button type="button" size="icon-sm" variant="ghost" aria-label="Open post" data-testid="post-open">
          <Globe />
        </Button>
      </DropdownMenuTrigger>
      <DropdownMenuContent align="end">
        <DropdownMenuLabel>Open on</DropdownMenuLabel>
        {links.map((t) => (
          <DropdownMenuItem key={t.channel_id} asChild>
            <a href={t.url} target="_blank" rel="noopener noreferrer">
              <ProviderIcon id={t.provider} className="size-4" /> {t.channel_name}
              <ExternalLink className="ml-auto size-3.5 text-muted-foreground" />
            </a>
          </DropdownMenuItem>
        ))}
      </DropdownMenuContent>
    </DropdownMenu>
  )
}

/**
 * The post's numbers per channel (the sheet's Statistics tab); read from the platforms that report them, cached
 * for ten minutes. Fetches only while `active`.
 */
export function PostStats({ post, active }: { post: Post; active: boolean }) {
  const qc = useQueryClient()
  const key = ['post-metrics', post.id] as const
  const metrics = useQuery({ queryKey: key, queryFn: () => api.postMetrics(post.id), enabled: active })
  const refresh = useMutation({
    mutationFn: () => api.postMetrics(post.id, true),
    onSuccess: (res) => qc.setQueryData(key, res),
    onError: (e) => toastApiError(e),
  })
  const rows = metrics.data?.data ?? []

  return (
    <div className="grid gap-3" data-testid="post-stats">
      {metrics.isLoading ? (
        <Skeleton className="h-32" />
      ) : metrics.error ? (
        <ErrorState error={metrics.error} onRetry={() => metrics.refetch()} />
      ) : rows.length === 0 ? (
        <p className="text-sm text-muted-foreground">This post has no channels yet.</p>
      ) : (
        <ul className="grid gap-3">
          {rows.map((t) => (
            <li key={t.channel_id} className="rounded-xl border p-3">
              <div className="flex items-center gap-2 text-sm">
                <ProviderIcon id={t.provider} className="size-4" />
                <span className="min-w-0 truncate font-medium">{t.channel_name}</span>
                {t.updated_at && (
                  <span className="ml-auto shrink-0 text-xs text-muted-foreground" title={formatDateTime(t.updated_at)}>
                    Updated {timeAgo(t.updated_at)}
                  </span>
                )}
              </div>
              {!t.available ? (
                <p className="mt-2 text-sm text-muted-foreground">Not available for this platform.</p>
              ) : t.status !== 'published' && !t.metrics ? (
                <p className="mt-2 text-sm text-muted-foreground">Numbers appear once the post is published.</p>
              ) : t.metrics && METRIC_LABELS.some(([k]) => t.metrics?.[k] != null) ? (
                <dl className="mt-3 grid grid-cols-2 gap-2 sm:grid-cols-3">
                  {METRIC_LABELS.filter(([k]) => t.metrics?.[k] != null).map(([k, label, Icon]) => (
                    // The label with its icon on top, the number under it, large: each one reads at a glance.
                    <div key={k} className="grid gap-1 rounded-lg border bg-muted/30 px-3 py-2.5">
                      <dt className="flex items-center gap-1.5 text-xs text-muted-foreground">
                        <Icon aria-hidden className="size-3.5" />
                        {metricLabel(t.provider, k, label)}
                      </dt>
                      <dd className="text-2xl font-semibold tracking-tight tabular-nums">
                        {metricValue(k, t.metrics![k]!)}
                      </dd>
                    </div>
                  ))}
                  {(t.metrics.reactions?.length ?? 0) > 0 && (
                    <div className="col-span-full flex flex-wrap gap-1.5" aria-label="Reactions">
                      {t.metrics.reactions!.map((r) => (
                        <span
                          key={r.emoji}
                          className="inline-flex items-center gap-1 rounded-full border bg-muted/40 px-2 py-0.5 text-sm"
                          title={r.people?.length ? r.people.join(', ') : undefined}
                        >
                          <span aria-label={r.emoji}>{EMOJI[r.emoji] ?? `:${r.emoji}:`}</span>
                          <span className="tabular-nums">{r.count}</span>
                          {r.people && r.people.length > 0 && (
                            <span className="max-w-48 truncate text-xs text-muted-foreground">
                              {r.people.join(', ')}
                            </span>
                          )}
                        </span>
                      ))}
                    </div>
                  )}
                </dl>
              ) : (
                <p className="mt-2 text-sm text-muted-foreground">
                  {t.provider === 'custom' ? 'Your automation has not sent numbers yet.' : 'No numbers yet.'}
                </p>
              )}
              {t.error && (
                <Callout tone="danger" size="xs" className="mt-3" data-testid="metrics-error">
                  <p className="font-medium">Couldn&apos;t read the latest numbers</p>
                  <p className="mt-0.5 text-muted-foreground">
                    {t.error}
                    {t.metrics ? '. These are the last ones read.' : ''}
                  </p>
                </Callout>
              )}
            </li>
          ))}
        </ul>
      )}
      <div>
        <Button type="button" variant="outline" disabled={refresh.isPending} onClick={() => refresh.mutate()}>
          <RefreshCw className={refresh.isPending ? 'animate-spin' : undefined} /> Refresh
        </Button>
      </div>
    </div>
  )
}

const EXPIRY: [number, string][] = [
  [0, 'Never'],
  [1, 'In 1 day'],
  [7, 'In 7 days'],
  [30, 'In 30 days'],
]

/** Share a read-only preview: create, copy and revoke links; read what reviewers said. */
export function ShareDialog({
  post,
  open,
  onOpenChange,
}: {
  post: Post
  open: boolean
  onOpenChange: (o: boolean) => void
}) {
  const qc = useQueryClient()
  const linksKey = ['post-previews', post.id] as const
  const links = useQuery({ queryKey: linksKey, queryFn: () => api.previewLinks(post.id), enabled: open })
  const comments = useQuery({ queryKey: commentsKey(post.id), queryFn: () => api.postComments(post.id), enabled: open })
  const [expiry, setExpiry] = useState(7)
  const [created, setCreated] = useState<PreviewLink | null>(null)
  const create = useMutation({
    mutationFn: () => api.createPreviewLink(post.id, expiry),
    onSuccess: (l) => {
      setCreated(l)
      void qc.invalidateQueries({ queryKey: linksKey })
    },
    onError: (e) => toastApiError(e),
  })
  const revoke = useMutation({
    mutationFn: (id: string) => api.revokePreviewLink(post.id, id),
    onSuccess: (_, id) => {
      if (created?.id === id) {
        setCreated(null)
      }

      void qc.invalidateQueries({ queryKey: linksKey })
      namedToast('Preview link', 'was revoked')
    },
    onError: (e) => toastApiError(e),
  })
  const refreshComments = () => qc.invalidateQueries({ queryKey: commentsKey(post.id) })
  const removeComment = useMutation({
    mutationFn: (id: string) => api.deletePostComment(post.id, id),
    onSuccess: () => void refreshComments(),
    onError: (e) => toastApiError(e),
  })
  const reply = useMutation({
    mutationFn: (v: { id: string; body: string }) => api.addPostComment(post.id, { body: v.body, parent_id: v.id }),
    onSuccess: () => void refreshComments(),
    onError: (e) => toastApiError(e),
  })
  const resolve = useMutation({
    mutationFn: (v: { id: string; resolved: boolean }) => api.setPostCommentResolved(post.id, v.id, v.resolved),
    onSuccess: (_, v) => {
      void refreshComments()
      namedToast('Comment', v.resolved ? 'was resolved' : 'was reopened')
    },
    onError: (e) => toastApiError(e),
  })
  const [activeThread, setActiveThread] = useState<string | null>(null)
  const active = (links.data?.data ?? []).filter((l) => l.active)
  const feedback = comments.data?.data ?? NO_COMMENTS
  const threads = useMemo(() => threadsOf(feedback), [feedback])
  const openCount = threads.filter((t) => !t.root.resolved_at).length
  const placeOf = (root: PostComment) => {
    const channel = post.targets?.find((t) => t.channel_id === root.channel_id)?.channel_name
    const where = root.part > 0 ? `In comment ${root.part}` : 'In the post'

    return channel ? `${where} on ${channel}` : where
  }

  return (
    <Dialog
      open={open}
      onOpenChange={(o) => {
        onOpenChange(o)

        if (!o) {
          setCreated(null)
        }
      }}
    >
      <DialogContent className="max-h-[90svh] overflow-y-auto sm:max-w-xl" data-testid="post-share">
        <DialogHeader>
          <DialogTitle>Preview and feedback</DialogTitle>
          <DialogDescription>
            Anyone with the link sees the post as each channel will show it and can leave a comment, without signing in.
          </DialogDescription>
        </DialogHeader>

        <section className="grid gap-3" aria-labelledby="share-link-heading">
          <h3 id="share-link-heading" className="text-sm font-semibold">
            Share link
          </h3>
          {created?.url ? (
            <div className="grid gap-2">
              <div className="flex items-center gap-1.5 rounded-lg border bg-muted/40 py-1 pr-1 pl-3">
                <span className="min-w-0 flex-1 truncate font-mono text-xs" data-testid="preview-url">
                  {created.url}
                </span>
                <CopyButton value={created.url} label="Copy link" />
                <Button asChild size="icon-sm" variant="ghost" aria-label="Open preview">
                  <a href={created.url} target="_blank" rel="noopener noreferrer">
                    <ExternalLink />
                  </a>
                </Button>
              </div>
              <p className="text-xs text-muted-foreground">Shown only now. Make a new link if you lose it.</p>
            </div>
          ) : (
            <div className="flex flex-wrap items-end gap-2">
              <label className="grid gap-1.5 text-sm">
                <span>Link expires</span>
                <NativeSelect
                  className="w-36"
                  value={String(expiry)}
                  onChange={(e) => setExpiry(Number(e.target.value))}
                >
                  {EXPIRY.map(([v, label]) => (
                    <option key={v} value={v}>
                      {label}
                    </option>
                  ))}
                </NativeSelect>
              </label>
              <Button
                type="button"
                disabled={create.isPending}
                onClick={() => create.mutate()}
                data-testid="create-preview"
              >
                <Link2 /> {create.isPending ? 'Creating...' : 'Create link'}
              </Button>
            </div>
          )}
          {active.length > 0 && (
            <ul className="grid gap-1.5" aria-label="Active links">
              {active.map((l) => (
                <li key={l.id} className="flex items-center gap-2 text-xs text-muted-foreground">
                  <Link2 className="size-3.5" />
                  <span className="min-w-0 flex-1 truncate">
                    Made {timeAgo(l.created_at)} ·{' '}
                    {l.expires_at ? `expires ${formatDateTime(l.expires_at)}` : 'never expires'}
                  </span>
                  <Button
                    type="button"
                    size="xs"
                    variant="ghost"
                    disabled={revoke.isPending}
                    onClick={() => revoke.mutate(l.id)}
                  >
                    Revoke
                  </Button>
                </li>
              ))}
            </ul>
          )}
        </section>

        {/* Feedback shows once someone has commented; until then the dialog is just the share link. */}
        {feedback.length > 0 && (
          <section className="grid gap-3 border-t pt-4" aria-labelledby="feedback-heading">
            <h3 id="feedback-heading" className="text-sm font-semibold">
              Feedback {openCount > 0 && <span className="text-muted-foreground">({openCount} open)</span>}
            </h3>
            <div data-testid="post-feedback">
              <FeedbackThreads
                threads={threads}
                activeId={activeThread}
                onActivate={setActiveThread}
                placeOf={placeOf}
                onReply={(id, body) => reply.mutateAsync({ id, body })}
                onResolve={(id, resolved) => resolve.mutate({ id, resolved })}
                onRemove={(c) => removeComment.mutate(c.id)}
                pending={reply.isPending || resolve.isPending || removeComment.isPending}
                showEmail
              />
            </div>
          </section>
        )}
      </DialogContent>
    </Dialog>
  )
}

/**
 * The actions on a saved post, as an icon row in its sheet: open it on the platform, share a preview
 * (and read the feedback), duplicate, delete; plus Publish now while it can still be published.
 */
export function PostActions({ post, onDeleted }: { post: Post; onDeleted: () => void }) {
  const qc = useQueryClient()
  const router = useRouter()
  const params = useSearchParams()
  const [deleteOpen, setDeleteOpen] = useState(false)
  const [shareOpen, setShareOpen] = useState(false)
  // A post that went out is history: it stays (the API refuses to delete it), so there is no Delete.
  const deletable = !wentOut(post)
  const comments = useQuery({ queryKey: commentsKey(post.id), queryFn: () => api.postComments(post.id) })
  // Open threads (a thread's first comment, not resolved): the badge counts what still needs an answer.
  const feedbackCount = (comments.data?.data ?? NO_COMMENTS).filter((c) => !c.parent_id && !c.resolved_at).length
  const duplicate = useMutation({
    mutationFn: () => api.duplicatePost(post.id),
    onSuccess: (p) => {
      // The copy opens from the response, and the lists take it in from their first page (marked as this tab's
      // own change, so its event doesn't reload every loaded page once more).
      qc.setQueryData(qk.post(p.id), p)
      refetchPostListsFromStart(qc, p.id)
      invalidateAfterPostChange(qc, p.id, { usage: true })
      namedToast(postTitle(p, 40), 'was duplicated as a draft')
      // Same view behind it: the collection and filters stay in the URL.
      router.push(postHref(p.id, params.toString()), { scroll: false })
    },
    onError: (e) => toastApiError(e),
  })
  const del = useMutation({
    mutationFn: () => api.deletePost(post.id),
    onSuccess: () => {
      removePostFromLists(qc, post.id)
      invalidateAfterPostChange(qc, undefined, { usage: true })
      namedToast(postTitle(post, 40), 'was deleted')
      setDeleteOpen(false)
      onDeleted()
    },
    onError: (e) => toastApiError(e),
  })

  return (
    <>
      <div className="flex items-center gap-0.5" role="group" aria-label="Post actions" data-testid="post-actions-row">
        {/* Open exists only once something went out; a draft or a fresh duplicate has nowhere to open. */}
        <OpenOnPlatform post={post} />
        <Button
          type="button"
          size="icon-sm"
          variant="ghost"
          className="relative"
          aria-label={
            feedbackCount
              ? `Preview and feedback, ${feedbackCount} open comment${feedbackCount === 1 ? '' : 's'}`
              : 'Preview and feedback'
          }
          tooltip="Preview and feedback"
          onClick={() => setShareOpen(true)}
          data-testid="post-preview"
        >
          <Eye />
          {feedbackCount > 0 && (
            <span
              aria-hidden
              className="absolute -top-0.5 -right-0.5 min-w-4 rounded-full bg-emerald-700 px-1 text-center text-[10px] leading-4 font-semibold text-white tabular-nums dark:bg-brand dark:text-brand-foreground"
            >
              {feedbackCount}
            </span>
          )}
        </Button>
        <Button
          type="button"
          size="icon-sm"
          variant="ghost"
          aria-label="Duplicate"
          disabled={duplicate.isPending}
          onClick={() => duplicate.mutate()}
          data-testid="post-duplicate"
        >
          <Copy />
        </Button>
        {deletable && (
          <Button
            type="button"
            size="icon-sm"
            variant="destructive"
            aria-label="Delete"
            onClick={() => setDeleteOpen(true)}
            data-testid="post-delete"
          >
            <Trash2 />
          </Button>
        )}
      </div>
      {shareOpen && <ShareDialog post={post} open={shareOpen} onOpenChange={setShareOpen} />}
      <ConfirmDialog
        open={deleteOpen && deletable}
        onOpenChange={setDeleteOpen}
        title="Delete this post?"
        description="It will not be published."

        confirmLabel={del.isPending ? 'Deleting...' : 'Delete'}
        variant="destructive"
        onConfirm={() => del.mutate()}
      />
    </>
  )
}
