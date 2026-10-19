'use client'

import { useIsFetching, useQueryClient } from '@tanstack/react-query'
import {
  Activity,
  Activity as ActivityIcon,
  Download,
  FileJson,
  FileSpreadsheet,
  Plus,
  RefreshCw,
  Search,
  X,
} from 'lucide-react'
import { usePathname, useRouter, useSearchParams } from 'next/navigation'
import { useId, useMemo, useState } from 'react'

import { DateRangePicker } from '@/components/common/date-range-picker'
import { EmptyState, ErrorState } from '@/components/common/error-state'
import { PageContainer, PageHeader } from '@/components/common/page-header'
import { SkeletonList } from '@/components/common/skeleton-list'
import { Button } from '@/components/ui/button'
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuLabel,
  DropdownMenuTrigger,
} from '@/components/ui/dropdown-menu'
import { Input } from '@/components/ui/input'
import { Popover, PopoverContent, PopoverTrigger } from '@/components/ui/popover'
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from '@/components/ui/select'
import { eventsExportUrl, type Collection, type EventExportQuery, type EventQuery, type StashEvent } from '@/lib/api'
import { qk, refreshEventList, useCollections, useEventsInfinite, useMode } from '@/lib/queries'
import { cn, hrefWithParams } from '@/lib/utils'

import { AuditEventList } from './audit-event'

// Events staqly actually emits (the types a filter can pick from).
const EVENT_TYPES = [
  'post.created',
  'post.updated',
  'post.scheduled',
  'post.unscheduled',
  'post.publish_requested',
  'post.target_published',
  'post.target_failed',
  'post.published',
  'post.failed',
  'post.deleted',
  'channel.connected',
  'channel.updated',
  'channel.needs_reconnect',
  'channel.disconnected',
  'schedule.created',
  'schedule.updated',
  'schedule.deleted',
  'schedule.ran',
  'schedule.posts_requested',
  'schedule.failed',
  'collection.created',
  'collection.updated',
  'collection.archived',
  'collection.deleted',
  'alert.created',
  'alert.updated',
  'alert.deleted',
  'webhook.created',
  'webhook.updated',
  'webhook.deleted',
  'webhook.test',
  'api_key.created',
  'api_key.rotated',
  'api_key.revoked',
  'workspace.updated',
  'workspace.plan_changed',
]

const DATE_RE = /^\d{4}-\d{2}-\d{2}$/

function dateParam(value: string | null): string {
  return value && DATE_RE.test(value) && !Number.isNaN(Date.parse(`${value}T00:00:00Z`)) ? value : ''
}

/** The API's `to` is exclusive; the picker's To day is inclusive, so send the following day. */
export function nextDay(day: string): string {
  const d = new Date(`${day}T00:00:00Z`)

  d.setUTCDate(d.getUTCDate() + 1)

  return d.toISOString().slice(0, 10)
}

/** Midnight of a YYYY-MM-DD day in the browser's time zone, as RFC 3339 (the API compares in UTC). */
export function localDayStart(day: string): string {
  const [y, m, d] = day.split('-').map(Number)

  return new Date(y, m - 1, d).toISOString()
}

/** Text filter applied on Enter or blur, kept in the URL. */
function TextFilter({
  value,
  onCommit,
  placeholder,
  label,
  className,
  icon,
  suggestions,
}: {
  value: string
  onCommit: (value?: string) => void
  placeholder: string
  label: string
  className?: string
  icon?: boolean
  /** Offered by the browser as you type (a native datalist). */
  suggestions?: string[]
}) {
  const [draft, setDraft] = useState(value)
  const [prev, setPrev] = useState(value)
  const listId = useId()

  if (prev !== value) {
    setPrev(value)
    setDraft(value)
  }

  function commit() {
    const v = draft.trim()

    if (v !== value) {
      onCommit(v || undefined)
    }
  }

  return (
    <form
      className={cn('relative', className)}
      onSubmit={(e) => {
        e.preventDefault()
        commit()
      }}
    >
      {icon && (
        <Search className="pointer-events-none absolute top-1/2 left-2.5 size-3.5 -translate-y-1/2 text-muted-foreground" />
      )}
      {suggestions && (
        <datalist id={listId}>
          {suggestions.map((t) => (
            <option key={t} value={t}>
              {t}
            </option>
          ))}
        </datalist>
      )}
      <Input
        list={suggestions ? listId : undefined}
        value={draft}
        onChange={(e) => setDraft(e.target.value)}
        onBlur={commit}
        placeholder={placeholder}
        className={cn('h-8 w-full font-mono text-xs', icon && 'pl-8')}
        aria-label={label}
        spellCheck={false}
        autoComplete="off"
      />
    </form>
  )
}

const ACTOR_LABELS: Record<string, string> = {
  local: 'Local (you & local agents)',
  session: 'Dashboard sessions',
  api_key: 'API keys (agents)',
  system: 'System',
}

const ADD_FIELDS = [
  { key: 'actor_type', label: 'Actor' },
  { key: 'collection', label: 'Collection' },
  { key: 'actor_id', label: 'Actor ID' },
  { key: 'post', label: 'Post ID' },
] as const

/** "Add filter": pick a field, then a value; the field list stays short however many filters exist. */
function AddFilter({
  collections,
  isLocal,
  onApply,
}: {
  collections?: Collection[]
  isLocal: boolean
  onApply: (key: string, value?: string) => void
}) {
  const [open, setOpen] = useState(false)
  const [field, setField] = useState<(typeof ADD_FIELDS)[number]['key']>('actor_type')
  const [value, setValue] = useState('')

  function apply(e: React.FormEvent) {
    e.preventDefault()

    if (value.trim()) {
      onApply(field, value.trim())
      setOpen(false)
      setValue('')
    }
  }

  const picker =
    field === 'actor_type' || field === 'collection' ? (
      <Select value={value} onValueChange={setValue}>
        <SelectTrigger className="w-full" aria-label="Filter value">
          <SelectValue placeholder="Choose..." />
        </SelectTrigger>
        <SelectContent>
          {field === 'actor_type'
            ? Object.entries(ACTOR_LABELS)
                .filter(([k]) => isLocal || k !== 'local')
                .map(([k, l]) => (
                  <SelectItem key={k} value={k}>
                    {l}
                  </SelectItem>
                ))
            : (collections ?? []).map((c) => (
                <SelectItem key={c.id} value={c.id}>
                  {c.name}
                </SelectItem>
              ))}
        </SelectContent>
      </Select>
    ) : (
      <Input
        value={value}
        onChange={(e) => setValue(e.target.value)}
        placeholder={field === 'actor_id' ? 'key_... or ses_...' : 'itm_...'}
        className="h-8 font-mono text-xs"
        aria-label="Filter value"
        spellCheck={false}
        autoComplete="off"
      />
    )

  return (
    <Popover open={open} onOpenChange={setOpen}>
      <PopoverTrigger asChild>
        <Button variant="outline" className="h-8">
          <Plus /> Add filter
        </Button>
      </PopoverTrigger>
      <PopoverContent align="end" className="w-72">
        <form className="grid gap-2" onSubmit={apply}>
          <Select
            value={field}
            onValueChange={(v) => {
              setField(v as typeof field)
              setValue('')
            }}
          >
            <SelectTrigger className="w-full" aria-label="Filter field">
              <SelectValue />
            </SelectTrigger>
            <SelectContent>
              {ADD_FIELDS.map((f) => (
                <SelectItem key={f.key} value={f.key}>
                  {f.label}
                </SelectItem>
              ))}
            </SelectContent>
          </Select>
          {picker}
          <Button type="submit" size="sm" className="justify-self-end">
            Apply
          </Button>
        </form>
      </PopoverContent>
    </Popover>
  )
}

/**
 * Refresh: only new events (a refetch would reload every page already loaded, one after another). It follows the
 * list's fetching itself, so a live event or "Load older" does not render the page header around it.
 */
function RefreshButton({ query }: { query: EventQuery }) {
  const qc = useQueryClient()
  const queryKey = useMemo(() => qk.events(query), [query])
  const fetching = useIsFetching({ queryKey, exact: true }) > 0

  return (
    <Button variant="outline" size="sm" onClick={() => void refreshEventList(qc, queryKey)} disabled={fetching}>
      <RefreshCw className={fetching ? 'animate-spin' : undefined} /> Refresh
    </Button>
  )
}

const FILTER_PARAMS = ['type', 'actor_type', 'actor_id', 'collection', 'post', 'from', 'to']
const NO_EVENTS: StashEvent[] = []

interface Chip {
  key: string
  label: string
  value: string
  mono?: boolean
}

export function ActivityFeed() {
  const params = useSearchParams()
  const router = useRouter()
  const pathname = usePathname()
  const type = params.get('type') ?? ''
  const actorType = (params.get('actor_type') as EventQuery['actor_type']) || undefined
  const collectionId = params.get('collection') ?? ''
  const actorId = params.get('actor_id') ?? ''
  const postId = params.get('post') ?? ''
  const from = dateParam(params.get('from'))
  const to = dateParam(params.get('to'))
  const collections = useCollections(true)
  const { isLocal } = useMode()

  // Memoised: the header's export links and Refresh button depend on them, and a new event must not render it.
  const exportQuery = useMemo<Omit<EventExportQuery, 'format'>>(
    () => ({
      type: type || undefined,
      actor_type: actorType,
      actor_id: actorId || undefined,
      collection_id: collectionId || undefined,
      post_id: postId || undefined,
      // Days in the user's own time zone: from its midnight to the midnight after the To day.
      from: from ? localDayStart(from) : undefined,
      to: to ? localDayStart(nextDay(to)) : undefined,
    }),
    [type, actorType, actorId, collectionId, postId, from, to],
  )
  const query = useMemo<EventQuery>(() => ({ ...exportQuery, limit: 50 }), [exportQuery])
  // The parts, not the result object: it is new on every render and would un-memoise the page around the list.
  const { data, error, isLoading, isPlaceholderData, isFetchingNextPage, hasNextPage, fetchNextPage, refetch } =
    useEventsInfinite(query)
  const filtered = Object.values(exportQuery).some(Boolean)
  // useMemo: a fresh array would re-render the whole list (and the page around it) on every render.
  const list = useMemo(() => data?.pages.flatMap((p) => p.data) ?? NO_EVENTS, [data])
  const collectionName = collectionId
    ? (collections.data?.find((c) => c.id === collectionId)?.name ?? collectionId)
    : ''

  function setParams(patch: Record<string, string | undefined>) {
    router.replace(hrefWithParams(pathname, params, patch), { scroll: false })
  }

  const chips = useMemo(
    () =>
      [
        type && { key: 'type', label: 'Type', value: type, mono: true },
        actorType && { key: 'actor_type', label: 'Actor', value: ACTOR_LABELS[actorType] ?? actorType },
        collectionId && { key: 'collection', label: 'Collection', value: collectionName },
        actorId && { key: 'actor_id', label: 'Actor ID', value: actorId, mono: true },
        postId && { key: 'post', label: 'Post ID', value: postId, mono: true },
        (from || to) && { key: 'dates', label: 'Dates', value: `${from || '...'} → ${to || '...'}` },
      ].filter(Boolean) as Chip[],
    [type, actorType, collectionId, collectionName, actorId, postId, from, to],
  )

  function setParam(key: string, value?: string) {
    setParams({ [key]: value })
  }

  function clearFilters() {
    setParams(Object.fromEntries(FILTER_PARAMS.map((k) => [k, undefined])))
  }

  return (
    <PageContainer>
      <PageHeader
        title="Activity"
        icon={<Activity className="size-4" />}
        description="Everything that happened in this workspace — by you, your agents and the system."
        actions={
          <>
            <DropdownMenu>
              <DropdownMenuTrigger asChild>
                <Button variant="outline" size="sm">
                  <Download /> Export
                </Button>
              </DropdownMenuTrigger>
              <DropdownMenuContent align="end" className="min-w-56">
                <DropdownMenuLabel className="text-xs font-normal text-muted-foreground">
                  {filtered ? 'Events matching the current filters' : 'All events'}, oldest first
                </DropdownMenuLabel>
                <DropdownMenuItem asChild>
                  <a href={eventsExportUrl({ ...exportQuery, format: 'ndjson' })} download data-testid="export-ndjson">
                    <FileJson /> JSONL (one event per line)
                  </a>
                </DropdownMenuItem>
                <DropdownMenuItem asChild>
                  <a href={eventsExportUrl({ ...exportQuery, format: 'csv' })} download data-testid="export-csv">
                    <FileSpreadsheet /> CSV
                  </a>
                </DropdownMenuItem>
              </DropdownMenuContent>
            </DropdownMenu>
            <RefreshButton query={query} />
          </>
        }
      />
      {/* One bar, like Kibana: event type search, dates and "Add filter"; active filters show as chips. */}
      <div className="mt-6 grid gap-2" role="group" aria-label="Filter events">
        <div className="flex flex-wrap items-center gap-2">
          <TextFilter
            value={type}
            onCommit={(v) => setParam('type', v)}
            placeholder="Event type, e.g. post.published"
            label="Event type"
            className="min-w-56 flex-1"
            icon
            suggestions={EVENT_TYPES}
          />
          <DateRangePicker
            value={from && to ? { from, to } : from ? { from, to: from } : to ? { from: to, to } : null}
            onChange={(r) => setParams({ from: r?.from, to: r?.to })}
            label="Dates"
            placeholder="Any date"
            className="max-sm:w-full [&>button:first-child]:max-sm:flex-1"
          />
          <AddFilter collections={collections.data} isLocal={isLocal} onApply={setParam} />
        </div>
        {filtered && (
          <div className="flex flex-wrap items-center gap-1.5" data-testid="active-filters">
            {chips.map((c) => (
              <span
                key={c.key}
                className="inline-flex h-7 items-center gap-1 rounded-full border bg-muted/40 pr-1 pl-2.5 text-xs"
              >
                <span className="text-muted-foreground">{c.label}:</span>
                <span className={cn('max-w-56 truncate', c.mono && 'font-mono')}>{c.value}</span>
                <Button
                  variant="ghost"
                  size="icon-xs"
                  className="rounded-full"
                  aria-label={`Remove ${c.label} filter`}
                  onClick={() => (c.key === 'dates' ? setParams({ from: undefined, to: undefined }) : setParam(c.key))}
                >
                  <X />
                </Button>
              </span>
            ))}
            <Button variant="ghost" size="xs" onClick={clearFilters} aria-label="Clear filters">
              Clear all
            </Button>
          </div>
        )}
      </div>

      <div className={cn('mt-6 transition-opacity', isPlaceholderData && 'opacity-60')} aria-busy={isPlaceholderData}>
        {isLoading ? (
          <div className="grid gap-4">
            <SkeletonList count={6} className="h-12" />
          </div>
        ) : error ? (
          <ErrorState error={error} onRetry={() => refetch()} />
        ) : list.length === 0 ? (
          <EmptyState icon={<ActivityIcon />} title="No events" description="Nothing matches these filters yet." />
        ) : (
          <div className="rounded-xl border bg-card p-4 sm:p-5">
            <AuditEventList events={list} />
          </div>
        )}
        {hasNextPage && (
          <div className="mt-4 flex justify-center">
            <Button variant="outline" onClick={() => void fetchNextPage()} disabled={isFetchingNextPage}>
              {isFetchingNextPage ? 'Loading...' : 'Load older events'}
            </Button>
          </div>
        )}
      </div>
    </PageContainer>
  )
}
