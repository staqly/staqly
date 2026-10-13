'use client'

import { toast } from 'sonner'

import { ApiError, errorMessage, type PlanLimitDetails } from './api'
import { formatBytes, formatInterval } from './format'

export const PLANS_HREF = '/settings/plan'

/** Navigation used by toast actions; replaced by the app router in <Providers>. */
let navigate: (href: string) => void = (href) => {
  if (typeof window !== 'undefined') {
    window.location.assign(href)
  }
}

export function setNavigator(fn: ((href: string) => void) | null) {
  navigate =
    fn ??
    ((href) => {
      if (typeof window !== 'undefined') {
        window.location.assign(href)
      }
    })
}

const LIMIT_LABELS: Record<string, string> = {
  items_per_month: 'items this month',
  webhooks: 'webhooks',
  api_keys: 'API keys',
  storage_bytes: 'storage',
  max_upload_bytes: 'maximum upload size',
  rules: 'rules',
  watchers: 'active watchers',
  min_watcher_interval_seconds: 'watcher interval',
  notification_channels: 'notification channels',
}

const PLAN_NAMES: Record<string, string> = { free: 'Free', indie: 'Indie', team: 'Team', unlocked: 'Unlocked' }

export function planName(id?: string) {
  if (!id) {
    return ''
  }

  return PLAN_NAMES[id] ?? id.charAt(0).toUpperCase() + id.slice(1)
}

/** Human description of a plan limit, e.g. "Your Free plan includes 3 webhooks (3 used)." */
export function describePlanLimit(d: PlanLimitDetails | undefined, fallback: string): string {
  if (!d?.limit || d.max === undefined) {
    return fallback
  }

  const plan = d.plan ? `Your ${planName(d.plan)} plan` : 'Your plan'

  if (d.limit === 'max_upload_bytes') {
    return `${plan} accepts files up to ${formatBytes(d.max)}.`
  }

  if (d.limit === 'min_watcher_interval_seconds') {
    return `${plan} checks sources at most every ${formatInterval(d.max)}. Pick a longer interval or upgrade.`
  }

  if (d.limit === 'storage_bytes') {
    return `${plan} includes ${formatBytes(d.max)} of storage${d.used !== undefined ? ` (${formatBytes(d.used)} used)` : ''}.`
  }

  const label = LIMIT_LABELS[d.limit] ?? d.limit.replace(/_/g, ' ')
  const what = d.limit === 'items_per_month' ? `${d.max.toLocaleString('en-US')} items per month` : `${d.max} ${label}`

  return `${plan} includes ${what}${d.used !== undefined ? ` (${d.used.toLocaleString('en-US')} used)` : ''}.`
}

/**
 * Toast for a failed API call. Plan limits (402) get a friendly explanation and
 * a "View plans" action; rate limits (429) a calm retry hint.
 */
export function toastApiError(err: unknown, fallbackTitle?: string) {
  if (err instanceof ApiError && err.isPlanLimit) {
    toast.warning('Plan limit reached', {
      id: 'plan-limit',
      description: describePlanLimit(err.planLimit, err.message),
      duration: 10_000,
      action: { label: 'View plans', onClick: () => navigate(PLANS_HREF) },
    })

    return
  }

  if (err instanceof ApiError && err.isRateLimited) {
    toast.warning('Slow down a little', { id: 'rate-limited', description: errorMessage(err) })

    return
  }

  if (fallbackTitle) {
    toast.error(fallbackTitle, { description: errorMessage(err) })
  } else {
    toast.error(errorMessage(err))
  }
}
