'use client'

import { Activity as ActivityIcon, Download, FileJson, FileSpreadsheet, RefreshCw, Search } from 'lucide-react'
import { usePathname, useRouter, useSearchParams } from 'next/navigation'
import { useState } from 'react'

import { EmptyState, ErrorState } from '@/components/common/error-state'
import { PageContainer, PageHeader } from '@/components/common/page-header'
import { SkeletonList } from '@/components/common/skeleton-list'
import { EventTimeline } from '@/components/items/event-timeline'
import { Button } from '@/components/ui/button'
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuLabel,
  DropdownMenuTrigger,
} from '@/components/ui/dropdown-menu'
import { Input } from '@/components/ui/input'
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from '@/components/ui/select'
import { eventsExportUrl, type EventExportQuery, type EventQuery } from '@/lib/api'
import { useCollections, useEventsInfinite, useMode } from '@/lib/queries'

export const COMMON_EVENT_TYPES = [
  'item.created',
  'item.updated',
  'item.status_changed',
  'item.approved',
  'item.rejected',
  'item.deleted',
  'item.regenerate_requested',
  'item.publish_requested',
  'item.retry_requested',
  'item.archived',
  'item.escalated',
  'item.rule_applied',
  'item.expired',
  'item.purged',
  'rule.created',
  'rule.updated',
  'rule.deleted',
  'watcher.created',
  'collection.created',
  'collection.updated',
  'collection.archived',
  'collection.deleted',
  'api_key.created',
  'api_key.revoked',
  'recovery_key.rotated',
  'session.recovered',
  'workspace.export_requested',
  'workspace.exported',
]

export function ActivityFeed() {
  const params = useSearchParams()
  const router = useRouter()
  const pathname = usePathname()
  const type = params.get('type') ?? ''
  const actorType = (params.get('actor_type') as EventQuery['actor_type']) || undefined
  const collectionId = params.get('collection') ?? ''
  const [customType, setCustomType] = useState(type && !COMMON_EVENT_TYPES.includes(type) ? type : '')
  const collections = useCollections(true)
  const { isLocal } = useMode()

  const query: EventQuery = {
    type: type || undefined,
    actor_type: actorType,
    collection_id: collectionId || undefined,
    limit: 50,
  }
  const events = useEventsInfinite(query)
  const exportQuery: Omit<EventExportQuery, 'format'> = {
    type: query.type,
    actor_type: query.actor_type,
    collection_id: query.collection_id,
  }
  const filtered = !!(query.type || query.actor_type || query.collection_id)
  const list = events.data?.pages.flatMap((p) => p.data) ?? []

  function setParam(key: string, value?: string) {
    const sp = new URLSearchParams(params.toString())

    if (value) {
      sp.set(key, value)
    } else {
      sp.delete(key)
    }

    router.replace(`${pathname}${sp.toString() ? `?${sp}` : ''}`, { scroll: false })
  }

  return (
    <PageContainer className="max-w-4xl">
      <PageHeader
        title="Activity"
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
                    <FileJson /> NDJSON
                  </a>
                </DropdownMenuItem>
                <DropdownMenuItem asChild>
                  <a href={eventsExportUrl({ ...exportQuery, format: 'csv' })} download data-testid="export-csv">
                    <FileSpreadsheet /> CSV
                  </a>
                </DropdownMenuItem>
              </DropdownMenuContent>
            </DropdownMenu>
            <Button variant="outline" size="sm" onClick={() => events.refetch()} disabled={events.isFetching}>
              <RefreshCw className={events.isFetching ? 'animate-spin' : undefined} /> Refresh
            </Button>
          </>
        }
      />
      <div className="mt-6 grid grid-cols-2 items-center gap-2 sm:flex sm:flex-wrap">
        <Select
          value={type && COMMON_EVENT_TYPES.includes(type) ? type : type ? 'custom' : 'all'}
          onValueChange={(v) => {
            if (v === 'custom') {
              return
            }

            setCustomType('')
            setParam('type', v === 'all' ? undefined : v)
          }}
        >
          <SelectTrigger size="sm" className="w-56 max-sm:col-span-2 max-sm:w-full" aria-label="Event type">
            <SelectValue />
          </SelectTrigger>
          <SelectContent>
            <SelectItem value="all">All event types</SelectItem>
            {COMMON_EVENT_TYPES.map((t) => (
              <SelectItem key={t} value={t}>
                <span className="font-mono text-xs">{t}</span>
              </SelectItem>
            ))}
            {type && !COMMON_EVENT_TYPES.includes(type) && (
              <SelectItem value="custom">
                <span className="font-mono text-xs">{type}</span>
              </SelectItem>
            )}
          </SelectContent>
        </Select>
        <form
          className="relative max-sm:col-span-2 max-sm:w-full"
          onSubmit={(e) => {
            e.preventDefault()
            setParam('type', customType.trim() || undefined)
          }}
        >
          <Search className="pointer-events-none absolute top-1/2 left-2.5 size-3.5 -translate-y-1/2 text-muted-foreground" />
          <Input
            value={customType}
            onChange={(e) => setCustomType(e.target.value)}
            placeholder="Exact type, e.g. item.publish_requested"
            className="h-8 w-72 pl-8 font-mono text-xs max-sm:w-full"
            aria-label="Custom event type"
          />
        </form>
        <Select value={actorType ?? 'all'} onValueChange={(v) => setParam('actor_type', v === 'all' ? undefined : v)}>
          <SelectTrigger size="sm" className="w-40 max-sm:w-full" aria-label="Actor type">
            <SelectValue />
          </SelectTrigger>
          <SelectContent>
            <SelectItem value="all">All actors</SelectItem>
            {isLocal && <SelectItem value="local">Local (you &amp; local agents)</SelectItem>}
            <SelectItem value="session">Dashboard sessions</SelectItem>
            <SelectItem value="api_key">API keys (agents)</SelectItem>
            <SelectItem value="system">System</SelectItem>
          </SelectContent>
        </Select>
        <Select
          value={collectionId || 'all'}
          onValueChange={(v) => setParam('collection', v === 'all' ? undefined : v)}
        >
          <SelectTrigger size="sm" className="w-44 max-sm:w-full" aria-label="Collection">
            <SelectValue />
          </SelectTrigger>
          <SelectContent>
            <SelectItem value="all">All collections</SelectItem>
            {(collections.data ?? []).map((c) => (
              <SelectItem key={c.id} value={c.id}>
                {c.name}
              </SelectItem>
            ))}
          </SelectContent>
        </Select>
      </div>

      <div className="mt-6">
        {events.isLoading ? (
          <div className="grid gap-4">
            <SkeletonList count={6} className="h-12" />
          </div>
        ) : events.error ? (
          <ErrorState error={events.error} onRetry={() => events.refetch()} />
        ) : list.length === 0 ? (
          <EmptyState icon={<ActivityIcon />} title="No events" description="Nothing matches these filters yet." />
        ) : (
          <div className="rounded-xl border bg-card p-4 sm:p-5">
            <EventTimeline events={list} showItem />
          </div>
        )}
        {events.hasNextPage && (
          <div className="mt-4 flex justify-center">
            <Button variant="outline" onClick={() => events.fetchNextPage()} disabled={events.isFetchingNextPage}>
              {events.isFetchingNextPage ? 'Loading…' : 'Load older events'}
            </Button>
          </div>
        )}
      </div>
    </PageContainer>
  )
}
