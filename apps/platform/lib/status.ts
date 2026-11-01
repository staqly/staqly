import {
  BadgeCheck,
  CalendarClock,
  CircleDashed,
  CircleX,
  Clock3,
  LoaderCircle,
  PauseCircle,
  PlugZap,
  Undo2,
  Unplug,
  type LucideIcon,
} from 'lucide-react'

import type { ChannelStatus, PostStatus, TargetStatus } from './api'
import { TONES, type Tone } from './tones'

export interface StatusMeta {
  label: string
  tone: Tone
  Icon: LucideIcon
  /** Badge classes (bg/text/ring), from the tone. */
  badge: string
  /** Solid dot colour, from the tone. */
  dot: string
  description: string
}

const status = (label: string, tone: Tone, Icon: LucideIcon, description: string): StatusMeta => ({
  label,
  tone,
  Icon,
  badge: TONES[tone].badge,
  dot: TONES[tone].dot,
  description,
})

/** Every post status: its tone (lib/tones.ts) and icon, the same in badges, counts, filters and timelines. */
export const POST_STATUS_META: Record<PostStatus, StatusMeta> = {
  draft: status('Draft', 'neutral', CircleDashed, 'Not scheduled yet'),
  scheduled: status('Scheduled', 'info', CalendarClock, 'Goes out at its time'),
  publishing: status('Publishing', 'info', LoaderCircle, 'Being published right now'),
  published: status('Published', 'success', BadgeCheck, 'Out on every channel'),
  failed: status('Failed', 'danger', CircleX, 'At least one channel failed; retry it or reschedule'),
}

/** A post that was published and then taken down from its channels (shown instead of Published). */
export const UNPUBLISHED_META = status('Unpublished', 'neutral', Undo2, 'Taken down from its channels')

/** One post on one channel. */
export const TARGET_STATUS_META: Record<TargetStatus, StatusMeta> = {
  pending: status('Waiting', 'neutral', Clock3, 'Not published yet'),
  publishing: status('Publishing', 'info', LoaderCircle, 'Being published right now'),
  published: status('Published', 'success', BadgeCheck, 'Out on this channel'),
  failed: status('Failed', 'danger', CircleX, 'Publishing here failed'),
}

/** A connected channel. */
export const CHANNEL_STATUS_META: Record<ChannelStatus, StatusMeta> = {
  connected: status('Connected', 'success', PlugZap, 'Ready to publish'),
  needs_reconnect: status('Needs reconnect', 'danger', Unplug, 'The platform no longer accepts the saved sign-in'),
  disabled: status('Disabled', 'neutral', PauseCircle, 'Kept, but nothing is published here'),
}

export function postStatusMeta(value: string): StatusMeta {
  return POST_STATUS_META[value as PostStatus] ?? status(value, 'neutral', CircleDashed, value)
}

export function targetStatusMeta(value: string): StatusMeta {
  return TARGET_STATUS_META[value as TargetStatus] ?? status(value, 'neutral', CircleDashed, value)
}

export function channelStatusMeta(value: string): StatusMeta {
  return CHANNEL_STATUS_META[value as ChannelStatus] ?? status(value, 'neutral', CircleDashed, value)
}
