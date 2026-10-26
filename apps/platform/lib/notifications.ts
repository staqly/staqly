import type { StashEvent } from './api'
import type { Tone } from './tones'

/** What the bell shows for one event: a sentence a person understands, where it leads, and its tone. */
export interface Notification {
  id: string
  seq: number
  tone: Tone
  text: string
  /** The dashboard page to open (a post, the channels, a collection, the plan). */
  href?: string
  /** An outside link: the published post on the platform. */
  link?: string
  at: string
}

const str = (v: unknown) => (typeof v === 'string' ? v : '')
const obj = (v: unknown) => (v && typeof v === 'object' ? (v as Record<string, unknown>) : undefined)

/** The first line of a post's content, shortened, in quotes. */
function quote(content: string) {
  const line = content.trim().split('\n')[0] ?? ''
  const short = line.length > 60 ? `${line.slice(0, 57)}...` : line

  return `“${short || 'Untitled post'}”`
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
  const target = obj(d.target)
  const content = str(post?.content)
  const channelName = str(channel?.name)
  const base = { id: e.id, seq: e.seq, at: e.created_at }
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
    case 'post.target_published': {
      const url = str(target?.external_url)

      return {
        ...base,
        tone: 'success',
        text: `${quote(content)} is out on ${channelName || 'a channel'}`,
        href: postHref,
        link: /^https?:\/\//.test(url) ? url : undefined,
      }
    }

    case 'post.target_failed':
      return {
        ...base,
        tone: 'warning',
        text: `${quote(content)} did not go out on ${channelName || 'a channel'}${str(target?.error) ? `: ${str(target?.error)}` : ''}`,
        href: postHref,
      }
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

/** Every event type that makes a notification (notificationFor): the bell and the page ask the API for these. */
export const NOTIFICATION_TYPES = [
  'post.published',
  'post.failed',
  'post.target_published',
  'post.target_failed',
  'post.feedback_added',
  'post.publish_requested',
  'schedule.posts_requested',
  'schedule.failed',
  'channel.needs_reconnect',
  'channel.connected',
  'workspace.plan_changed',
]

/** `muted`: event types the person turned off for the bell (Settings, Account, Notifications). */
export function notificationsFor(events: StashEvent[] | undefined, muted?: ReadonlySet<string>): Notification[] {
  return (events ?? [])
    .filter((e) => !muted?.has(e.type))
    .map(notificationFor)
    .filter((n): n is Notification => n !== null)
}

const KEY = 'staqly.notifications.seen.'

/** The newest event sequence number this browser has seen for the workspace (0 = none). */
export function readSeenSeq(workspace: string): number {
  try {
    return Number(localStorage.getItem(KEY + workspace) ?? 0) || 0
  } catch {
    return 0
  }
}

export function writeSeenSeq(workspace: string, seq: number) {
  try {
    localStorage.setItem(KEY + workspace, String(seq))
  } catch {
    // Private windows and blocked storage: the count simply starts over next time.
  }
}
