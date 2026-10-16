import {
  Archive,
  Ban,
  BadgeCheck,
  CircleCheck,
  CircleDashed,
  CircleX,
  Clock3,
  ClockFading,
  ThumbsDown,
  TimerOff,
  type LucideIcon,
} from 'lucide-react'

import type { ItemStatus } from './api'
import { TONES, type Tone } from './tones'

interface StatusMeta {
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

/** Every item status: its tone (lib/tones.ts) and icon, the same in badges, counts, tabs and timelines. */
export const STATUS_META: Record<ItemStatus, StatusMeta> = {
  draft: status('Draft', 'neutral', CircleDashed, 'Not ready for review'),
  pending: status('Pending', 'warning', Clock3, 'Waiting for review'),
  approved: status('Approved', 'success', CircleCheck, 'Approved by a reviewer'),
  rejected: status('Rejected', 'danger', ThumbsDown, 'Rejected by a reviewer'),
  expired: status('Expired', 'neutral', TimerOff, 'Nobody reviewed it in time'),
  processing: status('Processing', 'info', ClockFading, 'An automation is working on it'),
  completed: status('Completed', 'success', BadgeCheck, 'Downstream work finished'),
  failed: status('Failed', 'danger', CircleX, 'Downstream work failed'),
  archived: status('Archived', 'neutral', Archive, 'Kept for reference'),
}

/** A run stopped by hand: reported as failed with this error (processing-note.tsx), shown as Canceled. */
export const CANCELED_ERROR = 'Canceled in the dashboard'
export const CANCELED = status('Canceled', 'neutral', Ban, 'Stopped by hand before the automation reported back')

export function isCanceled(item: { run?: string; metadata?: unknown }) {
  return item.run === 'failed' && (item.metadata as { error?: string } | null)?.error === CANCELED_ERROR
}

export function statusMeta(value: string): StatusMeta {
  return STATUS_META[value as ItemStatus] ?? status(value, 'neutral', CircleDashed, value)
}
