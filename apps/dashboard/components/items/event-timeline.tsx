import {
  Archive,
  Bot,
  CirclePlus,
  Cog,
  FolderPlus,
  KeyRound,
  Laptop,
  Radar,
  Siren,
  Timer,
  Pencil,
  Repeat,
  RotateCcw,
  Send,
  Sparkles,
  Trash2,
  User,
  Zap,
  type LucideIcon,
} from 'lucide-react'
import Link from 'next/link'

import { StatusBadge } from '@/components/common/status-badge'
import type { StashEvent } from '@/lib/api'
import { formatDateTime, timeAgo } from '@/lib/format'
import { useCollections, useMode } from '@/lib/queries'
import { STATUS_META } from '@/lib/status'
import { TONES, type Tone } from '@/lib/tones'
import { cn } from '@/lib/utils'

import { FeedbackCard } from './feedback-note'

const LABELS: Record<string, string> = {
  'item.created': 'Created',
  'item.updated': 'Edited',
  'item.status_changed': 'Status changed',
  'item.approved': 'Approved',
  'item.rejected': 'Rejected',
  'item.deleted': 'Deleted',
  'item.archived': 'Archived',
  'item.regenerate_requested': 'Regeneration requested',
  'item.publish_requested': 'Publish requested',
  'item.retry_requested': 'Retry requested',
  'collection.created': 'Collection created',
  'collection.updated': 'Collection updated',
  'collection.archived': 'Collection archived',
  'collection.deleted': 'Collection deleted',
  'api_key.created': 'API key created',
  'api_key.revoked': 'API key revoked',
  'recovery_key.rotated': 'Recovery key rotated',
  'workspace.created': 'Workspace created',
  'webhook.test': 'Webhook test',
  'item.escalated': 'Escalated',
  'item.expired': 'Expired',
  'watcher.created': 'Watcher created',
  'item.purged': 'Purged',
  'workspace.export_requested': 'Workspace export requested',
  'workspace.exported': 'Workspace exported',
  'workspace.updated': 'Workspace renamed',
  'workspace.plan_changed': 'Plan changed',
  'api_key.rotated': 'API key rotated',
  'session.revoked': 'Session revoked',
  'session.recovered': 'Signed in with recovery key',
  'device_link.created': 'Sign-in link created',
  'device.connected': 'Device connected',
  'webhook.created': 'Webhook created',
  'webhook.updated': 'Webhook updated',
  'webhook.deleted': 'Webhook deleted',
  'watcher.updated': 'Watcher updated',
  'watcher.deleted': 'Watcher deleted',
  'watcher.degraded': 'Watcher degraded',
  'watcher.paused': 'Watcher paused',
  'watcher.recovered': 'Watcher recovered',
  'notification_channel.created': 'Notification channel created',
  'notification_channel.updated': 'Notification channel updated',
  'notification_channel.deleted': 'Notification channel deleted',
  'retention.events_pruned': 'Old events pruned',
  'retention.items_pruned': 'Old items pruned',
}

/** "in Default": a link while the collection exists, plain text once it was deleted. */
export function CollectionRef({ id, name }: { id?: string; name: string }) {
  const exists = useCollections(true).data?.some((c) => c.id === id)

  return (
    <span className="max-w-48 truncate text-muted-foreground">
      in{' '}
      {id && exists ? (
        <Link href={`/collections/${id}`} className="hover:text-foreground hover:underline">
          {name}
        </Link>
      ) : (
        name
      )}
    </span>
  )
}

function eventLabel(type: string) {
  if (LABELS[type]) {
    return LABELS[type]
  }

  // Unknown types: "item.foo_bar" reads "Foo bar"; other objects keep their name ("Webhook foo").
  const [object, ...rest] = type.split('.')
  const words = (object === 'item' ? rest : [object, ...rest]).join(' ').replace(/_/g, ' ')

  return words ? words.charAt(0).toUpperCase() + words.slice(1) : type
}

/** Tone by what happened (the verb), whatever the object is; the same tones as the statuses (lib/tones.ts). */
function eventTone(type: string): Tone {
  const verb = type.slice(type.lastIndexOf('.') + 1)

  // Green like their status badges.
  if (verb === 'approved' || verb === 'completed') {
    return 'success'
  }

  if (verb === 'rejected') {
    return 'danger'
  }

  if (/^(deleted|purged|revoked|failed|degraded|events_pruned|items_pruned)$/.test(verb)) {
    return 'danger'
  }

  if (/^(paused|escalated)$/.test(verb)) {
    return 'warning'
  }

  if (/^(created|connected|recovered)$/.test(verb) || type.endsWith('_requested')) {
    return 'info' // new things, and actions an automation picks up (publish, regenerate, retry...)
  }

  return 'neutral' // edits, archive, expiry, settings
}

// First match wins: item events that end in a status use that status's icon, then verbs, then the object.
const ICONS: [(type: string) => boolean, LucideIcon][] = [
  [(t) => t === 'item.escalated', Siren],
  [(t) => t === 'item.expired', STATUS_META.expired.Icon],
  [(t) => t === 'item.approved', STATUS_META.approved.Icon],
  [(t) => t === 'item.completed', STATUS_META.completed.Icon],
  [(t) => t === 'item.rejected', STATUS_META.rejected.Icon],
  [(t) => t === 'item.failed', STATUS_META.failed.Icon],
  [(t) => t === 'item.created', CirclePlus],
  [(t) => t === 'item.updated', Pencil],
  [(t) => t === 'item.status_changed', Repeat],
  [(t) => t.endsWith('.deleted') || t.endsWith('.purged'), Trash2],
  [(t) => t.endsWith('.archived'), Archive],
  [(t) => t.includes('regenerate'), Sparkles],
  [(t) => t.includes('retry'), RotateCcw],
  [(t) => t.includes('publish'), Send],
  [(t) => t.startsWith('watcher.'), Radar],
  [(t) => t.startsWith('collection.'), FolderPlus],
  [(t) => t.startsWith('api_key.') || t.startsWith('recovery_key.'), KeyRound],
  [(t) => t.startsWith('item.'), Zap],
]

/** Icon by the object (or the item action), colour by the verb. */
export function eventIcon(type: string) {
  return { Icon: ICONS.find(([match]) => match(type))?.[1] ?? Cog, cls: TONES[eventTone(type)].chip }
}

/** System actors created by automation: "Watcher: <name>", "Expiry". */
export function systemActorKind(actor: StashEvent['actor']): 'watcher' | 'expiry' | null {
  if (actor.type !== 'system') {
    return null
  }

  if (actor.name.startsWith('Watcher: ')) {
    return 'watcher'
  }

  if (actor.id === 'expiry' || actor.name === 'Expiry') {
    return 'expiry'
  }

  return null
}

function ActorLabel({ actor }: { actor: StashEvent['actor'] }) {
  const { isLocal } = useMode()
  const auto = systemActorKind(actor)

  if (auto) {
    const Icon = auto === 'watcher' ? Radar : Timer
    const name = auto === 'watcher' ? actor.name.slice(9) : 'Expiry timer'
    const href = auto === 'watcher' ? '/watchers' : undefined
    const body = (
      <>
        <Icon className="size-3" />
        <span className="text-muted-foreground/80">{auto === 'watcher' ? 'Watcher' : 'Automation'}</span>
        <span className="max-w-48 truncate font-medium text-foreground/80">{name}</span>
      </>
    )

    return href ? (
      <Link
        href={href}
        data-testid="actor-automation"
        className="inline-flex items-center gap-1 rounded-md bg-muted/70 px-1.5 py-0.5 text-muted-foreground hover:bg-muted"
        title={`${actor.name} (${actor.id})`}
      >
        {body}
      </Link>
    ) : (
      <span
        data-testid="actor-automation"
        className="inline-flex items-center gap-1 rounded-md bg-muted/70 px-1.5 py-0.5 text-muted-foreground"
      >
        {body}
      </span>
    )
  }

  const Icon = actor.type === 'api_key' ? Bot : actor.type === 'session' ? User : actor.type === 'local' ? Laptop : Cog
  const kind =
    actor.type === 'api_key'
      ? 'API key'
      : actor.type === 'session'
        ? 'Dashboard'
        : actor.type === 'local'
          ? 'Local'
          : 'System'

  return (
    <span className="inline-flex items-center gap-1 text-muted-foreground" title={`${kind}: ${actor.id}`}>
      <Icon className="size-3" />
      <span className="max-w-48 truncate">
        {/* Local installs have one person: older events still say "Dashboard". */}
        {isLocal && actor.type === 'session' && (!actor.name || actor.name === 'Dashboard')
          ? 'Local'
          : actor.name || kind}
      </span>
    </span>
  )
}

interface EventData {
  feedback?: string
  previous_status?: string
  action?: string
  edited?: boolean
  /** item.failed after the processing timeout (no result reported in time). */
  timeout?: boolean
  item?: { id?: string; title?: string; status?: string }
  collection?: { id?: string; name?: string }
  rule?: { id?: string; name?: string; action?: string }
  note?: string
  expires_at?: string
}

/** The event's title; a processing timeout reads "Timed out" rather than "Failed". */
export function eventTitle(e: StashEvent) {
  return e.type === 'item.failed' && eventData(e).timeout ? 'Timed out' : eventLabel(e.type)
}

export function eventData(e: StashEvent): EventData {
  return (e.data ?? {}) as EventData
}

export function EventTimeline({ events, showItem = false }: { events: StashEvent[]; showItem?: boolean }) {
  return (
    <ol className="relative" data-testid="event-timeline">
      {events.map((e, i) => {
        const { Icon, cls } = eventIcon(e.type)
        const d = eventData(e)
        const statusChanged = d.previous_status && d.item?.status && d.previous_status !== d.item.status

        return (
          <li key={e.id} className="relative flex gap-3 pb-5 last:pb-0">
            {i < events.length - 1 && (
              <span className="absolute top-7 bottom-0 left-[13px] w-px bg-border" aria-hidden />
            )}
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
              </div>
              {showItem && e.item_id && (
                // One line: the item, then the collection it is in (both links).
                <div className="mt-0.5 flex min-w-0 items-baseline gap-1.5 text-sm">
                  <Link href={`/items/${e.item_id}`} className="truncate text-foreground/80 hover:underline">
                    {d.item?.title || e.item_id}
                  </Link>
                  {e.collection_id && (
                    <span className="shrink-0 text-xs">
                      <CollectionRef id={e.collection_id} name={d.collection?.name || 'collection'} />
                    </span>
                  )}
                </div>
              )}
              {showItem && !e.item_id && e.collection_id && e.type !== 'collection.deleted' && (
                <Link
                  href={`/collections/${e.collection_id}`}
                  className="mt-0.5 block truncate text-sm text-foreground/80 hover:underline"
                >
                  {d.collection?.name || e.collection_id}
                </Link>
              )}
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
              <div className="mt-1 flex flex-wrap items-center gap-x-2 text-xs text-muted-foreground">
                <ActorLabel actor={e.actor} />
                <span>·</span>
                <time dateTime={e.created_at} title={formatDateTime(e.created_at)}>
                  {timeAgo(e.created_at)}
                </time>
                <span className="font-mono opacity-60">{e.type}</span>
              </div>
              {d.feedback && <FeedbackCard text={d.feedback} className="mt-2" />}
            </div>
          </li>
        )
      })}
    </ol>
  )
}
