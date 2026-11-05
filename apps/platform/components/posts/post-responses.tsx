'use client'

import { useQuery } from '@tanstack/react-query'
import { ChevronDown, Heart, MessageSquare, type LucideIcon } from 'lucide-react'
import { useState } from 'react'

import { ErrorState } from '@/components/common/error-state'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import { Collapsible, CollapsibleContent, CollapsibleTrigger } from '@/components/ui/collapsible'
import { Skeleton } from '@/components/ui/skeleton'
import { api, type PostResponses as Responses } from '@/lib/api'
import { formatDateTime, timeAgo } from '@/lib/format'
import { qk } from '@/lib/queries'
import { safeHttpUrl } from '@/lib/safe'
import { cn } from '@/lib/utils'

type Insight = NonNullable<Responses['overall']>

const FILTERS = ['all', 'positive', 'neutral', 'negative'] as const
const SHOWN = 50
const WAITING = "This month's AI allowance is used up; the comments are kept and analysed once it renews."

// Sentiment colours: positive green, neutral muted, negative destructive.
const BAR = { positive: 'bg-emerald-500', neutral: 'bg-muted-foreground/40', negative: 'bg-destructive' }

function SentimentBar({ i }: { i: Insight }) {
  const total = i.positive + i.neutral + i.negative

  if (total === 0) {
    return null
  }

  return (
    <div className="grid gap-1">
      <div
        className="flex h-2 overflow-hidden rounded-full bg-muted"
        role="img"
        aria-label={`${i.positive} positive, ${i.neutral} neutral, ${i.negative} negative`}
      >
        {(['positive', 'neutral', 'negative'] as const).map((k) => (
          <div key={k} className={BAR[k]} style={{ width: `${(i[k] / total) * 100}%` }} />
        ))}
      </div>
      <p className="text-xs text-muted-foreground tabular-nums">
        {i.positive} positive · {i.neutral} neutral · {i.negative} negative
      </p>
    </div>
  )
}

/** One analysis. `quiet` leaves out its error when the card around it already says it (the same read failed). */
function InsightView({ i, quiet }: { i: Insight; quiet?: boolean }) {
  return (
    <div className="grid gap-2">
      <SentimentBar i={i} />
      {i.status === 'waiting_credits' && <p className="text-sm text-muted-foreground">{WAITING}</p>}
      {i.status === 'failed' && i.error && !quiet && (
        <p className="text-xs text-amber-700 dark:text-amber-400">Couldn&apos;t refresh the comments: {i.error}</p>
      )}
      {i.summary && <p className="text-sm whitespace-pre-wrap">{i.summary}</p>}
      {i.themes.length > 0 && (
        <div className="flex flex-wrap gap-1.5" aria-label="Themes">
          {i.themes.map((t) => (
            <Badge key={t} variant="secondary">
              {t}
            </Badge>
          ))}
        </div>
      )}
      {i.questions.length > 0 && (
        <div>
          <p className="text-xs font-medium text-muted-foreground">Open questions</p>
          <ul className="list-disc pl-5 text-sm">
            {i.questions.map((q) => (
              <li key={q}>{q}</li>
            ))}
          </ul>
        </div>
      )}
    </div>
  )
}

export type SentimentFilter = (typeof FILTERS)[number]

/** The post's comments, one request shared by the overview and every channel's section. */
function useResponses(postId: string, active: boolean) {
  return useQuery({ queryKey: qk.postResponses(postId), queryFn: () => api.postResponses(postId), enabled: active })
}

/**
 * "Audience comments" over the channels: the AI's reading of every channel's comments together and the sentiment
 * filter. Nothing until there are comments: each channel says so in its own section.
 */
export function CommentsOverview({
  postId,
  active,
  filter,
  onFilter,
}: {
  postId: string
  active: boolean
  filter: SentimentFilter
  onFilter: (f: SentimentFilter) => void
}) {
  const q = useResponses(postId, active)
  const d = q.data

  if (q.error) {
    return <ErrorState error={q.error} onRetry={() => q.refetch()} />
  }

  if (!d || (!d.overall && d.channels.every((c) => c.responses.length === 0))) {
    return null
  }

  return (
    <section className="grid gap-3 rounded-xl border p-3" aria-label="Audience comments" data-testid="post-responses">
      <h3 className="text-sm font-semibold">Audience comments</h3>
      {d.overall && <InsightView i={d.overall} />}
      <div className="flex flex-wrap gap-1.5" role="group" aria-label="Filter by sentiment">
        {FILTERS.map((f) => (
          <Button
            key={f}
            type="button"
            size="sm"
            variant={filter === f ? 'default' : 'outline'}
            aria-pressed={filter === f}
            onClick={() => onFilter(f)}
            className="capitalize"
          >
            {f}
          </Button>
        ))}
      </div>
    </section>
  )
}

/** One channel's comments, in its section of the post's Channels tab. */
export function ChannelComments({
  postId,
  channelId,
  active,
  filter,
  quietError,
}: {
  postId: string
  channelId: string
  active: boolean
  filter: SentimentFilter
  /** The channel's card already says its read failed (the numbers'): no second message for the comments. */
  quietError?: boolean
}) {
  const q = useResponses(postId, active)
  const [all, setAll] = useState(false)
  const c = q.data?.channels.find((x) => x.channel_id === channelId)

  if (q.isPending) {
    return <Skeleton className="h-4 w-28" />
  }

  if (!c) {
    return null
  }

  if (!c.available) {
    return (
      <Unavailable icon={MessageSquare} what="Comments" why={c.error || "this platform doesn't let staqly read them"} />
    )
  }

  const rows = c.responses.filter((r) => filter === 'all' || r.sentiment === filter)
  // The API lists newest first.
  const shown = all ? rows : rows.slice(0, SHOWN)

  return (
    <Collapsible defaultOpen={c.responses.length > 0} data-testid={`responses-${c.channel_id}`}>
      <CollapsibleTrigger className="group flex items-center gap-1.5 text-xs font-medium text-muted-foreground hover:text-foreground">
        Comments <span className="tabular-nums">{c.responses.length}</span>
        <ChevronDown aria-hidden className="size-3.5 transition-transform group-data-[state=open]:rotate-180" />
      </CollapsibleTrigger>
      <CollapsibleContent className="mt-2 grid gap-3">
        {c.error && !quietError && (
          <p className="text-xs text-amber-700 dark:text-amber-400">Couldn&apos;t refresh the comments: {c.error}</p>
        )}
        {c.insight && <InsightView i={c.insight} quiet={!!c.error} />}
        {rows.length === 0 ? (
          <p className="text-sm text-muted-foreground">No comments yet</p>
        ) : (
          <ul className="grid gap-2">
            {shown.map((r) => {
              const url = r.url ? safeHttpUrl(r.url) : null

              return (
                <li key={r.id} className={cn('rounded-lg bg-muted/30 px-3 py-2', r.parent_id && 'ml-4')}>
                  <div className="flex items-center gap-2 text-xs text-muted-foreground">
                    <span className="font-medium text-foreground">{r.author}</span>
                    {r.author_handle && <span>{r.author_handle}</span>}
                    <span title={formatDateTime(r.created_at)}>{timeAgo(r.created_at)}</span>
                    {r.likes != null && (
                      <span className="inline-flex items-center gap-0.5 tabular-nums">
                        <Heart aria-hidden className="size-3" /> {r.likes}
                      </span>
                    )}
                    {url && (
                      <a href={url} target="_blank" rel="noreferrer noopener" className="ml-auto underline">
                        Open
                      </a>
                    )}
                  </div>
                  <p className="mt-1 text-sm break-words whitespace-pre-wrap">{r.text}</p>
                </li>
              )
            })}
          </ul>
        )}
        {rows.length > SHOWN && !all && (
          <div>
            <Button type="button" variant="outline" size="sm" onClick={() => setAll(true)}>
              Show all {rows.length}
            </Button>
          </div>
        )}
      </CollapsibleContent>
    </Collapsible>
  )
}

/** What a channel can't show (numbers, comments): its own labelled row, so two of them don't read as one sentence. */
export function Unavailable({ icon: Icon, what, why }: { icon: LucideIcon; what: string; why: string }) {
  return (
    <p className="flex items-start gap-2 rounded-lg bg-muted/50 px-2.5 py-1.5 text-xs" data-testid="unavailable">
      <Icon aria-hidden className="mt-px size-3.5 shrink-0 text-muted-foreground" />
      <span>
        <span className="font-medium">{what}</span>
        <span className="text-muted-foreground">: {why}</span>
      </span>
    </p>
  )
}
