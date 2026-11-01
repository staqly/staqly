'use client'

import { ProviderIcon } from '@staqly/brand/providers'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { ChevronDown, Heart, RefreshCw } from 'lucide-react'
import { useState } from 'react'

import { ErrorState } from '@/components/common/error-state'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import { Collapsible, CollapsibleContent, CollapsibleTrigger } from '@/components/ui/collapsible'
import { Skeleton } from '@/components/ui/skeleton'
import { api, type PostResponses as Responses } from '@/lib/api'
import { formatDateTime, timeAgo } from '@/lib/format'
import { toastApiError } from '@/lib/notify'
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

function InsightView({ i }: { i: Insight }) {
  return (
    <div className="grid gap-2">
      <SentimentBar i={i} />
      {i.status === 'waiting_credits' && <p className="text-sm text-muted-foreground">{WAITING}</p>}
      {i.status === 'failed' && i.error && <p className="text-sm text-destructive">{i.error}</p>}
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

function ChannelCard({ c, filter }: { c: Responses['channels'][number]; filter: (typeof FILTERS)[number] }) {
  const [all, setAll] = useState(false)
  const rows = c.responses.filter((r) => filter === 'all' || r.sentiment === filter)
  // The API lists newest first.
  const shown = all ? rows : rows.slice(0, SHOWN)

  return (
    <li className="rounded-xl border p-3" data-testid={`responses-${c.channel_id}`}>
      <Collapsible defaultOpen>
        <CollapsibleTrigger className="group flex w-full items-center gap-2 text-left text-sm">
          <ProviderIcon id={c.provider} className="size-4" />
          <span className="min-w-0 truncate font-medium">{c.channel_name}</span>
          {c.available && <span className="text-xs text-muted-foreground">{c.responses.length}</span>}
          <ChevronDown aria-hidden className="ml-auto size-4 transition-transform group-data-[state=open]:rotate-180" />
        </CollapsibleTrigger>
        <CollapsibleContent className="mt-2 grid gap-3">
          {!c.available ? (
            <p className="text-sm text-muted-foreground">
              {c.error || "This platform doesn't let staqly read comments"}
            </p>
          ) : (
            <>
              {c.error && <p className="text-sm text-destructive">{c.error}</p>}
              {c.insight && <InsightView i={c.insight} />}
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
            </>
          )}
        </CollapsibleContent>
      </Collapsible>
    </li>
  )
}

/** "Audience comments": what people answered under the post, with the AI's reading. Fetches only while `active`. */
export function PostResponses({ postId, active }: { postId: string; active: boolean }) {
  const qc = useQueryClient()
  const key = ['post-responses', postId]
  const [filter, setFilter] = useState<(typeof FILTERS)[number]>('all')
  const q = useQuery({ queryKey: key, queryFn: () => api.postResponses(postId), enabled: active })
  const refresh = useMutation({
    mutationFn: () => api.refreshPostResponses(postId),
    onSuccess: () => qc.invalidateQueries({ queryKey: key }),
    onError: (e) => toastApiError(e),
  })
  const d = q.data
  const none = d && !d.overall && d.channels.every((c) => c.responses.length === 0)

  return (
    <section className="grid gap-3 border-t pt-4" aria-label="Audience comments" data-testid="post-responses">
      <div className="flex items-center gap-2">
        <h3 className="text-sm font-semibold">Audience comments</h3>
        <Button
          type="button"
          variant="outline"
          size="sm"
          className="ml-auto"
          disabled={refresh.isPending}
          onClick={() => refresh.mutate()}
        >
          <RefreshCw className={refresh.isPending ? 'animate-spin' : undefined} /> Refresh comments
        </Button>
      </div>
      {q.isPending ? (
        <Skeleton className="h-5 w-32" />
      ) : q.error ? (
        <ErrorState error={q.error} onRetry={() => q.refetch()} />
      ) : !d || none ? (
        <p className="text-sm text-muted-foreground">No comments yet</p>
      ) : (
        <>
          {d.overall && <InsightView i={d.overall} />}
          <div className="flex flex-wrap gap-1.5" role="group" aria-label="Filter by sentiment">
            {FILTERS.map((f) => (
              <Button
                key={f}
                type="button"
                size="sm"
                variant={filter === f ? 'default' : 'outline'}
                aria-pressed={filter === f}
                onClick={() => setFilter(f)}
                className="capitalize"
              >
                {f}
              </Button>
            ))}
          </div>
          <ul className="grid gap-3">
            {d.channels.map((c) => (
              <ChannelCard key={c.channel_id} c={c} filter={filter} />
            ))}
          </ul>
        </>
      )}
    </section>
  )
}
