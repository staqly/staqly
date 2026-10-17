'use client'

import { Bell, Bot, Box, Cog, Folder, KeyRound, Laptop, Package, Radar, Timer, User, Webhook } from 'lucide-react'
import Link from 'next/link'
import { memo } from 'react'

import { DisclosureCard } from '@/components/common/disclosure-card'
import { IdChip } from '@/components/common/id-chip'
import { JsonCode } from '@/components/common/json-code'
import { StatusBadge } from '@/components/common/status-badge'
import { CollectionRef, eventData, eventIcon, eventTitle, systemActorKind } from '@/components/items/event-timeline'
import { FeedbackCard } from '@/components/items/feedback-note'
import type { StashEvent } from '@/lib/api'
import { formatDateTime, timeAgo } from '@/lib/format'
import { useMode } from '@/lib/queries'
import { TONES } from '@/lib/tones'
import { cn } from '@/lib/utils'

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

// Non-item events carry the object they are about in `data` (see apps/api internal/app emit calls).
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
    prefix: 'watcher.',
    kind: 'Watcher',
    Icon: Radar,
    obj: 'watcher',
    idKeys: ['watcher_id'],
    nameKeys: ['name'],
    href: '/watchers',
  },
  {
    prefix: 'notification_channel.',
    kind: 'Channel',
    Icon: Bell,
    obj: 'channel',
    idKeys: ['channel_id'],
    nameKeys: ['name'],
    href: '/notifications',
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

/** What the event is about: its item, its collection, or the object named in `data`. */
function eventEntity(e: StashEvent): EventEntity | null {
  const d = eventData(e)

  if (e.item_id) {
    return { kind: 'Item', Icon: Box, id: e.item_id, name: d.item?.title, href: `/items/${e.item_id}` }
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
      href: e.type === 'collection.deleted' ? undefined : `/collections/${e.collection_id}`,
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

function AuditActor({ actor }: { actor: StashEvent['actor'] }) {
  const { isLocal } = useMode()
  const auto = systemActorKind(actor)

  if (auto) {
    const Icon = auto === 'watcher' ? Radar : Timer
    const kind = auto === 'watcher' ? 'Watcher' : 'Automation'
    const name = auto === 'watcher' ? actor.name.slice(9) : 'Expiry timer'
    const href = auto === 'watcher' ? '/watchers' : undefined

    return (
      <span className="inline-flex min-w-0 items-center gap-1" data-testid="event-actor">
        <Icon className="size-3.5 shrink-0 text-muted-foreground" />
        <span className="text-muted-foreground">{kind}</span>
        {href ? (
          <Link href={href} className="truncate font-medium text-foreground/90 hover:underline" title={actor.id}>
            {name}
          </Link>
        ) : (
          <span className="truncate font-medium text-foreground/90">{name}</span>
        )}
      </span>
    )
  }

  const { label, Icon } = ACTOR_KIND[actor.type] ?? { label: actor.type, Icon: Cog }

  // A person: the local user's name (or "Local"), a dashboard session in cloud mode.
  if (actor.type === 'local' || actor.type === 'session') {
    const person = isLocal ? (actor.name && actor.name !== 'Dashboard' ? actor.name : 'Local') : actor.name || label

    return (
      <span className="inline-flex min-w-0 flex-wrap items-center gap-x-1" data-testid="event-actor">
        <Icon className="size-3.5 shrink-0 text-muted-foreground" />
        <span className="max-w-56 truncate font-medium text-foreground/90">{person}</span>
        {!isLocal && actor.type === 'session' && actor.id && <IdChip id={actor.id} label="actor" />}
      </span>
    )
  }

  const name = actor.name && actor.name !== label ? actor.name : ''
  const showId = actor.type === 'api_key'

  return (
    <span className="inline-flex min-w-0 flex-wrap items-center gap-x-1" data-testid="event-actor">
      <Icon className="size-3.5 shrink-0 text-muted-foreground" />
      <span className={name ? 'text-muted-foreground' : 'font-medium text-foreground/90'}>{label}</span>
      {name && <span className="max-w-56 truncate font-medium text-foreground/90">{name}</span>}
      {showId && actor.id && <IdChip id={actor.id} label="actor" />}
    </span>
  )
}

function AuditEntity({ entity, collection }: { entity: EventEntity; collection?: { id?: string; name: string } }) {
  const { Icon, name, href } = entity

  return (
    <span className="inline-flex min-w-0 flex-wrap items-center gap-x-1" data-testid="event-entity">
      <Icon className="size-3.5 shrink-0 text-muted-foreground" />
      {href && !name ? (
        <Link href={href} className="text-muted-foreground underline-offset-2 hover:text-foreground hover:underline">
          {entity.kind}
        </Link>
      ) : (
        <span className="text-muted-foreground">{entity.kind}</span>
      )}
      {name &&
        (href ? (
          <Link href={href} className="max-w-64 truncate font-medium text-foreground/90 hover:underline">
            {name}
          </Link>
        ) : (
          <span className="max-w-64 truncate font-medium text-foreground/90">{name}</span>
        ))}
      {entity.id && <IdChip id={entity.id} label={entity.kind} />}
      {collection && <CollectionRef id={collection.id} name={collection.name} />}
    </span>
  )
}

/** The event's raw data, collapsed to its ID until opened. */
function EventData({ event }: { event: StashEvent }) {
  const json = JSON.stringify(event.data ?? {}, null, 2)

  return (
    <DisclosureCard
      className="mt-2"
      label="Data"
      copy={json}
      copyLabel="Copy data"
      bodyTestId="event-data"
      header={
        <>
          <span className="shrink-0">Data</span>
          <code className="truncate rounded bg-muted px-1 py-px font-mono text-[11px] text-foreground/80">
            {event.id}
          </code>
          {typeof event.seq === 'number' && <span className="shrink-0 font-mono">#{event.seq}</span>}
        </>
      }
    >
      <JsonCode json={json} className="scrollbar-thin overflow-x-auto p-3" />
    </DisclosureCard>
  )
}

export const AuditEventRow = memo(function AuditEventRow({ event: e }: { event: StashEvent }) {
  const { Icon, cls } = eventIcon(e.type)
  const d = eventData(e)
  const entity = eventEntity(e)
  const statusChanged = d.previous_status && d.item?.status && d.previous_status !== d.item.status
  const collection =
    entity?.kind === 'Item' && d.collection?.name
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
              <StatusBadge status={d.previous_status!} />
              <span className="text-muted-foreground">→</span>
              <StatusBadge status={d.item!.status!} />
            </span>
          )}
          {d.edited && <span className="text-xs text-muted-foreground">(with edits)</span>}
          <span className="max-w-full truncate font-mono text-[11px] text-muted-foreground/80">{e.type}</span>
        </div>
        {e.type === 'item.rule_applied' && d.expires_at && (
          <div className="mt-1 text-sm text-muted-foreground">
            Expires if still pending{' '}
            <time
              dateTime={d.expires_at}
              title={formatDateTime(d.expires_at)}
              className="font-medium text-foreground/80"
            >
              {timeAgo(d.expires_at)}
            </time>
          </div>
        )}
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
              <AuditActor actor={e.actor} />
            </dd>
          </div>
          {entity && (
            <div className="flex min-w-0 max-w-full items-center gap-1.5">
              <dt className="sr-only">Entity</dt>
              <dd className="min-w-0">
                <AuditEntity entity={entity} collection={collection} />
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
        {d.feedback && <FeedbackCard text={d.feedback} className="mt-2" />}
        <EventData event={e} />
      </div>
    </li>
  )
})

export function AuditEventList({ events }: { events: StashEvent[] }) {
  return (
    <ol className="relative" data-testid="event-timeline">
      {events.map((e) => (
        <AuditEventRow key={e.id} event={e} />
      ))}
    </ol>
  )
}
