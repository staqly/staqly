'use client'

import {
  Archive,
  BadgeCheck,
  Bell,
  CalendarClock,
  CirclePlus,
  CircleX,
  Cog,
  Eye,
  FolderPlus,
  KeyRound,
  MessageSquareText,
  Pencil,
  PlugZap,
  Repeat,
  RotateCcw,
  Send,
  Share2,
  Trash2,
  type LucideIcon,
  Unplug,
  Zap,
} from 'lucide-react'
import Link from 'next/link'

import type { StashEvent } from '@/lib/api'
import { TONES, type Tone } from '@/lib/tones'

const LABELS: Record<string, string> = {
  'post.created': 'Post created',
  'post.updated': 'Post edited',
  'post.scheduled': 'Scheduled',
  'post.unscheduled': 'Schedule cleared',
  'post.publish_requested': 'Handed to your automation',
  'post.target_published': 'Published on a channel',
  'post.target_failed': 'A channel failed',
  'post.published': 'Published',
  'post.failed': 'Publishing failed',
  'post.deleted': 'Post deleted',
  'post.preview_shared': 'Preview link shared',
  'post.feedback_added': 'Feedback on a preview',
  'channel.connected': 'Channel connected',
  'channel_link.created': 'Connect link made',
  'channel_link.revoked': 'Connect link revoked',
  'channel.updated': 'Channel updated',
  'channel.needs_reconnect': 'Channel needs reconnecting',
  'channel.disconnected': 'Channel removed',
  'schedule.created': 'Schedule created',
  'schedule.updated': 'Schedule updated',
  'schedule.deleted': 'Schedule deleted',
  'schedule.ran': 'Schedule ran',
  'schedule.posts_requested': 'Schedule asked for posts',
  'schedule.failed': 'Schedule failed',
  'collection.created': 'Collection created',
  'collection.updated': 'Collection updated',
  'collection.archived': 'Collection archived',
  'collection.deleted': 'Collection deleted',
  'alert.created': 'Alert created',
  'alert.updated': 'Alert updated',
  'alert.deleted': 'Alert deleted',
  'api_key.created': 'API key created',
  'api_key.revoked': 'API key revoked',
  'api_key.rotated': 'API key rotated',
  'recovery_key.rotated': 'Recovery key rotated',
  'workspace.created': 'Workspace created',
  'workspace.export_requested': 'Workspace export requested',
  'workspace.exported': 'Workspace exported',
  'workspace.updated': 'Workspace updated',
  'workspace.plan_changed': 'Plan changed',
  'workspace.account_attached': 'Workspace joined an account',
  'webhook.test': 'Webhook test',
  'webhook.created': 'Webhook created',
  'webhook.updated': 'Webhook updated',
  'webhook.deleted': 'Webhook deleted',
  'session.revoked': 'Session revoked',
  'session.signed_in': 'Signed in with API key',
  'session.recovered': 'Signed in with recovery key',
  'device_link.created': 'Sign-in link created',
  'device.connected': 'Device connected',
  'sign_in_method.added': 'Sign-in method added',
  'sign_in_method.changed': 'Email address changed',
  'sign_in_method.removed': 'Sign-in method removed',
  'media.uploaded': 'Media uploaded',
  'media.deleted': 'Media deleted',
  'retention.events_pruned': 'Old events pruned',
  'retention.jobs_pruned': 'Old jobs pruned',
}

/** "in Default": a link while the collection exists, plain text once it was deleted. Lists look the collections up once for every row. */
export function CollectionLink({ id, name, exists }: { id?: string; name: string; exists: boolean }) {
  return (
    <span className="max-w-48 truncate text-muted-foreground" title={name}>
      in{' '}
      {id && exists ? (
        <Link prefetch={false} href={`/posts?collection=${id}`} className="hover:text-foreground hover:underline">
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

  // Unknown types: "post.foo_bar" reads "Foo bar"; other objects keep their name ("Webhook foo").
  const [object, ...rest] = type.split('.')
  const words = (object === 'post' ? rest : [object, ...rest]).join(' ').replace(/_/g, ' ')

  return words ? words.charAt(0).toUpperCase() + words.slice(1) : type
}

/** Tone by what happened (the verb), whatever the object is; the same tones as the statuses (lib/tones.ts). */
function eventTone(type: string): Tone {
  const verb = type.slice(type.lastIndexOf('.') + 1)

  if (verb === 'published' || verb === 'target_published' || verb === 'connected' || verb === 'ran') {
    return 'success'
  }

  if (/^(deleted|purged|revoked|failed|target_failed|disconnected|events_pruned|jobs_pruned)$/.test(verb)) {
    return 'danger'
  }

  if (/^(needs_reconnect|posts_requested)$/.test(verb)) {
    return 'warning'
  }

  if (/^(created|scheduled|recovered)$/.test(verb) || type.endsWith('_requested')) {
    return 'info'
  }

  return 'neutral' // edits, archive, settings
}

// First match wins: verbs first, then the object.
const ICONS: [(type: string) => boolean, LucideIcon][] = [
  [(t) => t === 'post.published' || t === 'post.target_published', BadgeCheck],
  [(t) => t === 'post.failed' || t === 'post.target_failed' || t === 'schedule.failed', CircleX],
  [(t) => t === 'post.created' || t === 'collection.created', CirclePlus],
  [(t) => t === 'post.updated', Pencil],
  [(t) => t === 'post.preview_shared', Eye],
  [(t) => t === 'post.feedback_added', MessageSquareText],
  [(t) => t === 'post.scheduled' || t === 'post.unscheduled' || t.startsWith('schedule.'), CalendarClock],
  [(t) => t.endsWith('.deleted') || t.endsWith('.purged'), Trash2],
  [(t) => t.endsWith('.archived'), Archive],
  [(t) => t === 'channel.needs_reconnect' || t === 'channel.disconnected', Unplug],
  [(t) => t === 'channel.connected', PlugZap],
  [(t) => t.startsWith('channel.') || t.startsWith('channel_link.'), Share2],
  [(t) => t.includes('retry'), RotateCcw],
  [(t) => t.includes('publish'), Send],
  [(t) => t.startsWith('alert.'), Bell],
  [(t) => t.startsWith('collection.'), FolderPlus],
  [(t) => t.startsWith('api_key.') || t.startsWith('recovery_key.'), KeyRound],
  [(t) => t.startsWith('post.'), Zap],
  [(t) => t.includes('status'), Repeat],
]

/** Icon by the object (or the verb), colour by the verb. */
export function eventIcon(type: string) {
  return { Icon: ICONS.find(([match]) => match(type))?.[1] ?? Cog, cls: TONES[eventTone(type)].chip }
}

/** System actors created by automation: the scheduler (collection schedules) and the job queue. */
export function systemActorKind(actor: StashEvent['actor']): 'scheduler' | 'jobs' | null {
  if (actor.type !== 'system') {
    return null
  }

  if (actor.id === 'scheduler' || actor.id === 'schedule') {
    return 'scheduler'
  }

  if (actor.id === 'jobs' || actor.id === 'worker' || actor.id === 'publisher') {
    return 'jobs'
  }

  return null
}

interface EventData {
  previous_status?: string
  post?: { id?: string; content?: string; status?: string }
  collection?: { id?: string; name?: string }
  channel?: { id?: string; name?: string; provider?: string }
  schedule?: { id?: string; name?: string }
  target?: { channel_id?: string; status?: string; external_url?: string; error?: string }
  note?: string
  error?: string
  /** post.scheduled: the new time (null = cleared) and the one before. */
  publish_at?: string | null
  previous_publish_at?: string | null
  reason?: string
}

/** The event's title: a move reads "Rescheduled"; a schedule's run says what it did. */
export function eventTitle(e: StashEvent) {
  const d = eventData(e)

  if (e.type === 'post.scheduled') {
    return d.previous_publish_at ? 'Rescheduled' : 'Scheduled'
  }

  return eventLabel(e.type)
}

export function eventData(e: StashEvent): EventData {
  return (e.data ?? {}) as EventData
}
