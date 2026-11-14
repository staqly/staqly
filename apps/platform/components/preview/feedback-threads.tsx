'use client'

import { Check, ChevronRight, MessageSquareReply, RotateCcw, Trash2 } from 'lucide-react'
import { memo, useState } from 'react'

import { Button } from '@/components/ui/button'
import { Textarea } from '@/components/ui/textarea'
import { authorChip, initials, type Thread } from '@/lib/annotations'
import type { PostComment } from '@/lib/api'
import { formatDateTime, timeAgo } from '@/lib/format'
import { TONES } from '@/lib/tones'
import { cn } from '@/lib/utils'

/**
 * Feedback as threads, on the preview page and in the dashboard: each thread's passage (or "Text changed"), its
 * comments, Reply, Resolve / Reopen. Resolved threads fold into their own list. Hovering or focusing a thread
 * marks its passage in the post (activeId), and a click on a passage focuses its thread (element id thread-<id>).
 */
export interface FeedbackThreadsProps {
  threads: Thread[]
  activeId: string | null
  onActivate: (id: string | null) => void
  /** Where a passage is: "Comment 1 on LinkedIn". */
  placeOf: (root: PostComment) => string
  onReply: (threadId: string, body: string) => Promise<unknown>
  onResolve: (threadId: string, resolved: boolean) => void
  onRemove?: (comment: PostComment) => void
  pending?: boolean
  /** The team sees reviewers' emails. */
  showEmail?: boolean
}

export function FeedbackThreads(props: FeedbackThreadsProps) {
  const [showResolved, setShowResolved] = useState(false)
  const open = props.threads.filter((t) => !t.root.resolved_at)
  const resolved = props.threads.filter((t) => t.root.resolved_at)

  return (
    <div className="grid gap-3">
      {open.length > 0 && (
        <ul className="grid gap-2" aria-label="Open comments" data-testid="open-threads">
          {open.map((t) => (
            <ThreadItem key={t.root.id} thread={t} active={props.activeId === t.root.id} {...props} />
          ))}
        </ul>
      )}
      {resolved.length > 0 && (
        <div className="grid gap-2">
          <Button
            type="button"
            variant="ghost"
            size="sm"
            className="w-fit px-1.5 text-muted-foreground"
            aria-expanded={showResolved}
            onClick={() => setShowResolved((v) => !v)}
          >
            <ChevronRight className={cn('transition-transform', showResolved && 'rotate-90')} />
            Resolved ({resolved.length})
          </Button>
          {showResolved && (
            <ul className="grid gap-2" aria-label="Resolved comments" data-testid="resolved-threads">
              {resolved.map((t) => (
                <ThreadItem key={t.root.id} thread={t} active={false} {...props} />
              ))}
            </ul>
          )}
        </div>
      )}
    </div>
  )
}

function Author({ c, showEmail }: { c: PostComment; showEmail?: boolean }) {
  return (
    <div className="flex min-w-0 items-center gap-2">
      <span
        aria-hidden
        className={cn(
          'grid size-6 shrink-0 place-items-center rounded-full text-[10px] font-semibold',
          authorChip(c.author_name),
        )}
      >
        {initials(c.author_name)}
      </span>
      <span className="min-w-0 truncate text-sm font-medium" title={c.author_name}>
        {c.author_name}
      </span>
      {c.author_kind === 'team' && <span className="shrink-0 text-xs text-muted-foreground">Team</span>}
      {showEmail && c.author_email && (
        <a
          className="min-w-0 truncate text-xs text-muted-foreground underline-offset-2 hover:underline"
          href={`mailto:${c.author_email}`}
        >
          {c.author_email}
        </a>
      )}
      <span className="ml-auto shrink-0 text-xs text-muted-foreground" title={formatDateTime(c.created_at)}>
        {timeAgo(c.created_at)}
      </span>
    </div>
  )
}

const ThreadItem = memo(function ThreadItem({
  thread,
  active,
  onActivate,
  placeOf,
  onReply,
  onResolve,
  onRemove,
  pending,
  showEmail,
}: FeedbackThreadsProps & { thread: Thread; active: boolean }) {
  const { root, replies } = thread
  const [replying, setReplying] = useState(false)
  const [body, setBody] = useState('')
  const [error, setError] = useState('')
  const resolved = !!root.resolved_at
  const send = async () => {
    if (!body.trim()) {
      setError('Write a reply.')

      return
    }

    try {
      await onReply(root.id, body.trim())
      setBody('')
      setError('')
      setReplying(false)
    } catch {
      // The caller shows the error; the text stays for another try.
    }
  }

  return (
    // oxlint-disable-next-line jsx-a11y/no-noninteractive-element-interactions -- hover only mirrors focus (onFocus), which marks the passage for keyboard users
    <li
      id={`thread-${root.id}`}
      tabIndex={-1}
      aria-label={`Comment from ${root.author_name}`}
      className={cn(
        'grid min-w-0 gap-2 rounded-xl border p-3 text-sm wrap-anywhere outline-none transition-colors focus-visible:ring-2 focus-visible:ring-ring',
        active && 'border-brand bg-brand-soft/30',
        resolved && 'bg-muted/30',
      )}
      onMouseEnter={() => onActivate(root.id)}
      onMouseLeave={() => onActivate(null)}
      onFocus={() => onActivate(root.id)}
      data-testid="feedback-thread"
    >
      <Author c={root} showEmail={showEmail} />
      {root.quote && (
        <div className="grid gap-1">
          <blockquote
            className={cn(
              'border-l-2 pl-2 text-xs wrap-anywhere text-muted-foreground italic',
              root.text_changed
                ? 'border-muted-foreground/40 line-through decoration-muted-foreground/40'
                : 'border-brand',
            )}
          >
            {root.quote}
          </blockquote>
          <p className="flex flex-wrap items-center gap-1.5 text-xs text-muted-foreground">
            {root.text_changed && (
              <span className={cn('rounded-full px-1.5 py-px ring-1 ring-inset', TONES.warning.badge)}>
                Text changed
              </span>
            )}
            <span>{placeOf(root)}</span>
          </p>
        </div>
      )}
      <p className="whitespace-pre-wrap">{root.body}</p>
      {replies.length > 0 && (
        <ul className="grid gap-2 border-l-2 pl-3" aria-label="Replies">
          {replies.map((r) => (
            <li key={r.id} className="grid gap-1">
              <Author c={r} showEmail={showEmail} />
              <p className="whitespace-pre-wrap">{r.body}</p>
              {onRemove && (
                <Button
                  type="button"
                  size="xs"
                  variant="ghost"
                  className="w-fit text-destructive hover:text-destructive"
                  aria-label={`Remove the reply from ${r.author_name}`}
                  disabled={pending}
                  onClick={() => onRemove(r)}
                >
                  <Trash2 /> Remove
                </Button>
              )}
            </li>
          ))}
        </ul>
      )}
      {replying && (
        <div className="grid gap-2">
          <Textarea
            rows={2}
            maxLength={2000}
            value={body}
            aria-label={`Reply to ${root.author_name}`}
            aria-invalid={!!error}
            placeholder="Write a reply"
            // oxlint-disable-next-line jsx-a11y/no-autofocus -- opened by the Reply button: typing follows the click
            autoFocus
            onChange={(e) => setBody(e.target.value)}
            onKeyDown={(e) => {
              if (e.key === 'Enter' && (e.metaKey || e.ctrlKey)) {
                e.preventDefault()
                void send()
              }
            }}
          />
          {error && <p className="text-xs text-destructive">{error}</p>}
          <div className="flex justify-end gap-2">
            <Button type="button" size="sm" variant="ghost" onClick={() => setReplying(false)}>
              Cancel
            </Button>
            <Button type="button" size="sm" disabled={pending} onClick={() => void send()}>
              Reply
            </Button>
          </div>
        </div>
      )}
      <div className="flex flex-wrap items-center gap-1">
        {!replying && !resolved && (
          <Button type="button" size="sm" variant="ghost" onClick={() => setReplying(true)}>
            <MessageSquareReply /> Reply
          </Button>
        )}
        <Button
          type="button"
          size="sm"
          variant="ghost"
          disabled={pending}
          onClick={() => onResolve(root.id, !resolved)}
        >
          {resolved ? (
            <>
              <RotateCcw /> Reopen
            </>
          ) : (
            <>
              <Check /> Resolve
            </>
          )}
        </Button>
        {resolved && root.resolved_by && (
          <span className="text-xs text-muted-foreground">Resolved by {root.resolved_by}</span>
        )}
        {onRemove && (
          <Button
            type="button"
            size="icon-sm"
            variant="ghost"
            className="ml-auto text-destructive hover:text-destructive"
            aria-label={`Remove the comment from ${root.author_name} and its replies`}
            disabled={pending}
            onClick={() => onRemove(root)}
          >
            <Trash2 />
          </Button>
        )}
      </div>
    </li>
  )
})
