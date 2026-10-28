import type { StashEvent } from './api'

/**
 * Which events make notifications and how they group, without their wording: the shell counts unread entries
 * with this alone, and the sentences (lib/notifications.ts) load with the list.
 */

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

const TYPES = new Set(NOTIFICATION_TYPES)

/** A publish's outcomes: each channel's result and the post's own. */
export const OUTCOMES = new Set(['post.target_published', 'post.target_failed', 'post.published', 'post.failed'])

/** Publishing outcomes of one post this close together are one publish (channels run as separate jobs). */
export const STACK_WINDOW_MS = 15 * 60_000

/**
 * Events (newest first) as notification entries, each a list of events newest first: the outcomes of one publish
 * become one entry, everything else is one entry per event. An entry's place and its unread state come from its
 * newest event. `muted`: event types the person turned off for the bell (Settings, Account, Notifications).
 */
export function groupNotificationEvents(events: StashEvent[] | undefined, muted?: ReadonlySet<string>): StashEvent[][] {
  const groups: StashEvent[][] = []
  const open = new Map<string, { events: StashEvent[]; oldest: number }>()

  for (const e of events ?? []) {
    if (!TYPES.has(e.type) || muted?.has(e.type)) {
      continue
    }

    if (!e.post_id || !OUTCOMES.has(e.type)) {
      groups.push([e])
      continue
    }

    const at = new Date(e.created_at).getTime()
    const stack = open.get(e.post_id)

    // Joins the stack while within the window of its oldest event (a chain of channel jobs); a later retry or
    // publish of the same post starts its own.
    if (stack && stack.oldest - at <= STACK_WINDOW_MS) {
      stack.events.push(e)
      stack.oldest = Math.min(stack.oldest, at)
      continue
    }

    const next = { events: [e], oldest: at }

    open.set(e.post_id, next)
    groups.push(next.events)
  }

  return groups
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
