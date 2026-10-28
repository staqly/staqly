import type { StashEvent } from './api'
import { groupNotificationEvents, OUTCOMES } from './notification-groups'
import type { Tone } from './tones'

/** One channel of a publish: where it went (or didn't), with the link to the published post. */
export interface NotificationChannel {
  id: string
  /** The channel's own name ("Brand"). */
  name: string
  provider: string
  /** The platform's name ("Bluesky"); empty on events from before the API sent it. */
  providerName: string
  avatar_url?: string
  status: 'published' | 'failed'
  /** The published post on the platform. */
  url?: string
  error?: string
}

/** What the bell shows for one event, or for one publish to several channels: a sentence, where it leads, its tone. */
export interface Notification {
  id: string
  /** The newest event's sequence number: a stack is unread while any of it is. */
  seq: number
  tone: Tone
  text: string
  /** The dashboard page to open (a post, the channels, a collection, the plan). */
  href?: string
  /** An outside link: the published post on the platform. */
  link?: string
  at: string
  /** The newest event behind it (who acted, its id). */
  event: StashEvent
  /** A publish: every channel it went to, newest outcome per channel (one entry for a single channel). */
  channels?: NotificationChannel[]
}

const str = (v: unknown) => (typeof v === 'string' ? v : '')
const obj = (v: unknown) => (v && typeof v === 'object' ? (v as Record<string, unknown>) : undefined)
const httpUrl = (v: string) => (/^https?:\/\//.test(v) ? v : undefined)

/** The first line of a post's content, shortened, in quotes. */
function quote(content: string) {
  const line = content.trim().split('\n')[0] ?? ''
  const short = line.length > 60 ? `${line.slice(0, 57)}...` : line

  return `“${short || 'Untitled post'}”`
}

/** "Brand on Bluesky"; just the name for your own automation or when the platform is unknown. */
export function channelLabel(c: Pick<NotificationChannel, 'name' | 'provider' | 'providerName'>) {
  const name = c.name || 'a channel'

  return c.providerName && c.provider !== 'custom' && c.providerName !== c.name ? `${name} on ${c.providerName}` : name
}

/** The channel of a per-target event: `data.channel` (newer servers), else the post's target for that channel. */
function targetChannel(e: StashEvent): NotificationChannel {
  const d = (e.data ?? {}) as Record<string, unknown>
  const channelId = str(d.channel_id) || e.channel_id || ''
  const channel = obj(d.channel)
  const targets = obj(d.post)?.targets
  const target = Array.isArray(targets)
    ? (targets.map(obj).find((t) => str(t?.channel_id) === channelId) ?? undefined)
    : undefined
  const failed = e.type === 'post.target_failed'

  return {
    id: channelId || e.id,
    name: str(channel?.name) || str(target?.channel_name),
    provider: str(channel?.provider) || str(target?.provider),
    providerName: str(channel?.provider_name),
    avatar_url: str(channel?.avatar_url) || str(target?.avatar_url) || undefined,
    status: failed ? 'failed' : 'published',
    url: failed ? undefined : httpUrl(str(d.url) || str(obj(d.target)?.external_url) || str(target?.external_url)),
    error: failed ? str(d.error) || str(obj(d.target)?.error) || str(target?.error) || undefined : undefined,
  }
}

/**
 * The events worth a notification: what the scheduler did (published, failed, asked for posts), what the
 * platforms did (a channel that needs reconnecting) and plan changes. Edits made in this dashboard are not
 * news to the person who made them.
 */
export function notificationFor(e: StashEvent): Notification | null {
  const d = (e.data ?? {}) as Record<string, unknown>
  const post = obj(d.post)
  const channel = obj(d.channel)
  const content = str(post?.content)
  const channelName = str(channel?.name)
  const base = { id: e.id, seq: e.seq, at: e.created_at, event: e }
  const postHref = e.post_id ? `/posts/${e.post_id}` : undefined

  switch (e.type) {
    case 'post.published':
      return { ...base, tone: 'success', text: `${quote(content)} was published`, href: postHref }
    case 'post.failed':
      return {
        ...base,
        tone: 'danger',
        text: `Publishing ${quote(content)} failed${str(d.error) ? `: ${str(d.error)}` : ''}`,
        href: postHref,
      }
    case 'post.target_published':
    case 'post.target_failed':
      return publishNotification(base, content, postHref, [targetChannel(e)])
    case 'post.feedback_added':
      return {
        ...base,
        tone: 'info',
        text: `${str(d.author_name) || 'A reviewer'} commented on a post preview: ${quote(str(d.body))}`,
        href: postHref,
      }
    case 'post.publish_requested':
      return {
        ...base,
        tone: 'info',
        text: `${quote(content)} is waiting for your automation (${channelName || 'custom channel'})`,
        href: postHref,
      }
    case 'schedule.posts_requested':
      return {
        ...base,
        tone: 'info',
        text: `${quote(str(obj(d.schedule)?.name))} found nothing to publish and asked for new posts`,
        href: e.collection_id ? `/posts?collection=${e.collection_id}` : '/posts',
      }
    case 'schedule.failed':
      return {
        ...base,
        tone: 'danger',
        text: `Schedule ${quote(str(obj(d.schedule)?.name))} failed${str(d.error) ? `: ${str(d.error)}` : ''}`,
        href: e.collection_id ? `/posts?collection=${e.collection_id}` : '/posts',
      }
    case 'channel.needs_reconnect':
      return {
        ...base,
        tone: 'danger',
        text: `${channelName || 'A channel'} needs to be reconnected${str(d.error) ? `: ${str(d.error)}` : ''}`,
        href: '/channels',
      }
    case 'channel.connected':
      return {
        ...base,
        tone: 'success',
        // Connected by someone outside through a connect link: say so, it's news to the team.
        text: e.actor?.id?.startsWith('channel_link:')
          ? `${channelName || 'A channel'} was connected through a link`
          : `${channelName || 'A channel'} is connected`,
        href: '/channels',
      }
    case 'workspace.plan_changed':
      return {
        ...base,
        tone: 'info',
        text: str(d.to) ? `The plan changed to ${str(d.to)}` : 'The plan changed',
        href: '/settings/plan',
      }
    default:
      return null
  }
}

/**
 * A publish to one or more channels as one sentence: "is out on Brand on Bluesky", "is out on 5 channels",
 * "is out on 4 channels, failed on 1". A failure always shows (red), never inside a success.
 * `postFailed`: the post's own failure when none of these channels failed (one outside the stack did).
 */
function publishNotification(
  base: Pick<Notification, 'id' | 'seq' | 'at' | 'event'>,
  content: string,
  href: string | undefined,
  channels: NotificationChannel[],
  postFailed?: { error: string },
): Notification {
  const out = channels.filter((c) => c.status === 'published')
  const failed = channels.filter((c) => c.status === 'failed')
  const only = channels.length === 1 ? channels[0]! : undefined
  let text: string

  if (only) {
    text =
      only.status === 'published'
        ? `${quote(content)} is out on ${channelLabel(only)}`
        : `${quote(content)} did not go out on ${channelLabel(only)}${only.error ? `: ${only.error}` : ''}`
  } else if (failed.length === 0) {
    text = `${quote(content)} is out on ${out.length} channels`
  } else if (out.length === 0) {
    text = `${quote(content)} failed on ${failed.length} channels`
  } else {
    text = `${quote(content)} is out on ${out.length} ${out.length === 1 ? 'channel' : 'channels'}, failed on ${failed.length}`
  }

  if (postFailed && failed.length === 0) {
    text += `, but publishing failed${postFailed.error ? `: ${postFailed.error}` : ''}`
  }

  return {
    ...base,
    tone: failed.length > 0 || postFailed ? 'danger' : 'success',
    text,
    href,
    link: only?.status === 'published' ? only.url : undefined,
    channels,
  }
}

/**
 * Notifications from events (newest first): the outcomes of one publish (each channel's result and the post's
 * own) become one stacked entry; everything else is one entry per event. `muted`: event types the person turned
 * off for the bell (Settings, Account, Notifications).
 */
export function notificationsFor(events: StashEvent[] | undefined, muted?: ReadonlySet<string>): Notification[] {
  return groupNotificationEvents(events, muted).flatMap((g) => {
    const n = notificationOf(g)

    return n ? [n] : []
  })
}

/** One entry of groupNotificationEvents as a notification. */
export function notificationOf(group: StashEvent[]): Notification | null {
  const first = group[0]

  if (!first) {
    return null
  }

  return first.post_id && OUTCOMES.has(first.type) ? stackNotification(group) : notificationFor(first)
}

/** One publish's events (newest first) as one notification. */
function stackNotification(events: StashEvent[]): Notification | null {
  const newest = events[0]!
  const byChannel = new Map<string, NotificationChannel>()
  let summary: StashEvent | undefined

  for (const e of events) {
    if (e.type === 'post.published' || e.type === 'post.failed') {
      summary ??= e
      continue
    }

    const c = targetChannel(e)

    // A retried channel: its newest outcome counts.
    if (!byChannel.has(c.id)) {
      byChannel.set(c.id, c)
    }
  }

  if (byChannel.size === 0) {
    return summary ? { ...notificationFor(summary)!, seq: newest.seq } : null
  }

  // Failed channels first: what needs attention leads the list.
  const channels = [...byChannel.values()].toSorted(
    (a, b) => Number(b.status === 'failed') - Number(a.status === 'failed'),
  )
  const content =
    events.map((e) => str(obj(((e.data ?? {}) as Record<string, unknown>).post)?.content)).find(Boolean) ?? ''
  const postFailed =
    summary?.type === 'post.failed' ? { error: str((summary.data as Record<string, unknown>)?.error) } : undefined

  return publishNotification(
    { id: newest.id, seq: newest.seq, at: newest.created_at, event: newest },
    content,
    `/posts/${newest.post_id}`,
    channels,
    postFailed,
  )
}
