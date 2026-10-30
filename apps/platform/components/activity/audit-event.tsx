'use client'

import {
  Bell,
  Bot,
  CalendarClock,
  Cog,
  Folder,
  KeyRound,
  Laptop,
  Package,
  PenSquare,
  Share2,
  User,
  Webhook,
  Workflow,
} from 'lucide-react'
import Link from 'next/link'
import { memo, useMemo } from 'react'

import { DisclosureCard } from '@/components/common/disclosure-card'
import { IdChip } from '@/components/common/id-chip'
import { JsonCode } from '@/components/common/json-code'
import { PostStatusBadge } from '@/components/common/status-badge'
import type { StashEvent } from '@/lib/api'
import { formatDateTime, timeAgo } from '@/lib/format'
import { postTitle } from '@/lib/posts'
import { useCollections, useMode } from '@/lib/queries'
import { TONES } from '@/lib/tones'
import { cn } from '@/lib/utils'

import { CollectionLink, eventData, eventIcon, eventTitle, systemActorKind } from './event-meta'

type Icon = typeof Bot

export interface EventEntity {
  kind: string
  Icon: Icon
  id?: string
  name?: string
  href?: string
}

interface ObjectKind {
  prefix: string
  kind: string
  Icon: Icon
  /** Key of the embedded object in `data` (e.g. data.webhook). */
  obj?: string
  /** Keys in `data` that hold the object's id when it is not embedded. */
  idKeys: string[]
  nameKeys: string[]
  href?: string
}

// Events about other objects carry them in `data` (see apps/api internal/app emit calls).
const OBJECT_KINDS: ObjectKind[] = [
  {
    prefix: 'webhook.',
    kind: 'Webhook',
    Icon: Webhook,
    obj: 'webhook',
    idKeys: ['webhook_id'],
    nameKeys: ['description', 'url'],
    href: '/webhooks',
  },
  {
    prefix: 'channel.',
    kind: 'Channel',
    Icon: Share2,
    obj: 'channel',
    idKeys: ['channel_id'],
    nameKeys: ['name', 'handle'],
    href: '/channels',
  },
  {
    prefix: 'schedule.',
    kind: 'Schedule',
    Icon: CalendarClock,
    obj: 'schedule',
    idKeys: ['schedule_id'],
    nameKeys: ['name'],
  },
  {
    prefix: 'alert.',
    kind: 'Alert',
    Icon: Bell,
    obj: 'alert',
    idKeys: ['alert_id'],
    nameKeys: ['name'],
    href: '/alerts',
  },
  {
    prefix: 'api_key.',
    kind: 'API key',
    Icon: KeyRound,
    obj: 'api_key',
    idKeys: ['api_key_id', 'old_key_id'],
    nameKeys: ['name'],
    href: '/integrations/api-keys',
  },
  { prefix: 'session.', kind: 'Session', Icon: User, idKeys: ['session_id'], nameKeys: [] },
  {
    prefix: 'workspace.export',
    kind: 'Export',
    Icon: Package,
    idKeys: ['export_id'],
    nameKeys: [],
    href: '/settings/data',
  },
  { prefix: 'workspace.', kind: 'Workspace', Icon: Cog, obj: 'workspace', idKeys: [], nameKeys: ['name'] },
]

function str(v: unknown): string | undefined {
  return typeof v === 'string' && v ? v : undefined
}

function pick(source: Record<string, unknown> | undefined, keys: string[]): string | undefined {
  if (!source) {
    return undefined
  }

  for (const k of keys) {
    const v = str(source[k])

    if (v) {
      return v
    }
  }

  return undefined
}

/** What the event is about: its post, its collection, or the object named in `data`. */
function eventEntity(e: StashEvent): EventEntity | null {
  const d = eventData(e)

  if (e.post_id) {
    return {
      kind: 'Post',
      Icon: PenSquare,
      id: e.post_id,
      name: d.post?.content ? postTitle({ content: d.post.content }, 60) : undefined,
      href: e.type === 'post.deleted' ? undefined : `/posts/${e.post_id}`,
    }
  }

  const data = (e.data ?? {}) as Record<string, unknown>
  const kind = OBJECT_KINDS.find((k) => e.type.startsWith(k.prefix))

  if (kind) {
    const raw = kind.obj ? data[kind.obj] : undefined
    const obj = raw && typeof raw === 'object' ? (raw as Record<string, unknown>) : undefined
    const id = str(obj?.id) ?? pick(data, kind.idKeys)
    const name = pick(obj, kind.nameKeys) ?? pick(data, kind.nameKeys)

    if (id || name) {
      return { kind: kind.kind, Icon: kind.Icon, id, name, href: kind.href }
    }
  }

  if (e.collection_id) {
    return {
      kind: 'Collection',
      Icon: Folder,
      id: e.collection_id,
      name: d.collection?.name,
      href: e.type === 'collection.deleted' ? undefined : `/posts?collection=${e.collection_id}`,
    }
  }

  return null
}

const ACTOR_KIND: Record<string, { label: string; Icon: Icon }> = {
  api_key: { label: 'API key', Icon: Bot },
  session: { label: 'Dashboard', Icon: User },
  local: { label: 'Local', Icon: Laptop },
  system: { label: 'System', Icon: Cog },
}

function AuditActor({ actor, isLocal }: { actor: StashEvent['actor']; isLocal: boolean }) {
  const auto = systemActorKind(actor)

  if (auto) {
    const Icon = auto === 'scheduler' ? CalendarClock : Workflow
    const name = auto === 'scheduler' ? 'Schedule' : 'Publisher'

    return (
      <span
        data-testid="event-actor"
        className="inline-flex items-center gap-1 rounded-md bg-muted/70 px-1.5 py-0.5 text-muted-foreground"
        title={`${actor.name} (${actor.id})`}
      >
        <Icon className="size-3" />
        <span>Staqly</span>
        <span className="max-w-48 truncate font-medium text-foreground/80">{name}</span>
      </span>
    )
  }

  const kind = ACTOR_KIND[actor.type] ?? ACTOR_KIND.system
  const Icon = kind.Icon
  // Local installs have one person: older events still say "Dashboard".
  const name = isLocal && actor.type === 'session' && (!actor.name || actor.name === 'Dashboard') ? 'Local' : actor.name

  return (
    <span
      className="inline-flex min-w-0 flex-wrap items-center gap-x-1 text-muted-foreground"
      data-testid="event-actor"
    >
      <Icon className="size-3" />
      <span>{kind.label}</span>
      {name && name !== kind.label && (
        <span className="max-w-48 truncate font-medium text-foreground/80" title={name}>
          {name}
        </span>
      )}
      <span className="max-w-40 truncate font-mono text-[11px]" title={actor.id}>
        {actor.id}
      </span>
    </span>
  )
}

// Entity links don't prefetch: a feed shows hundreds, mostly /items/<id> and /collections/<id>, which are all
// one exported page; prefetching each one in view cost two requests per row (see IdRoutePrefetch).
function AuditEntity({
  entity,
  collection,
  collectionIds,
}: {
  entity: EventEntity
  collection?: { id?: string; name: string }
  collectionIds: ReadonlySet<string>
}) {
  const { Icon, name, href } = entity

  return (
    <span className="inline-flex max-w-full min-w-0 flex-wrap items-center gap-x-1" data-testid="event-entity">
      <Icon className="size-3.5 shrink-0 text-muted-foreground" />
      {href && !name ? (
        <Link
          href={href}
          prefetch={false}
          className="text-muted-foreground underline-offset-2 hover:text-foreground hover:underline pointer-coarse:-my-3 pointer-coarse:inline-flex pointer-coarse:min-w-10 pointer-coarse:justify-center pointer-coarse:py-3"
        >
          {entity.kind}
        </Link>
      ) : (
        <span className="text-muted-foreground">{entity.kind}</span>
      )}
      {name &&
        (href ? (
          <Link
            href={href}
            prefetch={false}
            className="max-w-64 truncate font-medium text-foreground/90 hover:underline pointer-coarse:-my-3 pointer-coarse:py-3"
          >
            {name}
          </Link>
        ) : (
          <span className="max-w-64 truncate font-medium text-foreground/90" title={name}>
            {name}
          </span>
        ))}
      {entity.id && <IdChip id={entity.id} label={entity.kind} />}
      {collection && (
        <CollectionLink
          id={collection.id}
          name={collection.name}
          exists={!!collection.id && collectionIds.has(collection.id)}
        />
      )}
    </span>
  )
}

const stringify = (data: unknown) => JSON.stringify(data ?? {}, null, 2)

/** The data as JSON: mounted when the card first opens, so a long feed never stringifies what nobody reads. */
function EventJson({ data }: { data: unknown }) {
  return <JsonCode json={stringify(data)} className="scrollbar-thin overflow-x-auto p-3" />
}

/** The event's raw data, collapsed to its ID until opened. */
function EventData({ event }: { event: StashEvent }) {
  return (
    <DisclosureCard
      className="mt-2"
      label="Data"
      copy={() => stringify(event.data)}
      copyLabel="Copy data"
      bodyTestId="event-data"
      header={
        <>
          <span className="shrink-0">Data</span>{' '}
          <code className="truncate rounded bg-muted px-1 py-px font-mono text-[11px] text-foreground/80">
            {event.id}
          </code>{' '}
          {typeof event.seq === 'number' && <span className="shrink-0 font-mono">#{event.seq}</span>}
        </>
      }
    >
      <EventJson data={event.data} />
    </DisclosureCard>
  )
}

const collectionIdsOf = (cols: { id: string }[]) => cols.map((c) => c.id)

/**
 * What every row needs from the queries, read once per list: per row, `useMode` and the collections were three
 * query observers each (750 on a feed of 250 events), mounted with every page of "Load older".
 */
function useRowContext() {
  const { isLocal } = useMode()
  const ids = useCollections(true, collectionIdsOf).data
  const collectionIds = useMemo(() => new Set(ids), [ids])

  return { isLocal, collectionIds }
}

const AuditEventRow = memo(function AuditEventRow({
  event: e,
  isLocal,
  collectionIds,
}: {
  event: StashEvent
  isLocal: boolean
  /** Collections that still exist (a link) or not (only the name). */
  collectionIds: ReadonlySet<string>
}) {
  const { Icon, cls } = eventIcon(e.type)
  const d = eventData(e)
  const entity = eventEntity(e)
  const statusChanged = d.previous_status && d.post?.status && d.previous_status !== d.post.status
  const collection =
    entity?.kind === 'Post' && d.collection?.name
      ? { id: e.collection_id ?? d.collection.id, name: d.collection.name }
      : undefined

  return (
    <li
      className="group relative flex gap-3 pb-5 last:pb-0 [contain-intrinsic-size:auto_96px] [content-visibility:auto]"
      data-testid="audit-event"
    >
      {/* The line to the next event; CSS hides it on the last row, so loading more doesn't re-render that row. */}
      <span className="absolute top-7 bottom-0 left-[13px] w-px bg-border group-last:hidden" aria-hidden />
      <span className={cn('relative z-10 flex size-7 shrink-0 items-center justify-center rounded-full', cls)}>
        <Icon className="size-3.5" />
      </span>
      <div className="min-w-0 flex-1 pt-0.5">
        <div className="flex flex-wrap items-center gap-x-2 gap-y-1 text-sm">
          <span className="font-medium">{eventTitle(e)}</span>
          {statusChanged && (
            <span className="inline-flex items-center gap-1">
              <PostStatusBadge status={d.previous_status!} />
              <span className="text-muted-foreground">→</span>
              <PostStatusBadge status={d.post!.status!} />
            </span>
          )}
          <span className="max-w-full truncate font-mono text-[11px] text-muted-foreground">{e.type}</span>
        </div>
        {d.note && (
          <blockquote
            className={cn(
              'mt-1.5 rounded-md border-l-2 px-3 py-1.5 text-sm break-words whitespace-pre-wrap',
              TONES.warning.callout,
            )}
          >
            {d.note}
          </blockquote>
        )}
        <dl className="mt-1.5 flex flex-wrap items-center gap-x-4 gap-y-1 text-xs">
          <div className="flex min-w-0 max-w-full items-center gap-1.5">
            <dt className="sr-only">Actor</dt>
            <dd className="min-w-0">
              <AuditActor actor={e.actor} isLocal={isLocal} />
            </dd>
          </div>
          {entity && (
            <div className="flex min-w-0 max-w-full items-center gap-1.5">
              <dt className="sr-only">Entity</dt>
              <dd className="min-w-0">
                <AuditEntity entity={entity} collection={collection} collectionIds={collectionIds} />
              </dd>
            </div>
          )}
          <div className="flex items-center gap-1.5 text-muted-foreground">
            <dt className="sr-only">Time</dt>
            <dd>
              <time dateTime={e.created_at} title={timeAgo(e.created_at)} data-testid="event-time">
                {formatDateTime(e.created_at)}
              </time>
            </dd>
          </div>
        </dl>
        <EventData event={e} />
      </div>
    </li>
  )
})

export function AuditEventList({ events, className }: { events: StashEvent[]; className?: string }) {
  const { isLocal, collectionIds } = useRowContext()

  return (
    <ol className={cn('relative', className)} data-testid="event-timeline">
      {events.map((e) => (
        <AuditEventRow key={e.id} event={e} isLocal={isLocal} collectionIds={collectionIds} />
      ))}
    </ol>
  )
}
